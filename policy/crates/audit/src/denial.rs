use std::sync::Arc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::Duration;

use sqlx::{PgExecutor, PgPool, Postgres, Transaction};

use crate::error::Error;
use crate::read::{self, Page, RETENTION_DAYS};

/// How many live denial records an organization holds. Past it a
/// denial that matches none is counted in its day's overflow record.
pub const MAX_LIVE_DENIALS: i64 = 10_000;

/// How long a denial's record may take before the denial is answered
/// without it and counted as lost. Short, because the answer waits on
/// it: an authorization answer is not worth a longer wait than this to
/// an audit trail.
pub const RECORD_BOUND: Duration = Duration::from_secs(2);

/// The longest a denial's subject, action or resource may be, in
/// bytes: what an entity id is held to at the service's door. A
/// denial with a longer one is answered and left unrecorded.
pub const MAX_FIELD_BYTES: usize = 512;

/// What a reason that is an evaluation fault is recorded as, in place
/// of its text.
pub const FAULT: &str = "fault";

/// The organization's GRN prefix, which an organization's own id
/// carries.
const ORG_PREFIX: &str = "grn:root/iam/org/";

/// One denial, as the engine decided it.
#[derive(Debug, Clone, Copy)]
pub struct Denial<'a> {
    /// The calling service's `sub`.
    pub service: &'a str,
    /// The subject's GRN.
    pub subject: &'a str,
    /// The action in its qualified wire form.
    pub action: &'a str,
    /// The resource's GRN.
    pub resource: &'a str,
    /// The policies of the decision's reasons, by id, with
    /// [`FAULT`] for a reason that is an evaluation fault.
    pub policies: &'a [String],
}

/// What became of a denial.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Recorded {
    /// It added a record.
    Stored,
    /// It matched a record of its hour and added one to its count.
    Collapsed,
    /// The organization is at its cap: it was counted in its day's
    /// overflow record.
    Overflowed,
    /// No organization of the store's is the resource's, or a field
    /// is past [`MAX_FIELD_BYTES`]: nothing was recorded, and the
    /// omission was counted.
    Omitted,
    /// The record could not be written, or took past its bound: the
    /// failure was logged and counted.
    Lost,
}

/// Records denials and reads and sweeps the records. A denial's record
/// never changes the denial's answer: a failure to write it is logged
/// and counted here and goes no further.
#[derive(Debug, Clone)]
pub struct Recorder {
    pool: PgPool,
    lost: Arc<AtomicU64>,
    omitted: Arc<AtomicU64>,
    bound: Duration,
}

impl Recorder {
    /// A recorder over the service's database.
    pub fn new(pool: PgPool) -> Self {
        Self {
            pool,
            lost: Arc::default(),
            omitted: Arc::default(),
            bound: RECORD_BOUND,
        }
    }

    /// This recorder, giving a denial's record `bound` instead of
    /// [`RECORD_BOUND`].
    #[must_use]
    pub fn bounded(mut self, bound: Duration) -> Self {
        self.bound = bound;
        self
    }

    /// How many denials could not be recorded since start.
    pub fn lost(&self) -> u64 {
        self.lost.load(Ordering::Relaxed)
    }

    /// How many denials were left unrecorded since start because no
    /// organization of the store's is the resource's or a field was
    /// too long.
    pub fn omitted(&self) -> u64 {
        self.omitted.load(Ordering::Relaxed)
    }

    /// Records one denial for the organization of its resource, and
    /// answers within the recorder's bound, whatever the database
    /// does.
    pub async fn deny(&self, denial: &Denial<'_>) -> Recorded {
        match tokio::time::timeout(self.bound, self.store(denial)).await {
            Ok(Ok(Recorded::Omitted)) => {
                self.omitted.fetch_add(1, Ordering::Relaxed);
                tracing::warn!(
                    "a denial is left unrecorded: no held organization, or a field too long"
                );
                Recorded::Omitted
            }
            Ok(Ok(recorded)) => recorded,
            Ok(Err(error)) => {
                self.lost.fetch_add(1, Ordering::Relaxed);
                tracing::error!(error = ?error, "a denial could not be recorded");
                Recorded::Lost
            }
            Err(_) => {
                self.lost.fetch_add(1, Ordering::Relaxed);
                tracing::error!(bound = ?self.bound, "a denial's record passed its bound");
                Recorded::Lost
            }
        }
    }

    /// An organization's records, newest first, `limit` at a time
    /// after the record `after` names.
    pub async fn list(&self, org: &str, after: Option<i64>, limit: i64) -> Result<Page, Error> {
        read::list(&self.pool, org, after, limit).await
    }

    /// Removes the denials past their retention, and answers how many
    /// went. A write's record stays.
    pub async fn sweep(&self) -> Result<u64, Error> {
        read::sweep(&self.pool).await
    }

