//! What an organization's `permit` does in a decision (ADR-0144):
//! allows what it names inside the organization, never past tenant
//! isolation or a `forbid`.

#![cfg(test)]

mod fixture;

use cedar_policy::{EntityUid, PolicyId};
use decision::{
    Cause, EntitySlice, EntitySource, OrgGrn, OrgPolicies, Outcome, PolicySource, Reason,
};
use fixture::*;

const WATCH: &str = "agent-watch";
const PERMIT_ID: &str = "grn:acme/iam/policy/lee-watches-bot";
const LEE_WATCHES_BOT: &str = r#"permit (
    principal == Iam::User::"grn:acme/iam/user/lee",
    action == Iam::Action::"agent-watch",
    resource == Iam::Agent::"grn:acme/iam/agent/bot"
);"#;
const ANYONE_WATCHES_ACME: &str = r#"permit (
    principal,
    action == Iam::Action::"agent-watch",
    resource in Iam::Org::"grn:root/iam/org/acme"
);"#;
const NO_ONE_WATCHES_BOT: &str = r#"forbid (
    principal,
    action == Iam::Action::"agent-watch",
    resource == Iam::Agent::"grn:acme/iam/agent/bot"
);"#;

fn action() -> String {
    format!("iam:{WATCH}")
}

fn named(id: &str) -> Vec<Reason> {
    vec![Reason::Policy(PolicyId::new(id))]
}

#[test]
fn a_permit_allows_what_it_names_and_the_reasons_name_it_by_its_id() {
    let world = World::new();
    let permitted = world.compose(&[], &[policy(PERMIT_ID, LEE_WATCHES_BOT)]);

    let allowed = world.ask(&permitted, LEE, &action(), BOT);
    assert_eq!(allowed.outcome, Outcome::Allow);
    assert_eq!(allowed.reasons, named(PERMIT_ID));

    // No role holds `agent-watch`, so what allowed it is the permit.
    let without = world.compose(&[], &[]);
    let denied = world.ask(&without, LEE, &action(), BOT);
    assert_eq!(denied.outcome, Outcome::Deny);

    // It names lee and the bot alone.
    let other_principal = world.ask(&permitted, KIM, &action(), BOT);
    assert_eq!(other_principal.outcome, Outcome::Deny);
    let other_resource = world.ask(&permitted, LEE, &action(), PEER);
    assert_eq!(other_resource.outcome, Outcome::Deny);
}

#[test]
fn a_principal_outside_the_organization_is_denied_through_a_permit() {
    let world = World::new();
    let permitted = world.compose(&[], &[policy(PERMIT_ID, ANYONE_WATCHES_ACME)]);

    let outside = world.ask(&permitted, MIA, &action(), BOT);
    assert_eq!(outside.outcome, Outcome::Deny);
    assert_eq!(outside.reasons, named("tenant-isolation"));

    let control = world.ask(&permitted, LEE, &action(), BOT);
    assert_eq!(control.outcome, Outcome::Allow);
    assert_eq!(control.reasons, named(PERMIT_ID));
}

#[test]
fn a_forbid_guardrail_overrides_a_permit() {
    let world = World::new();
    let permitted = world.compose(
        &[],
        &[
            policy(PERMIT_ID, ANYONE_WATCHES_ACME),
            policy("grn:acme/iam/policy/no-watching", NO_ONE_WATCHES_BOT),
        ],
    );

    let overridden = world.ask(&permitted, LEE, &action(), BOT);
    assert_eq!(overridden.outcome, Outcome::Deny);
    assert_eq!(overridden.reasons, named("grn:acme/iam/policy/no-watching"));

    let control = world.ask(&permitted, LEE, &action(), PEER);
    assert_eq!(control.outcome, Outcome::Allow);
}

struct Held {
    world: World,
    held: OrgPolicies,
}

impl PolicySource for Held {
    async fn active(&self, org: &OrgGrn) -> Result<OrgPolicies, Cause> {
        if org.as_str() == ACME.1 {
            return Ok(self.held.clone());
        }
        Ok(OrgPolicies::default())
    }
}

impl EntitySource for Held {
    async fn slice(&self, _: &EntityUid, resource: &EntityUid) -> Result<EntitySlice, Cause> {
        let org = if resource.id().unescaped().contains("globex") {
            GLOBEX.1
        } else {
            ACME.1
        };
        Ok(EntitySlice {
            entities: self.world.entities.clone(),
            org: Some(OrgGrn::new(org)),
        })
    }
}

#[tokio::test]
async fn a_decision_composes_the_permits_of_the_resources_organization() {
    let world = World::new();
    let engine = world.engine.clone();
    let held = OrgPolicies {
        permits: vec![policy(PERMIT_ID, ANYONE_WATCHES_ACME)],
        ..OrgPolicies::default()
    };
    let store = Held { world, held };

    let allowed = engine
        .evaluate(&store, &store, &evaluation(LEE, &action(), BOT))
        .await
        .unwrap();
    assert_eq!(allowed.outcome, Outcome::Allow);
    assert_eq!(allowed.reasons, named(PERMIT_ID));

    let elsewhere = engine
        .evaluate(&store, &store, &evaluation(MIA, &action(), RIVAL))
        .await
        .unwrap();
    assert_eq!(elsewhere.outcome, Outcome::Deny);
}
