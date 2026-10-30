use decision::{
    Decision, Engine, EntityRef, EntitySource, Evaluation, Outcome, PolicySource, Reason,
};
use prost_types::value::Kind;
use serde_json::{Map, Number, Value, json};
use tonic::{Request, Response, Status};

use crate::proto;
use crate::proto::policy_service_server::PolicyService;
use crate::status::to_status;

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
    async fn answer(&self, question: Question<'_>) -> Result<proto::EvaluateResponse, Status> {
        let decision = self
            .engine
            .evaluate(&self.policies, &self.entities, &question.evaluation()?)
            .await
            .map_err(to_status)?;
        Ok(response(&decision))
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
        let answer = self
            .answer(Question {
                subject: request.subject.as_ref(),
                action: request.action.as_ref(),
                resource: request.resource.as_ref(),
                context: request.context.as_ref(),
            })
            .await?;
        Ok(Response::new(answer))
    }

    async fn evaluations(
        &self,
        request: Request<proto::EvaluationsRequest>,
    ) -> Result<Response<proto::EvaluationsResponse>, Status> {
        let request = request.into_inner();
        let mut evaluations = Vec::with_capacity(request.evaluations.len());
        for entry in &request.evaluations {
            let answer = self
                .answer(Question {
                    subject: entry.subject.as_ref().or(request.subject.as_ref()),
                    action: entry.action.as_ref().or(request.action.as_ref()),
                    resource: entry.resource.as_ref().or(request.resource.as_ref()),
                    context: entry.context.as_ref().or(request.context.as_ref()),
                })
                .await?;
            evaluations.push(answer);
        }
        Ok(Response::new(proto::EvaluationsResponse { evaluations }))
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
    fn evaluation(&self) -> Result<Evaluation<'a>, Status> {
        let subject = self
            .subject
            .ok_or_else(|| Status::invalid_argument("subject is required"))?;
        let action = self
            .action
            .ok_or_else(|| Status::invalid_argument("action is required"))?;
        let resource = self
            .resource
            .ok_or_else(|| Status::invalid_argument("resource is required"))?;
        // A caller sends no grounds until it can be shown to own the
        // type (ADR-0130 §8), which arrives with ApplyEntityChanges.
        if resource
            .properties
            .as_ref()
            .is_some_and(|properties| !properties.fields.is_empty())
        {
            return Err(Status::invalid_argument(
                "resource properties are not accepted",
            ));
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
            context: self.context.map_or_else(|| json!({}), object),
        })
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

fn object(fields: &prost_types::Struct) -> Value {
    Value::Object(
        fields
            .fields
            .iter()
            .map(|(name, value)| (name.clone(), json_value(value)))
            .collect::<Map<_, _>>(),
    )
}

fn json_value(value: &prost_types::Value) -> Value {
    match &value.kind {
        None | Some(Kind::NullValue(_)) => Value::Null,
        Some(Kind::BoolValue(value)) => Value::Bool(*value),
        Some(Kind::StringValue(value)) => Value::String(value.clone()),
        Some(Kind::NumberValue(value)) => number(*value),
        Some(Kind::StructValue(value)) => object(value),
        Some(Kind::ListValue(list)) => Value::Array(list.values.iter().map(json_value).collect()),
    }
}

/// `Struct` carries every number as a double, and Cedar has only
/// integers, so a whole number travels as one.
fn number(value: f64) -> Value {
    #[allow(clippy::cast_possible_truncation)]
    let whole = value as i64;
    #[allow(clippy::cast_precision_loss)]
    if whole as f64 == value {
        return Value::Number(whole.into());
    }
    Number::from_f64(value).map_or(Value::Null, Value::Number)
}