    async fn store(&self, denial: &Denial<'_>) -> Result<Recorded, Error> {
        let Some(org) = organization_of(denial.resource) else {
            return Ok(Recorded::Omitted);
        };
        if [denial.subject, denial.action, denial.resource]
            .iter()
            .any(|field| field.len() > MAX_FIELD_BYTES)
        {
            return Ok(Recorded::Omitted);
        }
        let mut policies = denial.policies.to_vec();
        policies.sort();
        policies.dedup();
        let identity = [
            org.as_str(),
            denial.subject,
            denial.action,
            denial.resource,
            &policies.join("\u{1f}"),
        ]
        .join("\u{1e}");
        // A repeated denial is one statement and takes no transaction,
        // so a flood of them neither waits on a lock nor holds a
        // connection across round trips.
        if collapse(&self.pool, &org, &identity).await? {
            return Ok(Recorded::Collapsed);
        }
        let mut transaction = self.pool.begin().await.map_err(Error::Store)?;
        // The cap is exact: one denial per organization is counted and
        // stored at a time, and the entity row it is held by cannot be
        // deleted underneath it.
        sqlx::query("SELECT pg_advisory_xact_lock(hashtextextended($1, 0))")
            .bind(&org)
            .execute(&mut *transaction)
            .await
            .map_err(Error::Store)?;
        let held = sqlx::query_scalar::<_, i32>(
            "SELECT 1 FROM entities
             WHERE entity_type = 'Iam::Org' AND entity_id = $1
               AND document IS NOT NULL
             FOR SHARE",
        )
        .bind(&org)
        .fetch_optional(&mut *transaction)
        .await
        .map_err(Error::Store)?;
        if held.is_none() {
            return Ok(Recorded::Omitted);
        }
        if collapse(&mut *transaction, &org, &identity).await? {
            transaction.commit().await.map_err(Error::Store)?;
            return Ok(Recorded::Collapsed);
        }
        let live: i64 = sqlx::query_scalar(
            "SELECT count(*) FROM audit_records
             WHERE org_grn = $1 AND kind = 'denial'
               AND last_at > now() - make_interval(days => $2)",
        )
        .bind(&org)
        .bind(RETENTION_DAYS)
        .fetch_one(&mut *transaction)
        .await
        .map_err(Error::Store)?;
        let recorded = if live < MAX_LIVE_DENIALS {
            insert(&mut transaction, &org, denial, &policies, &identity).await?;
            Recorded::Stored
        } else {
            overflow(&mut transaction, &org).await?;
            Recorded::Overflowed
        };
        transaction.commit().await.map_err(Error::Store)?;
        Ok(recorded)
    }
}

/// The organization a resource belongs to, as its GRN: the first
/// segment of the resource's own GRN, or the organization's own id.
/// A resource of no organization has none.
pub fn organization_of(resource: &str) -> Option<String> {
    let rest = resource.strip_prefix("grn:")?;
    let (slug, path) = rest.split_once('/')?;
    if slug == "root" {
        let own = path.strip_prefix("iam/org/")?;
        return (!own.is_empty() && !own.contains('/')).then(|| resource.to_owned());
    }
    (!slug.is_empty()).then(|| format!("{ORG_PREFIX}{slug}"))
}

/// The hour counted from the epoch, on the database's clock.
macro_rules! hour {
    () => {
        "floor(extract(epoch FROM now()) / 3600)::bigint"
    };
}

/// The UTC day counted from the epoch, on the database's clock.
macro_rules! day {
    () => {
        "floor(extract(epoch FROM now()) / 86400)::bigint"
    };
}

/// Adds one to the record of this hour that the denial matches.
async fn collapse<'e>(
    executor: impl PgExecutor<'e>,
    org: &str,
    identity: &str,
) -> Result<bool, Error> {
    let matched = sqlx::query(concat!(
        "UPDATE audit_records SET count = count + 1, last_at = now()
         WHERE kind = 'denial' AND org_grn = $1
           AND identity = encode(sha256(convert_to($2, 'UTF8')), 'hex')
           AND bucket = ",
        hour!()
    ))
    .bind(org)
    .bind(identity)
    .execute(executor)
    .await
    .map_err(Error::Store)?;
    Ok(matched.rows_affected() > 0)
}

async fn insert(
    transaction: &mut Transaction<'_, Postgres>,
    org: &str,
    denial: &Denial<'_>,
    policies: &[String],
    identity: &str,
) -> Result<(), Error> {
    sqlx::query(concat!(
        "INSERT INTO audit_records
             (org_grn, kind, service, subject, action, resource, outcome,
              policy_ids, identity, bucket)
         VALUES ($1, 'denial', $2, $3, $4, $5, 'deny', $6,
                 encode(sha256(convert_to($7, 'UTF8')), 'hex'), ",
        hour!(),
        ")"
    ))
    .bind(org)
    .bind(denial.service)
    .bind(denial.subject)
    .bind(denial.action)
    .bind(denial.resource)
    .bind(policies)
    .bind(identity)
    .execute(&mut **transaction)
    .await
    .map_err(Error::Store)?;
    Ok(())
}

/// Adds one to the organization's overflow record of this UTC day.
async fn overflow(transaction: &mut Transaction<'_, Postgres>, org: &str) -> Result<(), Error> {
    sqlx::query(concat!(
        "INSERT INTO audit_records (org_grn, kind, bucket)
         VALUES ($1, 'denial-overflow', ",
        day!(),
        ")
         ON CONFLICT (org_grn, bucket) WHERE kind = 'denial-overflow'
         DO UPDATE SET count = audit_records.count + 1, last_at = now()"
    ))
    .bind(org)
    .execute(&mut **transaction)
    .await
    .map_err(Error::Store)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::organization_of;

    #[test]
    fn a_resource_belongs_to_the_organization_its_grn_names_and_an_org_to_itself() {
        let acme = Some("grn:root/iam/org/acme".to_owned());
        assert_eq!(organization_of("grn:acme/iam/agent/bot"), acme);
        assert_eq!(organization_of("grn:root/iam/org/acme"), acme);
        assert_eq!(organization_of("grn:root/iam/org/"), None);
        assert_eq!(organization_of("grn:root/iam/user/x"), None);
        assert_eq!(organization_of("grn:/iam/agent/bot"), None);
        assert_eq!(organization_of("not-a-grn"), None);
    }
}
