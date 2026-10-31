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

impl EntityChange {
    /// The entity now reads as this. An entity carries containment and
    /// membership and nothing else, so these are all it can state:
    /// `org`, which every resource type requires, `members`, which an
    /// `Org` requires, and its parents. Refused where the schema does
    /// not admit it.
    pub fn upsert(
        engine: &Engine,
        uid: EntityUid,
        sequence: i64,
        org: Option<EntityUid>,
        members: Option<EntityUid>,
        parents: Vec<EntityUid>,
    ) -> Result<Self, Error> {
        if uid.type_name().to_string() == "Org" && !carries_itself(&uid, &org, &members) {
            return Err(Error::OrgNotItself);
        }
        let attrs: Map<String, Value> = [("org", &org), ("members", &members)]
            .into_iter()
            .filter_map(|(name, value)| {
                let value = value.as_ref()?;
                Some((name.to_owned(), json!({"__entity": uid_json(value)})))
            })
            .collect();
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
            members.type_name().to_string() == "OrgMembers" && members.id() == uid.id()
        })
}

fn uid_json(uid: &EntityUid) -> Value {
    json!({"type": uid.type_name().to_string(), "id": uid.id().unescaped()})
}
