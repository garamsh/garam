use decision::{
    Decision, Engine, EntityRef, EntitySource, Evaluation, Outcome, PolicySource, Reason,
};
use serde_json::json;
use tonic::{Request, Response, Status};

use crate::caller::{Principal, Refusal};
use crate::entity_changes::{parse, within};
use crate::entity_sink::EntitySink;
use crate::proto;
use crate::proto::policy_service_server::PolicyService;
use crate::status::{ServiceError, to_status};

/// The most evaluations one request holds: a page, not a table.
const MAX_EVALUATIONS: usize = 1000;
/// The most changes one request holds.
const MAX_CHANGES: usize = 1000;

/// Serves `garam.policy.v1`. Every read goes to storage, as no cache
/// exists yet, so both `Read` options answer what `READ_CURRENT`
/// promises.
#[derive(Debug, Clone)]
pub struct Service<P, E> {
    pub(crate) engine: Engine,
    pub(crate) policies: P,
    pub(crate) entities: E,
    pub(crate) store: Option<grant::PolicyStore>,
    pub(crate) recorder: Option<audit::Recorder>,
}

impl<P, E> Service<P, E> {
    /// Gives the service the recorder every denial is recorded to
    /// (ADR-0148) and the audit read reads from. Without it nothing is
    /// recorded and the read answers `UNAVAILABLE`.
    #[must_use]
    pub fn recording_to(mut self, recorder: audit::Recorder) -> Self {
        self.recorder = Some(recorder);
        self
    }

    /// Records a denial the engine decided, unless the question was a
    /// probe. The record never changes the answer: a failure to write
    /// it is logged and counted by the recorder.
    pub(crate) async fn note_denial(
        &self,
        principal: &Principal,
        asked: &Evaluation<'_>,
        decision: &Decision,
    ) {
        let mut policies: Vec<String> = decision
            .reasons
            .iter()
            .map(|reason| match reason {
                Reason::Policy(policy) => policy.to_string(),
                Reason::Error { .. } => audit::FAULT.to_owned(),
            })
            .collect();
        policies.sort();
        policies.dedup();
        self.record_denial(
            principal,
            (asked.subject.id, asked.action, asked.resource.id),
            &policies,
        )
        .await;
    }

    /// Records a denial of `action` on `resource` to `subject`, naming
    /// the `policies` that decided it. A write refused for want of a
    /// role is one: no policy decided it, so it names none.
    pub(crate) async fn record_denial(
        &self,
        principal: &Principal,
        (subject, action, resource): (&str, &str, &str),
        policies: &[String],
    ) {
        let Some(recorder) = &self.recorder else {
            return;
        };
        recorder
            .deny(&audit::Denial {
                service: &principal.sub(),
                subject,
                action,
                resource,
                policies,
            })
            .await;
    }

    /// The manifests the engine decides under, which the HTTP binding
    /// reads a GRN's type through.
    pub(crate) fn manifests(&self) -> &manifest::Manifests {
        self.engine.manifests()
    }

    /// Joins the engine to its stores.
    pub fn new(engine: Engine, policies: P, entities: E) -> Self {
        Self {
            engine,
            policies,
            entities,
            store: None,
            recorder: None,
        }
    }
}

impl<P: PolicySource, E: EntitySource> Service<P, E> {
    /// Answers one question: what both transports' single evaluation
    /// reaches.
    pub(crate) async fn evaluate_one(
        &self,
        principal: &Principal,
        request: &proto::EvaluateRequest,
    ) -> Result<proto::EvaluateResponse, ServiceError> {
        self.answer(
            principal,
            Question {
                subject: request.subject.as_ref(),
                action: request.action.as_ref(),
                resource: request.resource.as_ref(),
                context: request.context.as_ref(),
                probe: request.probe,
            },
        )
        .await
    }

    async fn answer(
        &self,
        principal: &Principal,
        question: Question<'_>,
    ) -> Result<proto::EvaluateResponse, ServiceError> {
        let evaluation = question.evaluation()?;
        if !principal.asks(evaluation.action, &evaluation.resource) {
            return Err(ServiceError::OutOfScope("a question"));
        }
        let decision = self
            .engine
            .evaluate(&self.policies, &self.entities, &evaluation)
            .await?;
        if decision.outcome == Outcome::Deny && !question.probe {
            self.note_denial(principal, &evaluation, &decision).await;
        }
        Ok(response(&decision))
    }

    /// Answers one entry of a batch: a question at fault is denied
    /// there, with the fault as its reason, and only what fails the
    /// whole call fails it.
    async fn answer_entry(
        &self,
        principal: &Principal,
        question: Question<'_>,
    ) -> Result<proto::EvaluateResponse, ServiceError> {
        match self.answer(principal, question).await {
            Err(error) if error.is_the_questions() => {
                tracing::info!(error = ?error, "refused an entry of a batch");
                Ok(refused(&error))
            }
            answer => answer,
        }
    }

