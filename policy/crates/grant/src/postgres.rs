use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};

use audit::{Origin, Outcome, Write, Written, record_write};
use decision::{Cause, OrgGrn, OrgPolicies, PolicySource};
use sqlx::postgres::PgRow;
use sqlx::types::Json;
use sqlx::{PgPool, Postgres, Row, Transaction};

use crate::body::{Body, BodyKind, Stored, StoredPolicy};
use crate::error::Error;
use crate::permit_right::PermitRight;
use crate::step::{Step, action_for};

/// The kind of a policy, which its first version holds.
macro_rules! kind_sql {
    () => {
        "(SELECT body->>'kind' FROM policy_versions
          WHERE policy_grn = $1 AND version = 1)"
    };
}

/// The summary query with a trailing clause, as one static string.
macro_rules! summary_sql {
    ($tail:literal) => {
        concat!(
            "SELECT policies.grn, policies.name, policies.status,
        policies.default_version, first.body->>'kind' AS kind
     FROM policies
     JOIN policy_versions first
       ON first.policy_grn = policies.grn AND first.version = 1 ",
            $tail
        )
    };
}

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

/// A policy without its versions.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Summary {
    /// The policy's GRN.
    pub grn: String,
    /// Its name.
    pub name: String,
    /// Whether it decides.
    pub status: Status,
    /// The version that decides once it is active.
    pub default_version: i32,
    /// What its versions say.
    pub kind: BodyKind,
}

/// The policies in the service's own database. Every write is
/// recorded in its own transaction, as made by the store's origin
/// (ADR-0148), unless the organization is not held: the write is then
/// made and its record is left out and counted.
#[derive(Debug, Clone)]
pub struct PolicyStore {
    pool: PgPool,
    origin: Origin,
    unrecorded: Arc<AtomicU64>,
}

impl PolicyStore {
    /// A store over this pool, writing as the policy service itself
    /// with no subject.
    pub fn new(pool: PgPool) -> Self {
        Self {
            pool,
            origin: Origin {
                service: "policy".to_owned(),
                subject: None,
            },
            unrecorded: Arc::default(),
        }
    }

    /// How many writes were made without a record since start,
    /// because their organization is not held.
    pub fn unrecorded(&self) -> u64 {
        self.unrecorded.load(Ordering::Relaxed)
    }

    /// This store, writing as `origin`: the calling service and the
    /// subject that asked.
    #[must_use]
    pub fn acting(&self, origin: Origin) -> Self {
        Self {
            pool: self.pool.clone(),
            origin,
            unrecorded: Arc::clone(&self.unrecorded),
        }
    }

    /// Records a write of `kind` in the transaction that makes it.
    async fn record(
        &self,
        transaction: &mut Transaction<'_, Postgres>,
        org: &OrgGrn,
        policy: &str,
        (kind, step): (BodyKind, Step),
        change: (Outcome, Option<i32>, Option<i32>),
    ) -> Result<(), Error> {
        let (outcome, from, to) = change;
        let written = record_write(
            transaction,
            &self.origin,
            &Write {
                org: org.as_str(),
                action: action_for(kind, step),
                policy,
                outcome,
                from,
                to,
            },
        )
        .await
        .map_err(Error::Record)?;
        if written == Written::Omitted {
            self.unrecorded.fetch_add(1, Ordering::Relaxed);
            tracing::warn!("a write is made without a record: its organization is not held");
        }
        Ok(())
    }

