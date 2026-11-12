//! The one exception to tenant isolation: a send between two agents
//! of two organizations that each consent to the other (ADR-0147).

#![cfg(test)]

mod fixture;

use cedar_policy::{EntityUid, Policy};
use decision::{
    Cause, EntitySlice, EntitySource, OrgGrn, OrgPolicies, Outcome, PolicySource, Reason, Role,
    RoleScope,
};
use fixture::*;

const ISOLATION: &str = "tenant-isolation";

fn decided(world: &World, who: Named, action: &str, what: Named) -> decision::Decision {
    let policies = world.compose(&[], &[]);
    world.ask(&policies, who, action, what)
}

#[test]
fn a_send_is_allowed_where_each_organization_consents_to_the_other() {
    let world = World::consenting(&[GLOBEX], &[ACME]);

    let sent = decided(&world, BOT, "iam:message.send", RIVAL);
    assert_eq!(sent.outcome, Outcome::Allow, "{sent:?}");
    let back = decided(&world, RIVAL, "iam:message.send", BOT);
    assert_eq!(back.outcome, Outcome::Allow, "{back:?}");
}

#[test]
fn a_send_is_denied_where_either_half_is_missing() {
    for (acme, globex) in [
        (&[GLOBEX][..], &[][..]),
        (&[][..], &[ACME][..]),
        (&[][..], &[][..]),
    ] {
        let world = World::consenting(acme, globex);
        let sent = decided(&world, BOT, "iam:message.send", RIVAL);
        assert_eq!(sent.outcome, Outcome::Deny);
        assert_eq!(sent.reasons, vec![reason(ISOLATION)]);
    }
    // The control: both halves, in the same fixture, allow.
    let both = World::consenting(&[GLOBEX], &[ACME]);
    assert_eq!(
        decided(&both, BOT, "iam:message.send", RIVAL).outcome,
        Outcome::Allow
    );
}

#[test]
fn a_consent_to_a_third_organization_is_not_a_consent_to_this_one() {
    let world = World::consenting(&[ACME], &[ACME]);
    let sent = decided(&world, BOT, "iam:message.send", RIVAL);
    assert_eq!(sent.outcome, Outcome::Deny);
    assert_eq!(sent.reasons, vec![reason(ISOLATION)]);
}

#[test]
fn both_halves_lift_isolation_for_a_send_and_for_no_other_action() {
    let world = World::consenting(&[GLOBEX], &[ACME]);
    let everything = policy("everything", "permit (principal, action, resource);");
    let policies = world.compose(&[], &[everything]);
    let mut asked = 0;
    for action in ["iam:agent.read", "iam:agent.configure", "iam:agent.watch"] {
        let Ok(request) = world.engine.request(&evaluation(BOT, action, RIVAL)) else {
            continue;
        };
        asked += 1;
        let decision = world.engine.decide(&request, &policies, &world.entities);
        assert_eq!(decision.outcome, Outcome::Deny, "{action}");
        assert_eq!(decision.reasons, vec![reason(ISOLATION)], "{action}");
    }
    assert!(asked > 0, "no action was asked");
    let control = world.ask(&policies, BOT, "iam:message.send", RIVAL);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn a_user_does_not_send_across_organizations_on_an_agents_consent() {
    let world = World::consenting(&[GLOBEX], &[ACME]);
    let everything = policy("everything", "permit (principal, action, resource);");
    let policies = world.compose(&[], &[everything]);
    let request = world
        .engine
        .request(&evaluation(LEE, "iam:message.send", RIVAL));
    // `message.send` asks of an agent, so a user cannot even be asked.
    assert!(request.is_err(), "a user was admitted as a sender");
    let control = world.ask(&policies, BOT, "iam:message.send", RIVAL);
    assert_eq!(control.outcome, Outcome::Allow);
}

struct Organizations {
    acme: Vec<Policy>,
    globex: Vec<Policy>,
}

impl PolicySource for Organizations {
    async fn active(&self, org: &OrgGrn) -> Result<OrgPolicies, Cause> {
        let guardrails = match org.as_str() {
            id if id == ACME.1 => self.acme.clone(),
            id if id == GLOBEX.1 => self.globex.clone(),
            _ => vec![],
        };
        Ok(OrgPolicies {
            links: vec![],
            guardrails,
            permits: vec![],
        })
    }
}

struct Store(World);

impl EntitySource for Store {
    async fn slice(&self, _: &EntityUid, resource: &EntityUid) -> Result<EntitySlice, Cause> {
        let org = match resource.id().unescaped() {
            id if id.contains("globex") => GLOBEX.1,
            _ => ACME.1,
        };
        Ok(EntitySlice {
            entities: self.0.entities.clone(),
            org: Some(OrgGrn::new(org)),
        })
    }
}

async fn send(acme: Vec<Policy>, globex: Vec<Policy>) -> decision::Decision {
    let world = World::consenting(&[GLOBEX], &[ACME]);
    let engine = world.engine.clone();
    engine
        .evaluate(
            &Organizations { acme, globex },
            &Store(world),
            &evaluation(BOT, "iam:message.send", RIVAL),
        )
        .await
        .unwrap()
}

#[tokio::test]
async fn the_senders_guardrail_forbids_a_send_the_recipient_would_take() {
    let allowed = send(vec![], vec![]).await;
    assert_eq!(allowed.outcome, Outcome::Allow, "{allowed:?}");

    let keep_quiet = policy(
        "keep-bot-quiet",
        r#"forbid (principal == Iam::Agent::"grn:acme/iam/agent/bot",
            action == Iam::Action::"message.send", resource);"#,
    );
    let refused = send(vec![keep_quiet], vec![]).await;
    assert_eq!(refused.outcome, Outcome::Deny, "{refused:?}");
    assert_eq!(
        refused.reasons,
        vec![reason("grn:root/iam/org/acme/keep-bot-quiet")]
    );
}

