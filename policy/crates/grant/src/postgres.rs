use decision::{Cause, OrgGrn, OrgPolicies, PolicySource};
use sqlx::types::Json;
use sqlx::{PgPool, Postgres, Row, Transaction};

use crate::body::{Body, Stored, StoredPolicy};
use crate::error::Error;

/// Whether a policy decides anything.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Status {
    /// Stored, and evaluated by nothing.
    Draft,
    /// Its default version is composed into decisions.
    Active,
}

/// A policy's first version.
#[derive(Debug, Clone)]
pub struct NewPolicy<'a> {
    /// The policy's GRN.
    pub grn: &'a str,
    /// The organization holding it.
    pub org: &'a OrgGrn,
    /// Its name.
    pub name: &'a str,
    /// What it says.
    pub body: Body,
    /// Who wrote it.
    pub created_by: &'a str,
}

/// One stored version of a policy.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Version {
    /// Its number, from 1.
    pub version: i32,
    /// What it says.
    pub body: Body,
    /// Who wrote it.
    pub created_by: String,
}

/// The policies in the service's own database.
#[derive(Debug, Clone)]
pub struct PolicyStore {
    pool: PgPool,
}

impl PolicyStore {
    /// A store over this pool.
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }

    /// Stores a policy as a draft holding version 1. Its GRN is under
    /// its organization, so no organization squats another's.
    pub async fn create(&self, policy: NewPolicy<'_>) -> Result<(), Error> {
        owned(policy.org, policy.grn)?;
        let mut transaction = self.pool.begin().await.map_err(Error::Store)?;
        let inserted = sqlx::query(
            "INSERT INTO policies (grn, org_grn, name, status, default_version)
             VALUES ($1, $2, $3, 'draft', 1)
             ON CONFLICT (grn) DO NOTHING",
        )
        .bind(policy.grn)
        .bind(policy.org.as_str())
        .bind(policy.name)
        .execute(&mut *transaction)
        .await
        .map_err(Error::Store)?;
        if inserted.rows_affected() == 0 {
            return Err(Error::AlreadyExists(policy.grn.to_owned()));
        }
        insert_version(
            &mut transaction,
            policy.grn,
            1,
            &policy.body,
            policy.created_by,
        )
        .await?;
        transaction.commit().await.map_err(Error::Store)
    }

    /// Appends a version and leaves the default where it is. Answers
    /// the new version's number.
    pub async fn append_version(
        &self,
        org: &OrgGrn,
        grn: &str,
        body: &Body,
        created_by: &str,
    ) -> Result<i32, Error> {
        owned(org, grn)?;
        let mut transaction = self.pool.begin().await.map_err(Error::Store)?;
        // The row lock orders two appends to one policy.
        sqlx::query("SELECT 1 FROM policies WHERE grn = $1 FOR UPDATE")
            .bind(grn)
            .fetch_optional(&mut *transaction)
            .await
            .map_err(Error::Store)?
            .ok_or_else(|| Error::NotFound(grn.to_owned()))?;
        let version: i32 = sqlx::query_scalar(
            "SELECT max(version) + 1 FROM policy_versions WHERE policy_grn = $1",
        )
        .bind(grn)
        .fetch_one(&mut *transaction)
        .await
        .map_err(Error::Store)?;
        insert_version(&mut transaction, grn, version, body, created_by).await?;
        transaction.commit().await.map_err(Error::Store)?;
        Ok(version)
    }

    /// Moves the default to any stored version, older or newer.
    pub async fn roll_back(&self, org: &OrgGrn, grn: &str, version: i32) -> Result<(), Error> {
        owned(org, grn)?;
        let moved = sqlx::query(
            "UPDATE policies SET default_version = $2, updated_at = now()
             WHERE grn = $1
               AND EXISTS (SELECT 1 FROM policy_versions
                           WHERE policy_grn = $1 AND version = $2)",
        )
        .bind(grn)
        .bind(version)
        .execute(&self.pool)
        .await
        .map_err(Error::Store)?;
        if moved.rows_affected() == 0 {
            return Err(Error::NoSuchVersion {
                policy: grn.to_owned(),
                version,
            });
        }
        Ok(())
    }

    /// Makes a policy decide, and leaves its default where it is.
    pub async fn activate(&self, org: &OrgGrn, grn: &str) -> Result<(), Error> {
        owned(org, grn)?;
        let updated =
            sqlx::query("UPDATE policies SET status = 'active', updated_at = now() WHERE grn = $1")
                .bind(grn)
                .execute(&self.pool)
                .await
                .map_err(Error::Store)?;
        if updated.rows_affected() == 0 {
            return Err(Error::NotFound(grn.to_owned()));
        }
        Ok(())
    }

    /// Retires a policy: it goes with its versions (ADR-0117).
    pub async fn delete(&self, org: &OrgGrn, grn: &str) -> Result<(), Error> {
        owned(org, grn)?;
        let deleted = sqlx::query("DELETE FROM policies WHERE grn = $1")
            .bind(grn)
            .execute(&self.pool)
            .await
            .map_err(Error::Store)?;
        if deleted.rows_affected() == 0 {
            return Err(Error::NotFound(grn.to_owned()));
        }
        Ok(())
    }

    /// Every stored version, oldest first.
    pub async fn versions(&self, org: &OrgGrn, grn: &str) -> Result<Vec<Version>, Error> {
        owned(org, grn)?;
        let rows = sqlx::query(
            "SELECT version, body, created_by FROM policy_versions
             WHERE policy_grn = $1 ORDER BY version",
        )
        .bind(grn)
        .fetch_all(&self.pool)
        .await
        .map_err(Error::Store)?;
        rows.into_iter()
            .map(|row| {
                let body: Json<Stored> = row.try_get("body").map_err(Error::Store)?;
                Ok(Version {
                    version: row.try_get("version").map_err(Error::Store)?,
                    body: Body(body.0),
                    created_by: row.try_get("created_by").map_err(Error::Store)?,
                })
            })
            .collect()
    }

    /// The organization's active policies at their default versions.
    pub async fn active_policies(&self, org: &OrgGrn) -> Result<OrgPolicies, Error> {
        let rows = sqlx::query(
            "SELECT policies.grn, policy_versions.body
             FROM policies
             JOIN policy_versions
               ON policy_versions.policy_grn = policies.grn
              AND policy_versions.version = policies.default_version
             WHERE policies.org_grn = $1 AND policies.status = 'active'",
        )
        .bind(org.as_str())
        .fetch_all(&self.pool)
        .await
        .map_err(Error::Store)?;
        let mut policies = OrgPolicies::default();
        for row in rows {
            let grn: String = row.try_get("grn").map_err(Error::Store)?;
            let body: Json<Stored> = row.try_get("body").map_err(Error::Store)?;
            match Body(body.0).into_policy(&grn)? {
                StoredPolicy::Link(link) => policies.links.push(link),
                StoredPolicy::Guardrail(guardrail) => policies.guardrails.push(guardrail),
            }
        }
        Ok(policies)
    }
}