    /// Stores a policy as a draft holding version 1. Its GRN is under
    /// its organization, so no organization squats another's.
    pub async fn create(&self, policy: NewPolicy<'_>) -> Result<(), Error> {
        owned(policy.org, policy.grn)?;
        policy.body.check_right(policy.org, policy.created_by)?;
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
        self.record(
            &mut transaction,
            policy.org,
            policy.grn,
            (policy.body.kind(), Step::Create),
            (Outcome::Created, None, Some(1)),
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
        body.check_right(org, created_by)?;
        let mut transaction = self.pool.begin().await.map_err(Error::Store)?;
        // The row lock orders two appends to one policy.
        sqlx::query("SELECT 1 FROM policies WHERE grn = $1 FOR UPDATE")
            .bind(grn)
            .fetch_optional(&mut *transaction)
            .await
            .map_err(Error::Store)?
            .ok_or_else(|| Error::NotFound(grn.to_owned()))?;
        let first: String = sqlx::query_scalar(
            "SELECT body->>'kind' FROM policy_versions
             WHERE policy_grn = $1 AND version = 1",
        )
        .bind(grn)
        .fetch_one(&mut *transaction)
        .await
        .map_err(Error::Store)?;
        if first != body.kind().tag() {
            return Err(Error::KindChange(grn.to_owned()));
        }
        let version: i32 = sqlx::query_scalar(
            "SELECT max(version) + 1 FROM policy_versions WHERE policy_grn = $1",
        )
        .bind(grn)
        .fetch_one(&mut *transaction)
        .await
        .map_err(Error::Store)?;
        insert_version(&mut transaction, grn, version, body, created_by).await?;
        self.record(
            &mut transaction,
            org,
            grn,
            (body.kind(), Step::Append),
            (Outcome::VersionAdded, None, Some(version)),
        )
        .await?;
        transaction.commit().await.map_err(Error::Store)?;
        Ok(version)
    }

    /// Moves the default to any stored version, older or newer, but a
    /// `permit` one: making that effective is granting what it says,
    /// which [`PolicyStore::roll_back_permit`] does under a right.
    pub async fn roll_back(&self, org: &OrgGrn, grn: &str, version: i32) -> Result<(), Error> {
        self.move_default(org, grn, version, false).await
    }

    /// [`PolicyStore::roll_back`] to any stored version, a `permit`
    /// one too, under the right to write a `permit` on `org`.
    pub async fn roll_back_permit(
        &self,
        org: &OrgGrn,
        grn: &str,
        version: i32,
        right: &PermitRight,
    ) -> Result<(), Error> {
        right.check_org(org)?;
        self.move_default(org, grn, version, true).await
    }

    /// Makes a policy decide, and leaves its default where it is,
    /// unless that default is a `permit` version:
    /// [`PolicyStore::activate_permit`] does that under a right.
    pub async fn activate(&self, org: &OrgGrn, grn: &str) -> Result<(), Error> {
        self.set_active(org, grn, false, None).await
    }

    /// [`PolicyStore::activate`] for a policy whose default is a
    /// `permit` version too, under the right to write a `permit` on
    /// `org`.
    pub async fn activate_permit(
        &self,
        org: &OrgGrn,
        grn: &str,
        right: &PermitRight,
    ) -> Result<(), Error> {
        right.check_org(org)?;
        self.set_active(org, grn, true, None).await
    }

    async fn move_default(
        &self,
        org: &OrgGrn,
        grn: &str,
        version: i32,
        permits: bool,
    ) -> Result<(), Error> {
        owned(org, grn)?;
        let mut transaction = self.pool.begin().await.map_err(Error::Store)?;
        let from: Option<i32> =
            sqlx::query_scalar("SELECT default_version FROM policies WHERE grn = $1 FOR UPDATE")
                .bind(grn)
                .fetch_optional(&mut *transaction)
                .await
                .map_err(Error::Store)?;
        let moved = sqlx::query(concat!(
            "UPDATE policies SET default_version = $2, updated_at = now()
             WHERE grn = $1
               AND EXISTS (SELECT 1 FROM policy_versions
                           WHERE policy_grn = $1 AND version = $2)
               AND ($3 OR NOT EXISTS (SELECT 1 FROM policy_versions
                                      WHERE policy_grn = $1 AND version = $2
                                        AND body->>'kind' = 'permit'))
             RETURNING ",
            kind_sql!(),
            " AS kind"
        ))
        .bind(grn)
        .bind(version)
        .bind(permits)
        .fetch_optional(&mut *transaction)
        .await
        .map_err(Error::Store)?;
        if let Some(row) = moved {
            self.record(
                &mut transaction,
                org,
                grn,
                (kind_of(grn, &row)?, Step::RollBack),
                (Outcome::RolledBack, from, Some(version)),
            )
            .await?;
            return transaction.commit().await.map_err(Error::Store);
        }
        drop(transaction);
        let permit: Option<bool> = sqlx::query_scalar(
            "SELECT body->>'kind' = 'permit' FROM policy_versions
             WHERE policy_grn = $1 AND version = $2",
        )
        .bind(grn)
        .bind(version)
        .fetch_optional(&self.pool)
        .await
        .map_err(Error::Store)?;
        match permit {
            Some(true) => Err(Error::PermitNeedsRight(grn.to_owned())),
            _ => Err(Error::NoSuchVersion {
                policy: grn.to_owned(),
                version,
            }),
        }
    }

    /// [`PolicyStore::activate`] where the default is still `expected`,
    /// the version the caller checked, and `DefaultMoved` where it is
    /// not.
    pub async fn activate_pinned(
        &self,
        org: &OrgGrn,
        grn: &str,
        expected: i32,
    ) -> Result<(), Error> {
        self.set_active(org, grn, false, Some(expected)).await
    }

    /// [`PolicyStore::activate_pinned`] for a `permit` default too,
    /// under the right to write a `permit` on `org`.
    pub async fn activate_permit_pinned(
        &self,
        org: &OrgGrn,
        grn: &str,
        expected: i32,
        right: &PermitRight,
    ) -> Result<(), Error> {
        right.check_org(org)?;
        self.set_active(org, grn, true, Some(expected)).await
    }

    async fn set_active(
        &self,
        org: &OrgGrn,
        grn: &str,
        permits: bool,
        expected: Option<i32>,
    ) -> Result<(), Error> {
        owned(org, grn)?;
        let mut transaction = self.pool.begin().await.map_err(Error::Store)?;
        let updated = sqlx::query(concat!(
            "UPDATE policies SET status = 'active', updated_at = now()
             WHERE grn = $1
               AND ($3::int IS NULL OR default_version = $3)
               AND ($2 OR NOT EXISTS (SELECT 1 FROM policy_versions
                                      WHERE policy_grn = $1
                                        AND version = policies.default_version
                                        AND body->>'kind' = 'permit'))
             RETURNING default_version, ",
            kind_sql!(),
            " AS kind"
        ))
        .bind(grn)
        .bind(permits)
        .bind(expected)
        .fetch_optional(&mut *transaction)
        .await
        .map_err(Error::Store)?;
        if let Some(row) = updated {
            let default: i32 = row.try_get("default_version").map_err(Error::Store)?;
            self.record(
                &mut transaction,
                org,
                grn,
                (kind_of(grn, &row)?, Step::Activate),
                (Outcome::Activated, None, Some(default)),
            )
            .await?;
            return transaction.commit().await.map_err(Error::Store);
        }
        drop(transaction);
        let default: Option<(i32, bool)> = sqlx::query_as(
            "SELECT policies.default_version,
                    policy_versions.body->>'kind' = 'permit' FROM policies
             JOIN policy_versions
               ON policy_versions.policy_grn = policies.grn
              AND policy_versions.version = policies.default_version
             WHERE policies.grn = $1",
        )
        .bind(grn)
        .fetch_optional(&self.pool)
        .await
        .map_err(Error::Store)?;
        match (default, expected) {
            (Some((version, _)), Some(expected)) if version != expected => {
                Err(Error::DefaultMoved {
                    policy: grn.to_owned(),
                    expected,
                })
            }
            (Some((_, true)), _) => Err(Error::PermitNeedsRight(grn.to_owned())),
            _ => Err(Error::NotFound(grn.to_owned())),
        }
    }

    /// Retires a policy: it goes with its versions (ADR-0117).
    pub async fn delete(&self, org: &OrgGrn, grn: &str) -> Result<(), Error> {
        owned(org, grn)?;
        let mut transaction = self.pool.begin().await.map_err(Error::Store)?;
        let held = sqlx::query(concat!(
            "SELECT ",
            kind_sql!(),
            " AS kind FROM policies WHERE grn = $1 FOR UPDATE"
        ))
        .bind(grn)
        .fetch_optional(&mut *transaction)
        .await
        .map_err(Error::Store)?
        .ok_or_else(|| Error::NotFound(grn.to_owned()))?;
        sqlx::query("DELETE FROM policies WHERE grn = $1")
            .bind(grn)
            .execute(&mut *transaction)
            .await
            .map_err(Error::Store)?;
        self.record(
            &mut transaction,
            org,
            grn,
            (kind_of(grn, &held)?, Step::Remove),
            (Outcome::Removed, None, None),
        )
        .await?;
        transaction.commit().await.map_err(Error::Store)
    }

    /// One policy of the organization, without its versions.
    pub async fn get(&self, org: &OrgGrn, grn: &str) -> Result<Summary, Error> {
        owned(org, grn)?;
        let row = sqlx::query(summary_sql!("WHERE policies.grn = $1"))
            .bind(grn)
            .fetch_optional(&self.pool)
            .await
            .map_err(Error::Store)?
            .ok_or_else(|| Error::NotFound(grn.to_owned()))?;
        summary(&row)
    }

    /// The organization's policies, by GRN.
    pub async fn list(&self, org: &OrgGrn) -> Result<Vec<Summary>, Error> {
        let rows = sqlx::query(summary_sql!(
            "WHERE policies.org_grn = $1 ORDER BY policies.grn"
        ))
        .bind(org.as_str())
        .fetch_all(&self.pool)
        .await
        .map_err(Error::Store)?;
        rows.iter().map(summary).collect()
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
                StoredPolicy::Permit(permit) => policies.permits.push(permit),
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

fn summary(row: &sqlx::postgres::PgRow) -> Result<Summary, Error> {
    let grn: String = row.try_get("grn").map_err(Error::Store)?;
    let status: String = row.try_get("status").map_err(Error::Store)?;
    let kind: String = row.try_get("kind").map_err(Error::Store)?;
    let unknown = |what: &str| Error::StoredBody {
        policy: grn.clone(),
        source: format!("unknown {what}").into(),
    };
    Ok(Summary {
        name: row.try_get("name").map_err(Error::Store)?,
        status: match status.as_str() {
            "draft" => Status::Draft,
            "active" => Status::Active,
            _ => return Err(unknown("status")),
        },
        default_version: row.try_get("default_version").map_err(Error::Store)?,
        kind: BodyKind::from_tag(&kind).ok_or_else(|| unknown("kind"))?,
        grn: grn.clone(),
    })
}

/// Refuses a GRN that is not `grn:<org>/iam/policy/<id>` under this
/// organization, which also keeps a policy's id from the system tier's:
/// those hold no segments.
fn owned(org: &OrgGrn, grn: &str) -> Result<(), Error> {
    if org.owns_policy(grn) {
        return Ok(());
    }
    Err(Error::ForeignPolicy(grn.to_owned()))
}

/// The kind of the policy a statement answered with its `kind`.
fn kind_of(grn: &str, row: &PgRow) -> Result<BodyKind, Error> {
    let tag: String = row.try_get("kind").map_err(Error::Store)?;
    BodyKind::from_tag(&tag).ok_or_else(|| Error::StoredBody {
        policy: grn.to_owned(),
        source: "unknown kind".into(),
    })
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
