use std::fmt;
use std::future::Future;

use cedar_policy::{EntityUid, Policy};

use crate::error::Cause;
use crate::role_link::RoleLink;

/// What an organization's GRN begins with: organizations are owned by
/// `root` (ADR-0142).
const ORG_PREFIX: &str = "grn:root/iam/org/";

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

    /// The organization's name: what follows `grn:root/iam/org/` in its
    /// GRN, and the organization every GRN beneath it names first
    /// (ADR-0142).
    pub fn slug(&self) -> &str {
        self.0.strip_prefix(ORG_PREFIX).unwrap_or_default()
    }

    /// What follows `grn:<org>/` in a GRN this organization owns, or
    /// `None` for a GRN it does not. An organization with no name owns
    /// nothing.
    fn beneath<'a>(&self, grn: &'a str) -> Option<&'a str> {
        let slug = self.slug();
        let rest = grn.strip_prefix("grn:")?.strip_prefix(slug)?;
        rest.strip_prefix('/').filter(|_| !slug.is_empty())
    }

    /// Whether this GRN is one this organization owns: it names the
    /// organization first. The rest is the caller's.
    pub fn owns_grn(&self, grn: &str) -> bool {
        self.beneath(grn).is_some()
    }

    /// Whether the GRN is a policy of this organization, the one form a
    /// policy takes: `grn:<org>/iam/policy/<id>`, whose id is one
    /// segment. This also keeps a policy's id from the system tier's:
    /// those hold no segments.
    pub fn owns_policy(&self, grn: &str) -> bool {
        self.beneath(grn)
            .and_then(|rest| rest.strip_prefix("iam/policy/"))
            .is_some_and(|id| !id.is_empty() && !id.contains(['/', '#']))
    }

    /// Whether the entity is this organization's. A user is nobody's:
    /// it may belong to several organizations, and tenant isolation
    /// decides each (ADR-0137).
    pub fn owns(&self, entity: &EntityUid) -> bool {
        let id = entity.id().unescaped();
        match entity.type_name().to_string().as_str() {
            "Iam::User" => true,
            "Iam::Org" | "Iam::OrgMembers" => id == self.0,
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
