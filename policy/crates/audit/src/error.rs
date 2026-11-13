/// Why a record was not written or read.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The database failed.
    #[error("the audit store failed")]
    Store(#[source] sqlx::Error),
}
