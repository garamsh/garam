#![cfg(test)]

mod fixture;

use cedar_policy::PolicyId;
use decision::{Outcome, Reason};
use fixture::{BOT, PEER, RIVAL, World, policy};

#[test]
fn an_agent_sends_inside_its_organization_with_no_policy_written() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let decision = world.ask(&policies, BOT, "message:send", PEER);
    assert_eq!(decision.outcome, Outcome::Allow);
    assert_eq!(
        decision.reasons,
        vec![Reason::Policy(PolicyId::new("agent-message-send"))]
    );
}

#[test]
fn an_agent_sends_to_no_agent_of_another_organization() {
    let world = World::new();
    let policies = world.compose(&[], &[]);

    let decision = world.ask(&policies, BOT, "message:send", RIVAL);
    assert_eq!(decision.outcome, Outcome::Deny);
    assert_eq!(
        decision.reasons,
        vec![Reason::Policy(PolicyId::new("tenant-isolation"))]
    );
}

#[test]
fn an_organization_narrows_sending_with_a_forbid() {
    let world = World::new();
    let guardrail = policy(
        "keep-bot-quiet",
        r#"forbid (
            principal == Agent::"grn:acme:default:agent:bot",
            action == Action::"message:send",
            resource
        );"#,
    );
    let policies = world.compose(&[], &[guardrail]);

    let refused = world.ask(&policies, BOT, "message:send", PEER);
    assert_eq!(refused.outcome, Outcome::Deny);
    assert_eq!(
        refused.reasons,
        vec![Reason::Policy(PolicyId::new("keep-bot-quiet"))]
    );

    let control = world.ask(&policies, PEER, "message:send", BOT);
    assert_eq!(control.outcome, Outcome::Allow);
}
