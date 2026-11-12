use std::collections::BTreeMap;

use serde::de::DeserializeOwned;
use serde::{Deserialize, Deserializer, Serialize};
use serde_json::{Map, Value};

use crate::proto;
use crate::proto::entity_change::Operation;
use crate::status::ServiceError;

/// The semantic AuthZEN 1.0 §7.1.2.1 makes the default, and the one
/// this binding serves.
const EXECUTE_ALL: &str = "execute_all";

/// Reads a body as the request its route takes.
pub(super) fn read<T: DeserializeOwned>(body: &[u8]) -> Result<T, ServiceError> {
    serde_json::from_slice(body).map_err(|error| ServiceError::Malformed(error.to_string()))
}

/// An entity as AuthZEN names it. An absent `type` or `id` reads as
/// empty, as an unset protobuf string does, so both transports refuse
/// it in the same place.
#[derive(Debug, Default, Deserialize)]
struct Entity {
    #[serde(default)]
    r#type: String,
    #[serde(default)]
    id: String,
    properties: Option<Map<String, Value>>,
}

#[derive(Debug, Default, Deserialize)]
struct Action {
    #[serde(default)]
    name: String,
    properties: Option<Map<String, Value>>,
}

/// One access evaluation: a request's body, or one entry of its
/// `evaluations`.
#[derive(Debug, Default, Deserialize)]
pub(super) struct Evaluation {
    subject: Option<Entity>,
    action: Option<Action>,
    resource: Option<Entity>,
    context: Option<Map<String, Value>>,
}

/// AuthZEN's request of the Access Evaluations API.
#[derive(Debug, Default, Deserialize)]
pub(super) struct Evaluations {
    subject: Option<Entity>,
    action: Option<Action>,
    resource: Option<Entity>,
    context: Option<Map<String, Value>>,
    options: Option<Options>,
    evaluations: Option<Vec<Evaluation>>,
}

#[derive(Debug, Default, Deserialize)]
struct Options {
    evaluations_semantic: Option<String>,
}

impl Evaluation {
    pub(super) fn into_proto(self) -> Result<proto::EvaluateRequest, ServiceError> {
        Ok(proto::EvaluateRequest {
            subject: subject(self.subject)?,
            action: action(self.action)?,
            resource: self.resource.map(resource),
            context: self.context.map(structure),
            read: 0,
        })
    }
}

impl Evaluations {
    pub(super) fn into_proto(self) -> Result<proto::EvaluationsRequest, ServiceError> {
        let semantic = self
            .options
            .and_then(|options| options.evaluations_semantic)
            .unwrap_or_else(|| EXECUTE_ALL.to_owned());
        if semantic != EXECUTE_ALL {
            return Err(ServiceError::Malformed(format!(
                "only the {EXECUTE_ALL} evaluations_semantic is served"
            )));
        }
        let evaluations = self
            .evaluations
            .unwrap_or_default()
            .into_iter()
            .map(|entry| {
                Ok(proto::Evaluation {
                    subject: subject(entry.subject)?,
                    action: action(entry.action)?,
                    resource: entry.resource.map(resource),
                    context: entry.context.map(structure),
                })
            })
            .collect::<Result<_, ServiceError>>()?;
        Ok(proto::EvaluationsRequest {
            subject: subject(self.subject)?,
            action: action(self.action)?,
            resource: self.resource.map(resource),
            context: self.context.map(structure),
            read: 0,
            evaluations,
        })
    }
}

/// A subject carries no properties: they are the grounds a decision
/// reads for itself, and the contract has no field for them.
fn subject(subject: Option<Entity>) -> Result<Option<proto::Subject>, ServiceError> {
    subject
        .map(|subject| {
            refuse_properties(subject.properties.as_ref(), "subject properties")?;
            Ok(proto::Subject {
                r#type: subject.r#type,
                id: subject.id,
            })
        })
        .transpose()
}

fn action(action: Option<Action>) -> Result<Option<proto::Action>, ServiceError> {
    action
        .map(|action| {
            refuse_properties(action.properties.as_ref(), "action properties")?;
            Ok(proto::Action { name: action.name })
        })
        .transpose()
}

fn refuse_properties(
    properties: Option<&Map<String, Value>>,
    what: &'static str,
) -> Result<(), ServiceError> {
    match properties {
        Some(properties) if !properties.is_empty() => Err(ServiceError::Grounds(what)),
        _ => Ok(()),
    }
}

fn resource(resource: Entity) -> proto::Resource {
    proto::Resource {
        r#type: resource.r#type,
        id: resource.id,
        properties: resource.properties.map(structure),
    }
}

fn structure(object: Map<String, Value>) -> prost_types::Struct {
    prost_types::Struct {
        fields: object
            .into_iter()
            .map(|(name, value)| (name, value_of(value)))
            .collect::<BTreeMap<_, _>>(),
    }
}

fn value_of(value: Value) -> prost_types::Value {
    use prost_types::value::Kind;
    let kind = match value {
        Value::Null => Kind::NullValue(0),
        Value::Bool(flag) => Kind::BoolValue(flag),
        Value::Number(number) => Kind::NumberValue(number.as_f64().unwrap_or_default()),
        Value::String(text) => Kind::StringValue(text),
        Value::Array(items) => Kind::ListValue(prost_types::ListValue {
            values: items.into_iter().map(value_of).collect(),
        }),
        Value::Object(object) => Kind::StructValue(structure(object)),
    };
    prost_types::Value { kind: Some(kind) }
}

