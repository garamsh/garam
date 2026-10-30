//! Lists the active policies naming a subject with its teams, or a
//! resource with its ancestors. It is a lookup over grants, not a
//! decision (ADR-0130 §9).

use std::collections::BTreeSet;

use cedar_policy::{
    Entities, EntityUid, Policy, PolicyId, PrincipalConstraint, ResourceConstraint,
};
use decision::{Cause, EntitySource, OrgGrn, OrgPolicies, PolicySource, RoleLink};

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
    /// What it names of the subject or resource asked about. Empty for
    /// a guardrail that reaches everything on that side and names
    /// nothing asked about.
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
    let asked = with_ancestors(uid, &slice.entities);
    let org_policies = policies.active(org).await.map_err(Error::PolicyRead)?;
    Ok(entries(&org_policies, &asked, side))
}

fn with_ancestors(uid: &EntityUid, entities: &Entities) -> BTreeSet<EntityUid> {
    let mut asked: BTreeSet<EntityUid> = entities
        .ancestors(uid)
        .map(|ancestors| ancestors.cloned().collect())
        .unwrap_or_default();
    asked.insert(uid.clone());
    asked
}

fn entries(policies: &OrgPolicies, asked: &BTreeSet<EntityUid>, side: Side) -> Vec<Entry> {
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

fn link_entry(link: &RoleLink, asked: &BTreeSet<EntityUid>, side: Side) -> Option<Entry> {
    let named = match side {
        Side::Subject => &link.principal,
        Side::Resource => &link.resource,
    };
    asked.contains(named).then(|| Entry {
        policy: link.id.clone(),
        named: vec![named.clone()],
    })
}

fn guardrail_entry(guardrail: &Policy, asked: &BTreeSet<EntityUid>, side: Side) -> Option<Entry> {
    let mut named: Vec<EntityUid> = guardrail
        .entity_literals()
        .into_iter()
        .filter(|literal| asked.contains(literal))
        .collect();
    named.sort();
    named.dedup();
    (!named.is_empty() || reaches_everything(guardrail, side)).then(|| Entry {
        policy: guardrail.id().clone(),
        named,
    })
}

/// A scope naming no entity on this side applies to whatever is asked
/// about, so a report that left it out would read as if nothing did.
fn reaches_everything(guardrail: &Policy, side: Side) -> bool {
    match side {
        Side::Subject => matches!(
            guardrail.principal_constraint(),
            PrincipalConstraint::Any | PrincipalConstraint::Is(_)
        ),
        Side::Resource => matches!(
            guardrail.resource_constraint(),
            ResourceConstraint::Any | ResourceConstraint::Is(_)
        ),
    }
}
