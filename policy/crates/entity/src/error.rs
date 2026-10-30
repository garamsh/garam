/// Why a write or a read of the entity store failed.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The entity does not fit the schema.
    #[error("the entity does not fit the schema")]
    Refused(#[source] decision::Error),
    /// A stored document no longer parses.
    #[error("the stored entities do not fit the schema")]
    Stored(#[source] decision::Error),
    /// The store failed.
    #[error("the entity store failed")]
    Store(#[source] sqlx::Error),
}
