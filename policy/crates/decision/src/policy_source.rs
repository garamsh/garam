use std::fmt;
use std::future::Future;

use cedar_policy::Policy;

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