impl PolicySource for PolicyStore {
    async fn active(&self, org: &OrgGrn) -> Result<OrgPolicies, Cause> {
        self.active_policies(org).await.map_err(Cause::from)
    }
}

/// Refuses a GRN that is not `grn:<org>:<project>:policy:<id>` under
/// this organization, which also keeps a policy's id from the system
/// tier's: those hold no segments.
fn owned(org: &OrgGrn, grn: &str) -> Result<(), Error> {
    let segments: Vec<&str> = grn.split(':').collect();
    let shaped = matches!(
        segments.as_slice(),
        ["grn", _, project, "policy", id] if !project.is_empty() && !id.is_empty()
    );
    if shaped && org.owns_grn(grn) {
        return Ok(());
    }
    Err(Error::ForeignPolicy(grn.to_owned()))
}

async fn insert_version(
    transaction: &mut Transaction<'_, Postgres>,
    grn: &str,
    version: i32,
    body: &Body,
    created_by: &str,
) -> Result<(), Error> {
    sqlx::query(
        "INSERT INTO policy_versions (policy_grn, version, body, created_by)
         VALUES ($1, $2, $3, $4)",
    )
    .bind(grn)
    .bind(version)
    .bind(Json(&body.0))
    .bind(created_by)
    .execute(&mut **transaction)
    .await
    .map_err(Error::Store)?;
    Ok(())
}