    pub(crate) async fn answer_batch(
        &self,
        principal: &Principal,
        request: &proto::EvaluationsRequest,
    ) -> Result<proto::EvaluationsResponse, ServiceError> {
        if request.evaluations.len() > MAX_EVALUATIONS {
            return Err(ServiceError::TooManyEvaluations {
                max: MAX_EVALUATIONS,
            });
        }
        // An empty list states one question, as AuthZEN's does: no
        // answers would read as nothing denied to a caller folding them.
        if request.evaluations.is_empty() {
            let answer = self
                .answer_entry(principal, Question::defaults_of(request))
                .await?;
            return Ok(proto::EvaluationsResponse {
                evaluations: vec![answer],
            });
        }
        let mut evaluations = Vec::with_capacity(request.evaluations.len());
        for entry in &request.evaluations {
            evaluations.push(
                self.answer_entry(principal, Question::of_entry(request, entry))
                    .await?,
            );
        }
        Ok(proto::EvaluationsResponse { evaluations })
    }
}

impl<P, E: EntitySink> Service<P, E> {
    /// Writes a batch. A type the caller does not own, or an entity
    /// outside the organization a scoped caller is bound to, refuses
    /// all of it, being the caller's mistake rather than an entry's,
    /// and any other fault refuses its own entry.
    pub(crate) async fn apply_changes(
        &self,
        principal: &Principal,
        request: &proto::ApplyEntityChangesRequest,
    ) -> Result<proto::ApplyEntityChangesResponse, ServiceError> {
        if request.changes.len() > MAX_CHANGES {
            return Err(ServiceError::TooManyChanges { max: MAX_CHANGES });
        }
        let not_owned = request
            .changes
            .iter()
            .filter_map(|change| change.entity.as_ref())
            .find(|entity| {
                !principal
                    .caller()
                    .owns(self.engine.manifests(), &entity.r#type)
            });
        if let Some(entity) = not_owned {
            return Err(ServiceError::NotOwned(entity.r#type.clone()));
        }
        if !request
            .changes
            .iter()
            .all(|change| within(principal, change))
        {
            return Err(ServiceError::OutOfScope("an entity"));
        }
        let mut changes = Vec::with_capacity(request.changes.len());
        let mut refusals = Vec::new();
        for (index, change) in request.changes.iter().enumerate() {
            match parse(&self.engine, change) {
                Ok(change) => changes.push(change),
                Err(fault) => {
                    tracing::info!(error = ?fault, index, "refused a change");
                    refusals.push(proto::EntityChangeRefusal {
                        index: count(index),
                        error: fault.to_string(),
                    });
                }
            }
        }
        let applied = self
            .entities
            .apply(&changes)
            .await
            .map_err(ServiceError::EntityWrite)?;
        Ok(proto::ApplyEntityChangesResponse {
            written: count(applied.written),
            dropped: count(applied.dropped),
            refusals,
        })
    }
}

/// A count the wire carries. `MAX_CHANGES` keeps it far below the limit
/// this saturates at.
fn count(value: usize) -> u32 {
    u32::try_from(value).unwrap_or(u32::MAX)
}

#[tonic::async_trait]
impl<P, E> PolicyService for Service<P, E>
where
    P: PolicySource + Send + Sync + 'static,
    E: EntitySource + EntitySink + Send + Sync + 'static,
{
    async fn evaluate(
        &self,
        request: Request<proto::EvaluateRequest>,
    ) -> Result<Response<proto::EvaluateResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let answer = self
            .evaluate_one(&principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(answer))
    }

    async fn evaluations(
        &self,
        request: Request<proto::EvaluationsRequest>,
    ) -> Result<Response<proto::EvaluationsResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let answers = self
            .answer_batch(&principal, &request.into_inner())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(answers))
    }

    async fn apply_entity_changes(
        &self,
        request: Request<proto::ApplyEntityChangesRequest>,
    ) -> Result<Response<proto::ApplyEntityChangesResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let applied = self
            .apply_changes(&principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(applied))
    }

    async fn create_policy(
        &self,
        request: Request<proto::CreatePolicyRequest>,
    ) -> Result<Response<proto::CreatePolicyResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let done = Service::create_policy(self, &principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(done))
    }

    async fn append_policy_version(
        &self,
        request: Request<proto::AppendPolicyVersionRequest>,
    ) -> Result<Response<proto::AppendPolicyVersionResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let done = Service::append_policy_version(self, &principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(done))
    }

    async fn activate_policy(
        &self,
        request: Request<proto::ActivatePolicyRequest>,
    ) -> Result<Response<proto::ActivatePolicyResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let done = Service::activate_policy(self, &principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(done))
    }

    async fn roll_back_policy(
        &self,
        request: Request<proto::RollBackPolicyRequest>,
    ) -> Result<Response<proto::RollBackPolicyResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let done = Service::roll_back_policy(self, &principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(done))
    }

    async fn delete_policy(
        &self,
        request: Request<proto::DeletePolicyRequest>,
    ) -> Result<Response<proto::DeletePolicyResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let done = Service::delete_policy(self, &principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(done))
    }

    async fn get_policy(
        &self,
        request: Request<proto::GetPolicyRequest>,
    ) -> Result<Response<proto::GetPolicyResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let done = Service::get_policy(self, &principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(done))
    }

    async fn list_policies(
        &self,
        request: Request<proto::ListPoliciesRequest>,
    ) -> Result<Response<proto::ListPoliciesResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let done = Service::list_policies(self, &principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(done))
    }

    async fn list_audit_records(
        &self,
        request: Request<proto::ListAuditRecordsRequest>,
    ) -> Result<Response<proto::ListAuditRecordsResponse>, Status> {
        let principal = principal_of(&request).map_err(|e| to_status(&e))?;
        let done = Service::list_audit_records(self, &principal, request.get_ref())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(done))
    }
}

