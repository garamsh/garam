use sqlx::{Postgres, Transaction};

use crate::error::Error;

/// Who made a write: the calling service's `sub`, and the subject
/// that asked it, which a write the platform makes as itself has not.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Origin {
    /// The calling service's `sub`.
    pub service: String,
    /// The subject's GRN, where the call named one.
    pub subject: Option<String>,
}

/// What a write did to its policy.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Outcome {
    /// A policy and its first version were stored.
    Created,
    /// A version was appended.
    VersionAdded,
    /// The policy was made to decide.
    Activated,
    /// The default moved to another version.
    RolledBack,
    /// The policy and its versions went.
    Removed,
}

impl Outcome {
    /// The word the record and the contract carry.
    pub const fn tag(self) -> &'static str {
        match self {
            Self::Created => "created",
            Self::VersionAdded => "version-added",
            Self::Activated => "activated",
            Self::RolledBack => "rolled-back",
            Self::Removed => "removed",
        }
    }
}

/// One write of a policy.
#[derive(Debug, Clone, Copy)]
pub struct Write<'a> {
    /// The organization's GRN.
    pub org: &'a str,
    /// The action in its qualified wire form.
    pub action: &'a str,
    /// The policy's GRN.
    pub policy: &'a str,
    /// What the write did.
    pub outcome: Outcome,
    /// The default version before, where a write moved it.
    pub from: Option<i32>,
    /// The version written, or moved to.
    pub to: Option<i32>,
}

/// Records a write in the transaction that makes it, so a write that
/// cannot be recorded is not made.
pub async fn record_write(
    transaction: &mut Transaction<'_, Postgres>,
    origin: &Origin,
    write: &Write<'_>,
) -> Result<(), Error> {
    sqlx::query(
        "INSERT INTO audit_records
             (org_grn, kind, service, subject, action, resource, outcome,
              policy_ids, version_from, version_to)
         VALUES ($1, 'write', $2, $3, $4, $5, $6, ARRAY[$5], $7, $8)",
    )
    .bind(write.org)
    .bind(&origin.service)
    .bind(origin.subject.as_deref())
    .bind(write.action)
    .bind(write.policy)
    .bind(write.outcome.tag())
    .bind(write.from)
    .bind(write.to)
    .execute(&mut **transaction)
    .await
    .map_err(Error::Store)?;
    Ok(())
}
