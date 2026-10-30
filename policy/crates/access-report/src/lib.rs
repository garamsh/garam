//! Lists the active policies whose scope reaches a subject with its
//! teams, or a resource with its ancestors, in one organization. It is
//! a lookup over grants, not a decision (ADR-0130 §9): a guardrail's
//! condition is not evaluated, so one is listed wherever its scope
//! reaches.

use std::collections::BTreeSet;

use cedar_policy::{
    Entities, EntityTypeName, EntityUid, Policy, PolicyId, PrincipalConstraint, ResourceConstraint,
};
use decision::{Cause, EntitySource, OrgGrn, OrgPolicies, PolicySource, RoleLink, RoleScope};

/// Why a report could not be read.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The entity store could not be read.
    #[error("the entity store could not be read")]
    EntityRead(#[source] Cause),
    /// The policy store could not be read.
    #[error("the policy store could not be read")]
    PolicyRead(#[source] Cause),
}

/// One policy a report lists.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Entry {
    /// The policy.
    pub policy: PolicyId,
    /// What it names of the subject or resource asked about and what
    /// holds it. Empty for a guardrail whose scope reaches what was
    /// asked and names none of it.
    pub named: Vec<EntityUid>,
}

/// The side of a policy a report reads.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Side {
    Subject,
    Resource,
}

/// The policies of `org` naming `subject` or anything holding it.
pub async fn for_subject<P: PolicySource, E: EntitySource>(
    policies: &P,
    entities: &E,
    org: &OrgGrn,
    subject: &EntityUid,
) -> Result<Vec<Entry>, Error> {
    report(policies, entities, org, subject, Side::Subject).await
}

/// The policies of `org` naming `resource` or anything containing it.
pub async fn for_resource<P: PolicySource, E: EntitySource>(
    policies: &P,
    entities: &E,
    org: &OrgGrn,
    resource: &EntityUid,
) -> Result<Vec<Entry>, Error> {
    report(policies, entities, org, resource, Side::Resource).await
}

async fn report<P: PolicySource, E: EntitySource>(
    policies: &P,
    entities: &E,
    org: &OrgGrn,
    uid: &EntityUid,
    side: Side,
) -> Result<Vec<Entry>, Error> {
    let slice = entities.slice(uid, uid).await.map_err(Error::EntityRead)?;
    let asked = Asked {
        uid,
        held_by: with_ancestors(uid, &slice.entities, org),
    };
    let org_policies = policies.active(org).await.map_err(Error::PolicyRead)?;
    Ok(entries(&org_policies, &asked, side))
}

/// What a report is asked about, and what holds it in this
/// organization.
struct Asked<'a> {
    uid: &'a EntityUid,
    held_by: BTreeSet<EntityUid>,
}

impl Asked<'_> {
    fn is_of_type(&self, entity_type: &EntityTypeName) -> bool {
        self.uid.type_name() == entity_type
    }
}

/// `uid` and its ancestors in `org`: a principal's teams elsewhere are
/// no business of this organization's report.
fn with_ancestors(uid: &EntityUid, entities: &Entities, org: &OrgGrn) -> BTreeSet<EntityUid> {
    let mut held_by: BTreeSet<EntityUid> = entities
        .ancestors(uid)
        .map(|ancestors| ancestors.filter(|a| org.owns(a)).cloned().collect())
        .unwrap_or_default();
    held_by.insert(uid.clone());
    held_by
}

fn entries(policies: &OrgPolicies, asked: &Asked<'_>, side: Side) -> Vec<Entry> {
    let links = policies
        .links
        .iter()
        .filter_map(|link| link_entry(link, asked, side));
    let guardrails = policies
        .guardrails
        .iter()
        .filter_map(|guardrail| guardrail_entry(guardrail, asked, side));
    let mut entries: Vec<Entry> = links.chain(guardrails).collect();
    entries.sort_by_key(|entry| entry.policy.to_string());
    entries
}

/// A link reaches what `in` reaches, but a scoped role's `resource ==`
/// reaches its team alone and nothing the team contains (ADR-0137).
fn link_entry(link: &RoleLink, asked: &Asked<'_>, side: Side) -> Option<Entry> {
    let (named, reached) = match (side, link.scope) {
        (Side::Subject, _) => (&link.principal, asked.held_by.contains(&link.principal)),
        (Side::Resource, RoleScope::Organization) => {
            (&link.resource, asked.held_by.contains(&link.resource))
        }
        (Side::Resource, RoleScope::TeamOrProject) => (&link.resource, link.resource == *asked.uid),
    };
    reached.then(|| Entry {
        policy: link.id.clone(),
        named: vec![named.clone()],
    })
}

fn guardrail_entry(guardrail: &Policy, asked: &Asked<'_>, side: Side) -> Option<Entry> {
    if !scope_reaches(guardrail, asked, side) {
        return None;
    }
    let mut named: Vec<EntityUid> = guardrail
        .entity_literals()
        .into_iter()
        .filter(|literal| asked.held_by.contains(literal))
        .collect();
    named.sort();
    named.dedup();
    Some(Entry {
        policy: guardrail.id().clone(),
        named,
    })
}

/// Reads the scope as Cedar does: `==` is the entity alone, `in` what
/// holds it, and `is` its type.
fn scope_reaches(guardrail: &Policy, asked: &Asked<'_>, side: Side) -> bool {
    let (is_of, equals, within) = match side {
        Side::Subject => match guardrail.principal_constraint() {
            PrincipalConstraint::Any => (None, None, None),
            PrincipalConstraint::Eq(entity) => (None, Some(entity), None),
            PrincipalConstraint::In(entity) => (None, None, Some(entity)),
            PrincipalConstraint::Is(entity_type) => (Some(entity_type), None, None),
            PrincipalConstraint::IsIn(entity_type, entity) => {
                (Some(entity_type), None, Some(entity))
            }
        },
        Side::Resource => match guardrail.resource_constraint() {
            ResourceConstraint::Any => (None, None, None),
            ResourceConstraint::Eq(entity) => (None, Some(entity), None),
            ResourceConstraint::In(entity) => (None, None, Some(entity)),
            ResourceConstraint::Is(entity_type) => (Some(entity_type), None, None),
            ResourceConstraint::IsIn(entity_type, entity) => {
                (Some(entity_type), None, Some(entity))
            }
        },
    };
    is_of.is_none_or(|entity_type| asked.is_of_type(&entity_type))
        && equals.is_none_or(|entity| entity == *asked.uid)
        && within.is_none_or(|entity| asked.held_by.contains(&entity))
}
