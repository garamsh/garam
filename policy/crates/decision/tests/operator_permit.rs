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
    let own = [policy(PERMIT_ID, ANYONE_CONFIGURES_ACME)];

    let for_user = world.compose_as(KIM, &[], &own);
    let user = world.ask(&for_user, KIM, CONFIGURE, BOT);
    assert_eq!(user.outcome, Outcome::Allow);

    let for_idle = world.compose_as(IDLE_OPERATOR, &[], &own);
    let idle = world.ask(&for_idle, IDLE_OPERATOR, CONFIGURE, BOT);
    assert_eq!(idle.outcome, Outcome::Deny);
}

#[test]
fn an_operator_keeps_what_its_delegation_holds_and_no_permit_is_the_reason() {
    let world = World::new();
    let own = [policy(PERMIT_ID, ANYONE_CONFIGURES_ACME)];
    let for_operator = world.compose_as(OPERATOR, &[], &own);

    let held = world.ask(&for_operator, OPERATOR, CONFIGURE, BOT);
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
    let own = [
        policy(PERMIT_ID, ANYONE_CONFIGURES_ACME),
        policy("grn:acme/iam/policy/not-bot", NO_ONE_CONFIGURES_BOT),
    ];

    let for_operator = world.compose_as(OPERATOR, &[], &own);
    let for_user = world.compose_as(KIM, &[], &own);
    assert_eq!(
        world.ask(&for_operator, OPERATOR, CONFIGURE, BOT).outcome,
        Outcome::Deny
    );
    assert_eq!(
        world.ask(&for_user, KIM, CONFIGURE, BOT).outcome,
        Outcome::Deny
    );
}

/// What the guard reads is the policy's effect, never its text: each of
/// these is a `permit` a user is allowed by and an idle operator is not.
#[test]
fn a_permit_is_kept_from_an_operator_whatever_surrounds_its_text() {
    let world = World::new();
    let shapes = [
        format!("{ANYONE_CONFIGURES_ACME} // a note"),
        format!("{ANYONE_CONFIGURES_ACME} // a; b;"),
        format!("// a; b;\n{ANYONE_CONFIGURES_ACME}"),
        ANYONE_CONFIGURES_ACME.replace(");", ") unless { principal == Iam::User::\"nobody\" };"),
    ];
    for source in shapes {
        let own = [policy(PERMIT_ID, &source)];
        let for_user = world.compose_as(KIM, &[], &own);
        assert_eq!(
            world.ask(&for_user, KIM, CONFIGURE, BOT).outcome,
            Outcome::Allow,
            "{source}"
        );
        let for_idle = world.compose_as(IDLE_OPERATOR, &[], &own);
        assert_eq!(
            world.ask(&for_idle, IDLE_OPERATOR, CONFIGURE, BOT).outcome,
            Outcome::Deny,
            "{source}"
        );
    }
}
