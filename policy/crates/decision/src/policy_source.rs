use std::fmt;
use std::future::Future;

use cedar_policy::{EntityUid, Policy};

use crate::error::Cause;
use crate::role_link::RoleLink;

/// An organization's GRN, the key its policies are held under.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct OrgGrn(String);

impl OrgGrn {
    /// Wraps a GRN read from an `Org` entity's identifier.
    pub fn new(grn: impl Into<String>) -> Self {
        Self(grn.into())
    }

    /// The GRN.
    pub fn as_str(&self) -> &str {
        &self.0
    }

    /// The organization's name, the last segment of its GRN and the
    /// organization segment of every GRN beneath it (ADR-0001).
    pub fn slug(&self) -> &str {
        self.0.rsplit(':').next().unwrap_or_default()
    }

    /// Whether this GRN is one this organization owns. A policy GRN's
    /// other segments are the caller's.
    pub fn owns_grn(&self, grn: &str) -> bool {
        grn.split(':').nth(1) == Some(self.slug())
    }

    /// Whether the entity is this organization's. A user is nobody's:
    /// it may belong to several organizations, and tenant isolation
    /// decides each (ADR-0137).
    pub fn owns(&self, entity: &EntityUid) -> bool {
        let id = entity.id().unescaped();
        match entity.type_name().to_string().as_str() {
            "User" => true,
            "Org" | "OrgMembers" => id == self.0,
            _ => self.owns_grn(id),
        }
    }
}

impl fmt::Display for OrgGrn {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str(&self.0)
    }
}

/// The active policies of one organization.
#[derive(Debug, Clone, Default)]
pub struct OrgPolicies {
    /// Its role assignments.
    pub links: Vec<RoleLink>,
    /// Its `forbid` guardrails.
    pub guardrails: Vec<Policy>,
}

/// Reads an organization's active policies.
pub trait PolicySource {
    /// The policies a decision on this organization composes.
    fn active(&self, org: &OrgGrn) -> impl Future<Output = Result<OrgPolicies, Cause>> + Send;
}
