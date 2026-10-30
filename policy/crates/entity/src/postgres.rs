use cedar_policy::EntityUid;
use decision::{Cause, Engine, EntitySlice, EntitySource, OrgGrn};
use serde_json::Value;
use sqlx::types::Json;
use sqlx::{PgPool, Postgres, Row, Transaction};

use crate::change::{EntityChange, Parsed};
use crate::error::Error;

/// How many changes of a batch took effect.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Applied {
    /// Changes that were written.
    pub written: usize,
    /// Changes dropped, the store holding this sequence or a later one.
    pub dropped: usize,
}

/// The entity store in the service's own database.
#[derive(Debug, Clone)]
pub struct EntityStore {
    pool: PgPool,
    engine: Engine,
}

impl EntityStore {
    /// A store over this pool, parsing what it reads with this engine.
    pub fn new(pool: PgPool, engine: Engine) -> Self {
        Self { pool, engine }
    }

    /// Writes a batch in one transaction. A change at or below the
    /// stored sequence is dropped, so a replayed or reordered delivery
    /// changes nothing.
    pub async fn apply(&self, changes: &[EntityChange]) -> Result<Applied, Error> {
        let mut transaction = self.pool.begin().await.map_err(Error::Store)?;
        let mut applied = Applied {
            written: 0,
            dropped: 0,
        };
        for change in changes {
            if write(&mut transaction, change).await? {
                applied.written += 1;
            } else {
                applied.dropped += 1;
            }
        }
        transaction.commit().await.map_err(Error::Store)?;
        Ok(applied)
    }

    /// Reads the principal, the resource and everything each reaches.
    pub async fn read_slice(
        &self,
        principal: &EntityUid,
        resource: &EntityUid,
    ) -> Result<EntitySlice, Error> {
        let rows = sqlx::query(
            "WITH RECURSIVE reached (entity_type, entity_id) AS (
                 VALUES ($1::text, $2::text), ($3::text, $4::text)
                 UNION
                 SELECT target_type, target_id
                 FROM entity_references
                 JOIN reached USING (entity_type, entity_id)
             )
             SELECT entities.entity_type, entities.entity_id, entities.document
             FROM entities
             JOIN reached USING (entity_type, entity_id)
             WHERE entities.document IS NOT NULL",
        )
        .bind(principal.type_name().to_string())
        .bind(principal.id().unescaped())
        .bind(resource.type_name().to_string())
        .bind(resource.id().unescaped())
        .fetch_all(&self.pool)
        .await
        .map_err(Error::Store)?;

        let mut documents = Vec::with_capacity(rows.len());
        let mut org = None;
        for row in rows {
            let document: Json<Value> = row.try_get("document").map_err(Error::Store)?;
            let entity_type: String = row.try_get("entity_type").map_err(Error::Store)?;
            let entity_id: String = row.try_get("entity_id").map_err(Error::Store)?;
            if entity_type == resource.type_name().to_string()
                && entity_id == resource.id().unescaped()
            {
                org = document
                    .0
                    .pointer("/attrs/org/__entity/id")
                    .and_then(Value::as_str)
                    .map(OrgGrn::new);
            }
            documents.push(document.0);
        }
        let entities = self
            .engine
            .entities(Value::Array(documents))
            .map_err(Error::Stored)?;
        Ok(EntitySlice { entities, org })
    }
}

impl EntitySource for EntityStore {
    async fn slice(
        &self,
        principal: &EntityUid,
        resource: &EntityUid,
    ) -> Result<EntitySlice, Cause> {
        self.read_slice(principal, resource)
            .await
            .map_err(Cause::from)
    }
}

/// Writes one change, answering whether it took effect.
async fn write(
    transaction: &mut Transaction<'_, Postgres>,
    change: &EntityChange,
) -> Result<bool, Error> {
    let entity_type = change.uid.type_name().to_string();
    let entity_id = change.uid.id().unescaped();
    let (document, references): (Option<&Value>, &[EntityUid]) = match &change.operation {
        Parsed::Upsert {
            document,
            references,
        } => (Some(document), references),
        Parsed::Delete => (None, &[]),
    };
    let written = sqlx::query(
        "INSERT INTO entities (entity_type, entity_id, sequence, document)
         VALUES ($1, $2, $3, $4)
         ON CONFLICT (entity_type, entity_id) DO UPDATE
           SET sequence = EXCLUDED.sequence, document = EXCLUDED.document
           WHERE entities.sequence < EXCLUDED.sequence",
    )
    .bind(&entity_type)
    .bind(entity_id)
    .bind(change.sequence)
    .bind(document.map(Json))
    .execute(&mut **transaction)
    .await
    .map_err(Error::Store)?;
    if written.rows_affected() == 0 {
        return Ok(false);
    }
    sqlx::query("DELETE FROM entity_references WHERE entity_type = $1 AND entity_id = $2")
        .bind(&entity_type)
        .bind(entity_id)
        .execute(&mut **transaction)
        .await
        .map_err(Error::Store)?;
    for target in references {
        sqlx::query(
            "INSERT INTO entity_references (entity_type, entity_id, target_type, target_id)
             VALUES ($1, $2, $3, $4)
             ON CONFLICT DO NOTHING",
        )
        .bind(&entity_type)
        .bind(entity_id)
        .bind(target.type_name().to_string())
        .bind(target.id().unescaped())
        .execute(&mut **transaction)
        .await
        .map_err(Error::Store)?;
    }
    Ok(true)
}