#[tokio::test]
async fn the_recipients_guardrail_forbids_a_send_the_sender_would_make() {
    let keep_out = policy(
        "keep-bot-out",
        r#"forbid (principal, action == Iam::Action::"message.send",
            resource == Iam::Agent::"grn:globex/iam/agent/rival");"#,
    );
    let refused = send(vec![], vec![keep_out]).await;
    assert_eq!(refused.outcome, Outcome::Deny, "{refused:?}");
    assert_eq!(refused.reasons, vec![reason("keep-bot-out")]);
}

#[tokio::test]
async fn two_organizations_may_name_a_guardrail_alike() {
    let acme = policy(
        "no-send",
        r#"forbid (principal == Iam::Agent::"grn:acme/iam/agent/bot",
            action == Iam::Action::"message.send", resource);"#,
    );
    let globex = policy(
        "no-send",
        r#"forbid (principal, action == Iam::Action::"message.send",
            resource == Iam::Agent::"grn:globex/iam/agent/rival");"#,
    );
    let refused = send(vec![acme], vec![globex]).await;
    assert_eq!(refused.outcome, Outcome::Deny, "{refused:?}");
    assert_eq!(refused.reasons.len(), 2, "{refused:?}");
}

#[tokio::test]
async fn a_senders_permit_is_not_read_only_its_forbids() {
    // The sender's organization holds a link; only the recipient's
    // links decide a send, so the sender's grant adds nothing.
    struct Linked;
    impl PolicySource for Linked {
        async fn active(&self, org: &OrgGrn) -> Result<OrgPolicies, Cause> {
            let links = if org.as_str() == ACME.1 {
                vec![link(Role::Owner, RoleScope::Organization, LEE, ACME)]
            } else {
                vec![]
            };
            Ok(OrgPolicies {
                links,
                guardrails: vec![],
                permits: vec![],
            })
        }
    }
    let world = World::consenting(&[GLOBEX], &[ACME]);
    let engine = world.engine.clone();
    let decision = engine
        .evaluate(
            &Linked,
            &Store(world),
            &evaluation(BOT, "iam:message.send", RIVAL),
        )
        .await
        .unwrap();
    // Allowed by the tier's own permit, with no acme link in the
    // reasons: the sender's links never reach the composition.
    assert_eq!(decision.outcome, Outcome::Allow, "{decision:?}");
    assert!(
        decision
            .reasons
            .iter()
            .all(|reason| !matches!(reason, Reason::Policy(id) if id.to_string().contains("link"))),
        "{decision:?}"
    );
}

fn reason(id: &str) -> Reason {
    Reason::Policy(cedar_policy::PolicyId::new(id))
}
