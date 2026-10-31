//! `Engine::evaluate` reads through its ports and decides.

#![cfg(test)]

mod fixture;

use cedar_policy::EntityUid;
use decision::{
    Cause, EntitySlice, EntitySource, Error, OrgGrn, OrgPolicies, Outcome, PolicySource, Role,
    RoleScope,
};
use fixture::*;

struct Policies {
    org: OrgGrn,
    held: OrgPolicies,
}

impl PolicySource for Policies {
    async fn active(&self, org: &OrgGrn) -> Result<OrgPolicies, Cause> {
        if *org == self.org {
            return Ok(self.held.clone());
        }
        Ok(OrgPolicies::default())
    }
}

struct Unreadable;

impl PolicySource for Unreadable {
    async fn active(&self, _org: &OrgGrn) -> Result<OrgPolicies, Cause> {
        Err("the store is down".into())
    }
}

impl EntitySource for Unreadable {
    async fn slice(&self, _: &EntityUid, _: &EntityUid) -> Result<EntitySlice, Cause> {
        Err("the store is down".into())
    }
}

/// The directory, with the resource's organization as a store reads it.
struct Directory {
    world: World,
    org: Option<OrgGrn>,
}

impl EntitySource for Directory {
    async fn slice(&self, _: &EntityUid, _: &EntityUid) -> Result<EntitySlice, Cause> {
        Ok(EntitySlice {
            entities: self.world.entities.clone(),
            org: self.org.clone(),
        })
    }
}

fn acme_grants_lee() -> Policies {
    Policies {
        org: OrgGrn::new(ACME.1),
        held: OrgPolicies {
            links: vec![link(Role::Member, RoleScope::Organization, LEE, ACME)],
            guardrails: vec![],
        },
    }
}

#[tokio::test]
async fn a_decision_composes_the_policies_of_the_resources_organization() {
    let world = World::new();
    let engine = world.engine.clone();
    let directory = Directory {
        world,
        org: Some(OrgGrn::new(ACME.1)),
    };

    let decision = engine
        .evaluate(
            &acme_grants_lee(),
            &directory,
            &evaluation(LEE, "agent:read", BOT),
        )
        .await
        .unwrap();
    assert_eq!(decision.outcome, Outcome::Allow, "{decision:?}");

    let elsewhere = Directory {
        world: World::new(),
        org: Some(OrgGrn::new(GLOBEX.1)),
    };
    let decision = engine
        .evaluate(
            &acme_grants_lee(),
            &elsewhere,
            &evaluation(LEE, "agent:read", BOT),
        )
        .await
        .unwrap();
    assert_eq!(decision.outcome, Outcome::Deny, "{decision:?}");
}

#[tokio::test]
async fn a_resource_the_store_does_not_hold_is_denied_with_no_policy_read() {
    let world = World::new();
    let engine = world.engine.clone();
    let directory = Directory { world, org: None };

    let decision = engine
        .evaluate(&Unreadable, &directory, &evaluation(LEE, "agent:read", BOT))
        .await
        .unwrap();
    assert_eq!(decision.outcome, Outcome::Deny, "{decision:?}");
}

#[tokio::test]
async fn a_store_that_cannot_be_read_fails_the_call_rather_than_answering() {
    let world = World::new();
    let engine = world.engine.clone();
    let directory = Directory {
        world,
        org: Some(OrgGrn::new(ACME.1)),
    };

    let policies = engine
        .evaluate(&Unreadable, &directory, &evaluation(LEE, "agent:read", BOT))
        .await;
    assert!(
        matches!(policies, Err(Error::PolicyRead(_))),
        "{policies:?}"
    );

    let entities = engine
        .evaluate(
            &acme_grants_lee(),
            &Unreadable,
            &evaluation(LEE, "agent:read", BOT),
        )
        .await;
    assert!(
        matches!(entities, Err(Error::EntityRead(_))),
        "{entities:?}"
    );
}
