//! No organization `permit` reaches an operator: its reach is what its
//! delegation holds, whatever a `permit` of the organization says
//! (ADR-0146 §A hosted operator's delegation).

#![cfg(test)]

mod fixture;

use decision::Outcome;
use fixture::*;

const CONFIGURE: &str = "iam:agent.configure";
const PERMIT_ID: &str = "grn:acme/iam/policy/anyone-configures";
const ANYONE_CONFIGURES_ACME: &str = r#"permit (
    principal,
    action == Iam::Action::"agent.configure",
    resource in Iam::Org::"grn:root/iam/org/acme"
);"#;
const NO_ONE_CONFIGURES_BOT: &str = r#"forbid (
    principal,
    action == Iam::Action::"agent.configure",
    resource == Iam::Agent::"grn:acme/iam/agent/bot"
);"#;

#[test]
fn an_operator_holding_no_operation_is_denied_under_an_open_permit_a_user_is_allowed() {
    let world = World::new();
    let permitted = world.compose(&[], &[policy(PERMIT_ID, ANYONE_CONFIGURES_ACME)]);

    let user = world.ask(&permitted, KIM, CONFIGURE, BOT);
    assert_eq!(user.outcome, Outcome::Allow);

    let idle = world.ask(&permitted, IDLE_OPERATOR, CONFIGURE, BOT);
    assert_eq!(idle.outcome, Outcome::Deny);
}

#[test]
fn an_operator_keeps_what_its_delegation_holds_and_no_permit_is_the_reason() {
    let world = World::new();
    let permitted = world.compose(&[], &[policy(PERMIT_ID, ANYONE_CONFIGURES_ACME)]);

    let held = world.ask(&permitted, OPERATOR, CONFIGURE, BOT);
    assert_eq!(held.outcome, Outcome::Allow);
    let reasons = format!("{:?}", held.reasons);
    assert!(
        !reasons.contains(PERMIT_ID),
        "the permit decided for an operator: {reasons}"
    );
}

#[test]
fn a_forbid_still_denies_an_operator_and_a_user() {
    let world = World::new();
    let narrowed = world.compose(
        &[],
        &[
            policy(PERMIT_ID, ANYONE_CONFIGURES_ACME),
            policy("grn:acme/iam/policy/not-bot", NO_ONE_CONFIGURES_BOT),
        ],
    );

    assert_eq!(
        world.ask(&narrowed, OPERATOR, CONFIGURE, BOT).outcome,
        Outcome::Deny
    );
    assert_eq!(
        world.ask(&narrowed, KIM, CONFIGURE, BOT).outcome,
        Outcome::Deny
    );
}
