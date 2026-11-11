use std::future::Future;

use entity::{Applied, EntityChange, EntityStore, Error};

/// Where `ApplyEntityChanges` writes.
pub trait EntitySink {
    /// Writes a batch in one transaction.
    fn apply(
        &self,
        changes: &[EntityChange],
    ) -> impl Future<Output = Result<Applied, Error>> + Send;
}

impl EntitySink for EntityStore {
    async fn apply(&self, changes: &[EntityChange]) -> Result<Applied, Error> {
        EntityStore::apply(self, changes).await
    }
}
