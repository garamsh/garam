//! A standalone host's supervisor activates the agent it holds and
//! nothing else (ADR-0164): one system policy, the supervisor a
//! principal of that one action, and no organization `permit` reaching
//! it.

#![cfg(test)]

mod fixture;

use cedar_policy::PolicyId;
use decision::{EntityRef, Evaluation, Outcome, Reason};
use fixture::*;
use serde_json::json;

const ACTIVATE: &str = "iam:agent.activate";
const PERMIT_ID: &str = "grn:acme/iam/policy/anyone-activates";
const ANYONE_ACTIVATES_ACME: &str = r#"permit (
    principal,
    action == Iam::Action::"agent.activate",
    resource in Iam::Org::"grn:root/iam/org/acme"
);"#;

fn reason(id: &str) -> Reason {
    Reason::Policy(PolicyId::new(id))
}

#[test]
fn a_supervisor_activates_the_agent_it_holds_and_no_other_agent_of_its_organization() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let held = world.ask(&policies, SUPERVISOR, ACTIVATE, BOT);
    assert_eq!(held.outcome, Outcome::Allow, "{held:?}");
    assert_eq!(held.reasons, vec![reason("supervisor-agent-activate")]);

    // Same organization, the same system tier, an agent it does not
    // hold: only the policy's own condition refuses it.
    let other = world.ask(&policies, SUPERVISOR, ACTIVATE, PEER);
    assert_eq!(other.outcome, Outcome::Deny);
    assert_eq!(other.reasons, Vec::new());
}

#[test]
fn a_supervisor_is_denied_an_agent_of_another_organization() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let foreign = world.ask(&policies, SUPERVISOR, ACTIVATE, RIVAL);
    assert_eq!(foreign.outcome, Outcome::Deny);

    // Fed holding a globex agent, acme's supervisor meets the policy's
    // condition, so the refusal is tenant isolation's alone.
    let misfed = world.ask(&policies, MISFED_SUPERVISOR, ACTIVATE, RIVAL);
    assert_eq!(misfed.outcome, Outcome::Deny);
    assert_eq!(misfed.reasons, vec![reason("tenant-isolation")]);
    // The control: globex's own supervisor, the same agent.
    let control = world.ask(&policies, GLOBEX_SUPERVISOR, ACTIVATE, RIVAL);
    assert_eq!(control.outcome, Outcome::Allow, "{control:?}");
}

#[test]
fn a_supervisor_fed_with_no_agent_is_denied_and_the_one_holding_an_agent_is_not() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let idle = world.ask(&policies, IDLE_SUPERVISOR, ACTIVATE, BOT);
    assert_eq!(idle.outcome, Outcome::Deny);
    // No evaluation error either: the condition reads `has` first.
    assert_eq!(idle.reasons, Vec::new());
    let control = world.ask(&policies, SUPERVISOR, ACTIVATE, BOT);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn a_guardrail_forbids_a_supervisor_as_it_forbids_any_principal() {
    let world = World::new();
    let guardrail = policy(
        "no-activation",
        r#"forbid (principal, action == Iam::Action::"agent.activate", resource);"#,
    );
    let policies = world.compose(&[], &[guardrail]);

    let refused = world.ask(&policies, SUPERVISOR, ACTIVATE, BOT);
    assert_eq!(refused.outcome, Outcome::Deny);
    assert_eq!(refused.reasons, vec![reason("no-activation")]);
    let control = world.ask(&world.compose(&[], &[]), SUPERVISOR, ACTIVATE, BOT);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn no_organization_permit_reaches_a_supervisor() {
    let world = World::new();
    let own = [policy(PERMIT_ID, ANYONE_ACTIVATES_ACME)];

    let for_supervisor = world.compose_as(SUPERVISOR, &[], &own);
    let other = world.ask(&for_supervisor, SUPERVISOR, ACTIVATE, PEER);
    assert_eq!(other.outcome, Outcome::Deny);
    let idle_for = world.compose_as(IDLE_SUPERVISOR, &[], &own);
    let idle = world.ask(&idle_for, IDLE_SUPERVISOR, ACTIVATE, BOT);
    assert_eq!(idle.outcome, Outcome::Deny);

    // The controls: the permit allows a user of the organization, and
    // the supervisor keeps the agent it holds.
    let for_user = world.compose_as(KIM, &[], &own);
    assert_eq!(
        world.ask(&for_user, KIM, ACTIVATE, PEER).outcome,
        Outcome::Allow
    );
    let held = world.ask(&for_supervisor, SUPERVISOR, ACTIVATE, BOT);
    assert_eq!(held.outcome, Outcome::Allow);
    assert_eq!(held.reasons, vec![reason("supervisor-agent-activate")]);
}

/// The manifests admit a supervisor as a principal of one action. For
/// every other action, the question is refused with a principal the
/// action admits asked beside it, so nothing but the supervisor's type
/// separates the two.
#[test]
fn a_supervisor_is_no_principal_of_any_other_action() {
    let world = World::new();
    let id = |entity_type: &str| format!("grn:acme/iam/probe/{entity_type}");
    let ask = |principal_type: &str, action: &str, resource_type: &str| {
        let (principal, resource) = (id(principal_type), id(resource_type));
        world.engine.request(&Evaluation {
            subject: EntityRef {
                entity_type: principal_type,
                id: &principal,
            },
            action,
            resource: EntityRef {
                entity_type: resource_type,
                id: &resource,
            },
            context: json!({}),
        })
    };

    let (mut admitting, mut others) = (Vec::new(), 0);
    for service in world.engine.manifests().services() {
        for action in &service.actions {
            let Some(applies) = &action.applies_to else {
                continue;
            };
            let resource = &applies.resources[0];
            let admitted = &applies.principals[0];
            let control = ask(admitted, &action.name, resource);
            assert!(
                control.is_ok(),
                "{} is not askable: {control:?}",
                action.name
            );
            let supervisor = ask("Iam::Supervisor", &action.name, resource);
            if applies
                .principals
                .iter()
                .any(|kind| kind == "Iam::Supervisor")
            {
                assert!(
                    supervisor.is_ok(),
                    "{} refused: {supervisor:?}",
                    action.name
                );
                admitting.push(action.name.clone());
            } else {
                assert!(
                    supervisor.is_err(),
                    "a supervisor may be asked {}",
                    action.name
                );
                others += 1;
            }
        }
    }
    assert_eq!(admitting, vec![ACTIVATE.to_owned()]);
    assert!(others > 50, "only {others} other actions were checked");
}
