use sqlx::{PgPool, Row};

use crate::error::Error;

/// How long a denial is kept past its last occurrence, in days.
pub const RETENTION_DAYS: i32 = 90;

/// What a record is.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    /// A write of a policy.
    Write,
    /// A denial, or the identical denials of an hour.
    Denial,
    /// The denials of a UTC day the cap stored no row for.
    DenialOverflow,
}

impl Kind {
    /// The word the record and the contract carry.
    pub const fn tag(self) -> &'static str {
        match self {
            Self::Write => "write",
            Self::Denial => "denial",
            Self::DenialOverflow => "denial-overflow",
        }
    }

    fn from_tag(tag: &str) -> Option<Self> {
        [Self::Write, Self::Denial, Self::DenialOverflow]
            .into_iter()
            .find(|kind| kind.tag() == tag)
    }
}

/// One record as the organization reads it. Times are microseconds
/// since the epoch, on the database's clock.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Record {
    /// Its place in the organization's order, newest highest.
    pub id: i64,
    /// What it is.
    pub kind: Kind,
    /// When a write was made, or a denial first occurred.
    pub first_at: i64,
    /// When a denial last occurred; a write's `first_at`.
    pub last_at: i64,
    /// How many identical denials it holds; 1 for a write.
    pub count: i64,
    /// The calling service's `sub`, except on an overflow record.
    pub service: Option<String>,
    /// The subject's GRN, where the record names one.
    pub subject: Option<String>,
    /// The action, in its qualified wire form.
    pub action: Option<String>,
    /// The resource's GRN: for a write, the policy's.
    pub resource: Option<String>,
    /// `deny`, or what a write did.
    pub outcome: Option<String>,
    /// The policies of a denial's reasons, a fault as `fault`; a
    /// write's policy.
    pub policy_ids: Vec<String>,
    /// The default a write moved from.
    pub version_from: Option<i32>,
    /// The version a write wrote or moved to.
    pub version_to: Option<i32>,
    /// The UTC day an overflow record holds, as its first second.
    pub day: Option<i64>,
}

/// A page of an organization's records, newest first.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Page {
    /// The records.
    pub records: Vec<Record>,
    /// The `after` that reads the next page, or none at the end.
    pub next: Option<i64>,
}

/// Reads an organization's records, newest first, `limit` at a time
/// after the record `after` names. A denial past its retention is
/// answered by nothing, whether or not the sweep has removed it.
pub(crate) async fn list(
    pool: &PgPool,
    org: &str,
    after: Option<i64>,
    limit: i64,
) -> Result<Page, Error> {
    let rows = sqlx::query(
        "SELECT id, kind,
                (extract(epoch FROM first_at) * 1000000)::bigint AS first_at,
                (extract(epoch FROM last_at) * 1000000)::bigint AS last_at,
                count, service, subject, action, resource, outcome,
                policy_ids, version_from, version_to,
                CASE WHEN kind = 'denial-overflow' THEN bucket * 86400 END AS day
         FROM audit_records
         WHERE org_grn = $1
           AND ($2::bigint IS NULL OR id < $2)
           AND (kind = 'write' OR last_at > now() - make_interval(days => $3))
         ORDER BY id DESC
         LIMIT $4",
    )
    .bind(org)
    .bind(after)
    .bind(RETENTION_DAYS)
    .bind(limit + 1)
    .fetch_all(pool)
    .await
    .map_err(Error::Store)?;
    let mut records = rows.iter().map(record).collect::<Result<Vec<_>, _>>()?;
    let more = i64::try_from(records.len()).is_ok_and(|held| held > limit);
    if more {
        records.truncate(usize::try_from(limit).unwrap_or(0));
    }
    let next = records.last().filter(|_| more).map(|last| last.id);
    Ok(Page { records, next })
}

/// Removes the denials past their retention. A write stays.
pub(crate) async fn sweep(pool: &PgPool) -> Result<u64, Error> {
    sqlx::query(
        "DELETE FROM audit_records
         WHERE kind <> 'write' AND last_at <= now() - make_interval(days => $1)",
    )
    .bind(RETENTION_DAYS)
    .execute(pool)
    .await
    .map(|done| done.rows_affected())
    .map_err(Error::Store)
}

fn record(row: &sqlx::postgres::PgRow) -> Result<Record, Error> {
    let tag: String = row.try_get("kind").map_err(Error::Store)?;
    let kind = Kind::from_tag(&tag).ok_or_else(|| {
        Error::Store(sqlx::Error::Decode(
            format!("an audit record of kind {tag}").into(),
        ))
    })?;
    Ok(Record {
        id: row.try_get("id").map_err(Error::Store)?,
        kind,
        first_at: row.try_get("first_at").map_err(Error::Store)?,
        last_at: row.try_get("last_at").map_err(Error::Store)?,
        count: row.try_get("count").map_err(Error::Store)?,
        service: row.try_get("service").map_err(Error::Store)?,
        subject: row.try_get("subject").map_err(Error::Store)?,
        action: row.try_get("action").map_err(Error::Store)?,
        resource: row.try_get("resource").map_err(Error::Store)?,
        outcome: row.try_get("outcome").map_err(Error::Store)?,
        policy_ids: row.try_get("policy_ids").map_err(Error::Store)?,
        version_from: row.try_get("version_from").map_err(Error::Store)?,
        version_to: row.try_get("version_to").map_err(Error::Store)?,
        day: row.try_get("day").map_err(Error::Store)?,
    })
}
