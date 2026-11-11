use std::str::FromStr;

use cedar_policy::{EntityId, EntityTypeName, EntityUid, ParseErrors};
use decision::Engine;
use entity::EntityChange;

use crate::proto;
use crate::proto::entity_change::Operation;

/// The longest entity id, in bytes: far under what a btree key holds,
/// and past any GRN.
const MAX_ID_BYTES: usize = 512;
/// The longest entity type, in bytes.
const MAX_TYPE_BYTES: usize = 64;
/// The most parents one change names.
const MAX_PARENTS: usize = 1000;

/// Why one change of a batch is refused.
#[derive(Debug, thiserror::Error)]
pub(crate) enum Fault {
    /// A required field is absent.
    #[error("{0} is required")]
    Missing(&'static str),
    /// A sequence of zero is what an unset field reads as, and no
    /// later change could be told from it.
    #[error("the sequence is not above zero")]
    Sequence,
    /// No store can hold an id with a NUL, and an empty one names
    /// nothing.
    #[error("an entity id is empty or holds a NUL")]
    Id,
    /// A type or an id past what a store key holds.
    #[error("an entity type or id is too long")]
    Length,
    /// More parents than one change may name.
    #[error("a change names more than {MAX_PARENTS} parents")]
    Parents,
    /// A type that does not parse.
    #[error("an entity type does not parse")]
    Type(#[source] Box<ParseErrors>),
    /// The entity does not fit the schema.
    #[error(transparent)]
    Entity(entity::Error),
}

/// Reads a change off the wire.
pub(crate) fn parse(engine: &Engine, change: &proto::EntityChange) -> Result<EntityChange, Fault> {
    if change.sequence <= 0 {
        return Err(Fault::Sequence);
    }
    let entity = change.entity.as_ref().ok_or(Fault::Missing("entity"))?;
    if let Some(Operation::Upsert(upsert)) = change.operation.as_ref()
        && upsert.parents.len() > MAX_PARENTS
    {
        return Err(Fault::Parents);
    }
    let entity = uid(entity)?;
    match change.operation.as_ref() {
        Some(Operation::Upsert(upsert)) => EntityChange::upsert(
            engine,
            entity,
            change.sequence,
            upsert.org.as_ref().map(uid).transpose()?,
            upsert.members.as_ref().map(uid).transpose()?,
            upsert.parents.iter().map(uid).collect::<Result<_, _>>()?,
        )
        .map_err(Fault::Entity),
        Some(Operation::Delete(_)) => Ok(EntityChange::delete(entity, change.sequence)),
        None => Err(Fault::Missing("operation")),
    }
}

fn uid(entity: &proto::EntityRef) -> Result<EntityUid, Fault> {
    if entity.id.len() > MAX_ID_BYTES || entity.r#type.len() > MAX_TYPE_BYTES {
        return Err(Fault::Length);
    }
    if entity.id.is_empty() || entity.id.contains('\0') {
        return Err(Fault::Id);
    }
    let name =
        EntityTypeName::from_str(&entity.r#type).map_err(|error| Fault::Type(Box::new(error)))?;
    Ok(EntityUid::from_type_name_and_id(
        name,
        EntityId::new(&entity.id),
    ))
}