/// AuthZEN's Decision (§5.5), with the reasons the gRPC answer carries
/// under `context`.
#[derive(Debug, Serialize)]
pub(super) struct Decision {
    decision: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    context: Option<Context>,
}

#[derive(Debug, Serialize)]
struct Context {
    reasons: Vec<Reason>,
}

/// A reason names its policy, or its error, and omits what is empty.
#[derive(Debug, Serialize)]
struct Reason {
    #[serde(skip_serializing_if = "String::is_empty")]
    policy_id: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    error: String,
}

impl From<&proto::EvaluateResponse> for Decision {
    fn from(answer: &proto::EvaluateResponse) -> Self {
        Self {
            decision: answer.decision,
            context: answer.context.as_ref().map(|context| Context {
                reasons: context
                    .reasons
                    .iter()
                    .map(|reason| Reason {
                        policy_id: reason.policy_id.clone(),
                        error: reason.error.clone(),
                    })
                    .collect(),
            }),
        }
    }
}

/// AuthZEN's response of the Access Evaluations API (§7.2).
#[derive(Debug, Serialize)]
pub(super) struct Decisions {
    evaluations: Vec<Decision>,
}

impl From<&proto::EvaluationsResponse> for Decisions {
    fn from(answers: &proto::EvaluationsResponse) -> Self {
        Self {
            evaluations: answers.evaluations.iter().map(Decision::from).collect(),
        }
    }
}

/// AuthZEN's Policy Decision Point metadata (§9.1.1), naming the two
/// endpoints this binding serves.
#[derive(Debug, Serialize)]
pub(super) struct Metadata {
    pub(super) policy_decision_point: String,
    pub(super) access_evaluation_endpoint: String,
    pub(super) access_evaluations_endpoint: String,
}

/// The JSON form of `ApplyEntityChangesRequest`.
#[derive(Debug, Default, Deserialize)]
pub(super) struct Changes {
    changes: Option<Vec<Change>>,
}

#[derive(Debug, Deserialize)]
struct Change {
    entity: Option<EntityRef>,
    #[serde(default, deserialize_with = "int64")]
    sequence: i64,
    upsert: Option<Upsert>,
    delete: Option<Delete>,
}

#[derive(Debug, Deserialize)]
struct EntityRef {
    #[serde(default)]
    r#type: String,
    #[serde(default)]
    id: String,
}

#[derive(Debug, Default, Deserialize)]
struct Upsert {
    org: Option<EntityRef>,
    members: Option<EntityRef>,
    parents: Option<Vec<EntityRef>>,
    consents: Option<Vec<EntityRef>>,
    operations: Option<Vec<String>>,
}

#[derive(Debug, Default, Deserialize)]
struct Delete {}

/// An `int64` in protobuf's JSON form is a string, and a number is
/// accepted too.
fn int64<'de, D: Deserializer<'de>>(deserializer: D) -> Result<i64, D::Error> {
    #[derive(Deserialize)]
    #[serde(untagged)]
    enum Int64 {
        Number(i64),
        Text(String),
    }
    match Int64::deserialize(deserializer)? {
        Int64::Number(number) => Ok(number),
        Int64::Text(text) => text.parse().map_err(serde::de::Error::custom),
    }
}

fn entity_ref(reference: EntityRef) -> proto::EntityRef {
    proto::EntityRef {
        r#type: reference.r#type,
        id: reference.id,
    }
}

fn entity_refs(references: Option<Vec<EntityRef>>) -> Vec<proto::EntityRef> {
    references
        .unwrap_or_default()
        .into_iter()
        .map(entity_ref)
        .collect()
}

impl Changes {
    pub(super) fn into_proto(self) -> Result<proto::ApplyEntityChangesRequest, ServiceError> {
        let changes = self
            .changes
            .unwrap_or_default()
            .into_iter()
            .map(|change| {
                let operation = match (change.upsert, change.delete) {
                    (Some(_), Some(_)) => {
                        return Err(ServiceError::Malformed(
                            "a change is an upsert or a delete, not both".to_owned(),
                        ));
                    }
                    (Some(upsert), None) => Some(Operation::Upsert(proto::EntityUpsert {
                        org: upsert.org.map(entity_ref),
                        members: upsert.members.map(entity_ref),
                        parents: entity_refs(upsert.parents),
                        consents: entity_refs(upsert.consents),
                        operations: upsert.operations.unwrap_or_default(),
                    })),
                    (None, Some(Delete {})) => Some(Operation::Delete(proto::EntityDelete {})),
                    (None, None) => None,
                };
                Ok(proto::EntityChange {
                    entity: change.entity.map(entity_ref),
                    sequence: change.sequence,
                    operation,
                })
            })
            .collect::<Result<_, ServiceError>>()?;
        Ok(proto::ApplyEntityChangesRequest { changes })
    }
}

/// The JSON form of `ApplyEntityChangesResponse`.
#[derive(Debug, Serialize)]
pub(super) struct Applied {
    written: u32,
    dropped: u32,
    refusals: Vec<Refusal>,
}

#[derive(Debug, Serialize)]
struct Refusal {
    index: u32,
    error: String,
}

impl From<&proto::ApplyEntityChangesResponse> for Applied {
    fn from(applied: &proto::ApplyEntityChangesResponse) -> Self {
        Self {
            written: applied.written,
            dropped: applied.dropped,
            refusals: applied
                .refusals
                .iter()
                .map(|refusal| Refusal {
                    index: refusal.index,
                    error: refusal.error.clone(),
                })
                .collect(),
        }
    }
}
