use cedar_policy::EntityUid;
use decision::Engine;
use serde_json::{Map, Value, json};

use crate::error::Error;

/// One write an owner makes. `sequence` rises with every change the
/// owner makes to this entity, so a change that arrives after a later
/// one is dropped (ADR-0138).
#[derive(Debug, Clone)]
pub struct EntityChange {
    pub(crate) uid: EntityUid,
    pub(crate) sequence: i64,
    pub(crate) operation: Parsed,
}

#[derive(Debug, Clone)]
pub(crate) enum Parsed {
    Upsert {
        document: Value,
        references: Vec<EntityUid>,
    },
    Delete,
}

/// What an upsert states of an entity: where it sits, who belongs to
/// it, and the one attribute each of two types carries.
#[derive(Debug, Clone, Default)]
pub struct Upsert {
    /// The organization a resource names, which an `Org` names as
    /// itself.
    pub org: Option<EntityUid>,
    /// The `OrgMembers` an `Org` names as its own.
    pub members: Option<EntityUid>,
    /// What the entity sits in.
    pub parents: Vec<EntityUid>,
    /// The organizations an `Org` has granted its half of a message
    /// toward (ADR-0147). Only an `Org` carries any.
    pub consents: Vec<EntityUid>,
    /// The operations an `Operator`'s delegation grants, as action
    /// names in their wire form (ADR-0146). Only an `Operator`
    /// carries any, and each is an action an `Operator` may be the
    /// principal of.
    pub operations: Vec<String>,
}

impl EntityChange {
    /// The entity now reads as this. An entity carries containment and
    /// membership and, on two types, one attribute each, so these are
    /// all it can state: `org`, which every resource type requires,
    /// `members`, which an `Org` requires, its parents, an `Org`'s
    /// `consents` and an `Operator`'s `operations`. Refused where the
    /// schema does not admit it, or where the attribute belongs to
    /// another type.
    pub fn upsert(
        engine: &Engine,
        uid: EntityUid,
        sequence: i64,
        upsert: Upsert,
    ) -> Result<Self, Error> {
        let Upsert {
            org,
            members,
            parents,
            consents,
            operations,
        } = upsert;
        let entity_type = uid.type_name().to_string();
        if entity_type == "Iam::Org" && !carries_itself(&uid, &org, &members) {
            return Err(Error::OrgNotItself);
        }
        if entity_type != "Iam::Org" && !consents.is_empty() {
            return Err(Error::Attribute("consents"));
        }
        if entity_type != "Iam::Operator" && !operations.is_empty() {
            return Err(Error::Attribute("operations"));
        }
        if let Some(operation) = operations
            .iter()
            .find(|operation| !engine.manifests().delegable(operation))
        {
            return Err(Error::NotDelegable(operation.clone()));
        }
        let mut attrs: Map<String, Value> = [("org", &org), ("members", &members)]
            .into_iter()
            .filter_map(|(name, value)| {
                let value = value.as_ref()?;
                Some((name.to_owned(), json!({"__entity": uid_json(value)})))
            })
            .collect();
        if !consents.is_empty() {
            let consents: Vec<Value> = consents
                .iter()
                .map(|org| json!({"__entity": uid_json(org)}))
                .collect();
            attrs.insert("consents".to_owned(), Value::Array(consents));
        }
        if !operations.is_empty() {
            attrs.insert("operations".to_owned(), json!(operations));
        }
        let document = json!({
            "uid": uid_json(&uid),
            "attrs": attrs,
            "parents": parents.iter().map(uid_json).collect::<Vec<_>>(),
        });
        engine.entities(json!([document])).map_err(Error::Refused)?;
        let references = parents.into_iter().chain(org).chain(members).collect();
        Ok(Self {
            uid,
            sequence,
            operation: Parsed::Upsert {
                document,
                references,
            },
        })
    }

    /// The owner no longer has the entity.
    pub fn delete(uid: EntityUid, sequence: i64) -> Self {
        Self {
            uid,
            sequence,
            operation: Parsed::Delete,
        }
    }
}

/// `resource.org.members` is what tenant isolation reads, so an `Org`
/// naming another's members would lend that organization's members
/// this one.
fn carries_itself(uid: &EntityUid, org: &Option<EntityUid>, members: &Option<EntityUid>) -> bool {
    org.as_ref() == Some(uid)
        && members.as_ref().is_some_and(|members| {
            members.type_name().to_string() == "Iam::OrgMembers" && members.id() == uid.id()
        })
}

fn uid_json(uid: &EntityUid) -> Value {
    json!({"type": uid.type_name().to_string(), "id": uid.id().unescaped()})
}
