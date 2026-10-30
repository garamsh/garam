use decision::{
    Decision, Engine, EntityRef, EntitySource, Evaluation, Outcome, PolicySource, Reason,
};
use serde_json::json;
use tonic::{Request, Response, Status};

use crate::proto;
use crate::proto::policy_service_server::PolicyService;
use crate::status::{ServiceError, to_status};

/// The most evaluations one request holds: a page, not a table.
const MAX_EVALUATIONS: usize = 1000;

/// Serves `garam.policy.v1`. Every read goes to storage, as no cache
/// exists yet, so both `Read` options answer what `READ_CURRENT`
/// promises.
#[derive(Debug)]
pub struct Service<P, E> {
    engine: Engine,
    policies: P,
    entities: E,
}

impl<P, E> Service<P, E> {
    /// Joins the engine to its stores.
    pub fn new(engine: Engine, policies: P, entities: E) -> Self {
        Self {
            engine,
            policies,
            entities,
        }
    }
}

impl<P: PolicySource, E: EntitySource> Service<P, E> {
    async fn answer(
        &self,
        question: Question<'_>,
    ) -> Result<proto::EvaluateResponse, ServiceError> {
        let decision = self
            .engine
            .evaluate(&self.policies, &self.entities, &question.evaluation()?)
            .await?;
        Ok(response(&decision))
    }

    /// Answers one entry of a batch: a question at fault is denied
    /// there, with the fault as its reason, and only what fails the
    /// whole call fails it.
    async fn answer_entry(
        &self,
        question: Question<'_>,
    ) -> Result<proto::EvaluateResponse, ServiceError> {
        match self.answer(question).await {
            Err(error) if error.is_the_questions() => {
                tracing::info!(error = ?error, "refused an entry of a batch");
                Ok(refused(&error))
            }
            answer => answer,
        }
    }

    async fn answer_batch(
        &self,
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
            let answer = self.answer_entry(Question::defaults_of(request)).await?;
            return Ok(proto::EvaluationsResponse {
                evaluations: vec![answer],
            });
        }
        let mut evaluations = Vec::with_capacity(request.evaluations.len());
        for entry in &request.evaluations {
            evaluations.push(
                self.answer_entry(Question::of_entry(request, entry))
                    .await?,
            );
        }
        Ok(proto::EvaluationsResponse { evaluations })
    }
}

#[tonic::async_trait]
impl<P, E> PolicyService for Service<P, E>
where
    P: PolicySource + Send + Sync + 'static,
    E: EntitySource + Send + Sync + 'static,
{
    async fn evaluate(
        &self,
        request: Request<proto::EvaluateRequest>,
    ) -> Result<Response<proto::EvaluateResponse>, Status> {
        let request = request.into_inner();
        let question = Question {
            subject: request.subject.as_ref(),
            action: request.action.as_ref(),
            resource: request.resource.as_ref(),
            context: request.context.as_ref(),
        };
        let answer = self.answer(question).await.map_err(|e| to_status(&e))?;
        Ok(Response::new(answer))
    }

    async fn evaluations(
        &self,
        request: Request<proto::EvaluationsRequest>,
    ) -> Result<Response<proto::EvaluationsResponse>, Status> {
        let answers = self
            .answer_batch(&request.into_inner())
            .await
            .map_err(|e| to_status(&e))?;
        Ok(Response::new(answers))
    }
}

/// One question with each field resolved against the request's
/// defaults.
struct Question<'a> {
    subject: Option<&'a proto::Subject>,
    action: Option<&'a proto::Action>,
    resource: Option<&'a proto::Resource>,
    context: Option<&'a prost_types::Struct>,
}

impl<'a> Question<'a> {
    fn defaults_of(request: &'a proto::EvaluationsRequest) -> Self {
        Self {
            subject: request.subject.as_ref(),
            action: request.action.as_ref(),
            resource: request.resource.as_ref(),
            context: request.context.as_ref(),
        }
    }

    fn of_entry(request: &'a proto::EvaluationsRequest, entry: &'a proto::Evaluation) -> Self {
        Self {
            subject: entry.subject.as_ref().or(request.subject.as_ref()),
            action: entry.action.as_ref().or(request.action.as_ref()),
            resource: entry.resource.as_ref().or(request.resource.as_ref()),
            context: entry.context.as_ref().or(request.context.as_ref()),
        }
    }

    fn evaluation(&self) -> Result<Evaluation<'a>, ServiceError> {
        let subject = self.subject.ok_or(ServiceError::Missing("subject"))?;
        let action = self.action.ok_or(ServiceError::Missing("action"))?;
        let resource = self.resource.ok_or(ServiceError::Missing("resource"))?;
        // A caller sends no grounds until it can be shown to own the
        // type (ADR-0130 §8), which arrives with ApplyEntityChanges.
        if holds_fields(resource.properties.as_ref()) {
            return Err(ServiceError::Grounds("resource properties"));
        }
        // No action of the schema declares a context, so none has one.
        if holds_fields(self.context) {
            return Err(ServiceError::Grounds("context"));
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
            context: json!({}),
        })
    }
}

fn holds_fields(fields: Option<&prost_types::Struct>) -> bool {
    fields.is_some_and(|fields| !fields.fields.is_empty())
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
