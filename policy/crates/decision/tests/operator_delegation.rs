//! A hosted operator's delegated operations (ADR-0146 row 15): one
//! system policy per operation, permitting an operator of the
//! resource's organization that holds it.

#![cfg(test)]

mod fixture;

use cedar_policy::PolicyId;
use decision::{Outcome, Reason};
use fixture::*;

fn reason(id: &str) -> Reason {
    Reason::Policy(PolicyId::new(id))
}

#[test]
fn an_operator_does_what_it_holds_in_its_own_organization() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let activated = world.ask(&policies, OPERATOR, "iam:agent.activate", BOT);
    assert_eq!(activated.outcome, Outcome::Allow, "{activated:?}");
    assert_eq!(activated.reasons, vec![reason("operator-agent-activate")]);
    let configured = world.ask(&policies, OPERATOR, "iam:agent.configure", BOT);
    assert_eq!(configured.outcome, Outcome::Allow, "{configured:?}");
}

#[test]
fn an_operator_is_denied_a_delegable_operation_it_does_not_hold() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let recovered = world.ask(&policies, OPERATOR, "iam:agent.recover", BOT);
    assert_eq!(recovered.outcome, Outcome::Deny);
    assert_eq!(recovered.reasons, Vec::new());
    // The control: the same operator, an operation it holds.
    let control = world.ask(&policies, OPERATOR, "iam:agent.configure", BOT);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn an_operator_holding_another_operation_is_denied_this_one() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let refused = world.ask(&policies, CONFIGURE_ONLY, "iam:agent.activate", BOT);
    assert_eq!(refused.outcome, Outcome::Deny);
    assert_eq!(refused.reasons, Vec::new());
    let control = world.ask(&policies, CONFIGURE_ONLY, "iam:agent.configure", BOT);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn an_operator_holding_no_operations_is_denied_every_one() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let idle = world.ask(&policies, IDLE_OPERATOR, "iam:agent.activate", BOT);
    assert_eq!(idle.outcome, Outcome::Deny);
    assert_eq!(idle.reasons, Vec::new());
    let control = world.ask(&policies, OPERATOR, "iam:agent.activate", BOT);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn an_operator_is_denied_in_an_organization_that_is_not_its_own() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let foreign = world.ask(&policies, OPERATOR, "iam:agent.activate", RIVAL);
    assert_eq!(foreign.outcome, Outcome::Deny);
    assert_eq!(foreign.reasons, vec![reason("tenant-isolation")]);
    let other = world.ask(&policies, GLOBEX_OPERATOR, "iam:agent.activate", BOT);
    assert_eq!(other.outcome, Outcome::Deny);
    assert_eq!(other.reasons, vec![reason("tenant-isolation")]);
    let control = world.ask(&policies, GLOBEX_OPERATOR, "iam:agent.activate", RIVAL);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn a_guardrail_forbids_an_operator_as_it_forbids_any_principal() {
    let world = World::new();
    let guardrail = policy(
        "no-activation",
        r#"forbid (principal, action == Iam::Action::"agent.activate", resource);"#,
    );
    let policies = world.compose(&[], &[guardrail]);

    let refused = world.ask(&policies, OPERATOR, "iam:agent.activate", BOT);
    assert_eq!(refused.outcome, Outcome::Deny);
    assert_eq!(refused.reasons, vec![reason("no-activation")]);
    let control = world.ask(&policies, OPERATOR, "iam:agent.configure", BOT);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn an_operator_is_no_principal_of_an_operation_that_is_not_delegable() {
    let world = World::new();
    for action in [
        "iam:agent.read",
        "iam:agent.rename",
        "iam:agent.enroll",
        "iam:message.send",
        "iam:org.delete",
    ] {
        let asked = world.engine.request(&evaluation(OPERATOR, action, BOT));
        assert!(asked.is_err(), "an operator may be asked {action}");
    }
    // The control: the schema admits the same operator on a delegable
    // one.
    assert!(
        world
            .engine
            .request(&evaluation(OPERATOR, "iam:agent.activate", BOT))
            .is_ok()
    );
}