/// The caller the interceptor verified, which nothing else sets.
fn principal_of<T>(request: &Request<T>) -> Result<Principal, ServiceError> {
    request
        .extensions()
        .get::<Principal>()
        .cloned()
        .ok_or(ServiceError::Unauthenticated(Refusal::NoToken))
}

/// One question with each field resolved against the request's
/// defaults.
struct Question<'a> {
    subject: Option<&'a proto::Subject>,
    action: Option<&'a proto::Action>,
    resource: Option<&'a proto::Resource>,
    context: Option<&'a prost_types::Struct>,
    /// Whether the caller states it is deciding what to show, so a
    /// denial is not recorded (ADR-0148 item 2).
    probe: bool,
}

impl<'a> Question<'a> {
    fn defaults_of(request: &'a proto::EvaluationsRequest) -> Self {
        Self {
            subject: request.subject.as_ref(),
            action: request.action.as_ref(),
            resource: request.resource.as_ref(),
            context: request.context.as_ref(),
            probe: request.probe,
        }
    }

    fn of_entry(request: &'a proto::EvaluationsRequest, entry: &'a proto::Evaluation) -> Self {
        Self {
            subject: entry.subject.as_ref().or(request.subject.as_ref()),
            action: entry.action.as_ref().or(request.action.as_ref()),
            resource: entry.resource.as_ref().or(request.resource.as_ref()),
            context: entry.context.as_ref().or(request.context.as_ref()),
            probe: entry.probe.unwrap_or(request.probe),
        }
    }

    fn evaluation(&self) -> Result<Evaluation<'a>, ServiceError> {
        let subject = self.subject.ok_or(ServiceError::Missing("subject"))?;
        let action = self.action.ok_or(ServiceError::Missing("action"))?;
        let resource = self.resource.ok_or(ServiceError::Missing("resource"))?;
        // An owner writes an entity through ApplyEntityChanges, so no
        // question carries one (ADR-0140).
        if holds_fields(resource.properties.as_ref()) {
            return Err(ServiceError::Grounds("resource properties"));
        }
        Ok(Evaluation {
            subject: EntityRef {
                entity_type: &subject.r#type,
                id: &subject.id,
            },
            action: &action.name,
            resource: EntityRef {
                entity_type: &resource.r#type,
                id: &resource.id,
            },
            context: self.context.map_or_else(|| json!({}), json_of_struct),
        })
    }
}

fn holds_fields(fields: Option<&prost_types::Struct>) -> bool {
    fields.is_some_and(|fields| !fields.fields.is_empty())
}

/// A context as the JSON object the engine reads. The schema refuses a
/// key or a type its action does not declare.
fn json_of_struct(fields: &prost_types::Struct) -> serde_json::Value {
    serde_json::Value::Object(
        fields
            .fields
            .iter()
            .map(|(name, value)| (name.clone(), json_of_value(value)))
            .collect(),
    )
}

fn json_of_value(value: &prost_types::Value) -> serde_json::Value {
    use prost_types::value::Kind;
    match &value.kind {
        None | Some(Kind::NullValue(_)) => serde_json::Value::Null,
        Some(Kind::BoolValue(flag)) => json!(flag),
        Some(Kind::NumberValue(number)) => json!(number),
        Some(Kind::StringValue(text)) => json!(text),
        Some(Kind::ListValue(list)) => {
            serde_json::Value::Array(list.values.iter().map(json_of_value).collect())
        }
        Some(Kind::StructValue(fields)) => json_of_struct(fields),
    }
}

fn refused(error: &ServiceError) -> proto::EvaluateResponse {
    proto::EvaluateResponse {
        decision: false,
        context: Some(proto::DecisionContext {
            reasons: vec![proto::Reason {
                policy_id: String::new(),
                error: error.to_string(),
            }],
        }),
    }
}

fn response(decision: &Decision) -> proto::EvaluateResponse {
    proto::EvaluateResponse {
        decision: decision.outcome == Outcome::Allow,
        context: Some(proto::DecisionContext {
            reasons: decision.reasons.iter().map(reason).collect(),
        }),
    }
}

fn reason(reason: &Reason) -> proto::Reason {
    match reason {
        Reason::Policy(policy) => proto::Reason {
            policy_id: policy.to_string(),
            error: String::new(),
        },
        Reason::Error { policy, message } => proto::Reason {
            policy_id: policy.to_string(),
            error: message.clone(),
        },
    }
}
