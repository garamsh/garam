#![cfg(test)]

mod fixture;

use cedar_policy::PolicyId;
use decision::{Outcome, Reason, Role, RoleScope};
use fixture::{ACME, BOT, ENG, LEE, Named, WEB, World, link, organization, policy};
use serde_json::json;

/// `User` declares no `nickname`, so this `forbid` errors when read.
const ERRORING_FORBID: &str = r#"
    forbid (principal is Iam::User, action, resource)
    when { principal.nickname == "x" };
"#;

fn errored_in(reasons: &[Reason], id: &str) -> bool {
    reasons.iter().any(
        |reason| matches!(reason, Reason::Error { policy, .. } if *policy == PolicyId::new(id)),
    )
}

#[test]
fn a_forbid_that_errors_denies_what_a_role_allows() {
    let world = World::new();
    let member = link(Role::Member, RoleScope::Organization, LEE, ACME);

    let control = world.compose(std::slice::from_ref(&member), &[]);
    assert_eq!(
        world.ask(&control, LEE, "iam:org-read", ACME).outcome,
        Outcome::Allow
    );

    let policies = world.compose(&[member], &[policy("broken", ERRORING_FORBID)]);
    let decision = world.ask(&policies, LEE, "iam:org-read", ACME);
    assert_eq!(decision.outcome, Outcome::Deny);
    assert!(errored_in(&decision.reasons, "broken"), "{decision:?}");
}

#[test]
fn a_principal_missing_from_the_store_is_denied_by_isolation_itself() {
    const GHOST: Named = ("Iam::User", "grn:acme/iam/user/ghost");
    let world = World::new();
    let ghost = link(Role::Member, RoleScope::Organization, GHOST, ACME);
    let member = link(Role::Member, RoleScope::Organization, LEE, ACME);
    let policies = world.compose(&[ghost, member], &[]);

    let decision = world.ask(&policies, GHOST, "iam:agent-read", BOT);
    assert_eq!(decision.outcome, Outcome::Deny);
    assert_eq!(
        decision.reasons,
        vec![Reason::Policy(PolicyId::new("tenant-isolation"))]
    );

    let control = world.ask(&policies, LEE, "iam:agent-read", BOT);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn a_resource_whose_organization_is_missing_from_the_store_is_denied() {
    let world = World::new();
    let team = |org: serde_json::Value| {
        json!({
            "uid": {"type": "Iam::Team", "id": ENG.1},
            "attrs": {"org": {"__entity": {"type": "Iam::Org", "id": ACME.1}}},
            "parents": org,
        })
    };
    let lee = json!({
        "uid": {"type": "Iam::User", "id": LEE.1},
        "attrs": {},
        "parents": [{"type": "Iam::OrgMembers", "id": ACME.1}],
    });
    let members =
        json!({"uid": {"type": "Iam::OrgMembers", "id": ACME.1}, "attrs": {}, "parents": []});
    let orphaned = world
        .engine
        .entities(json!([team(json!([])), lee, members]))
        .unwrap();
    let complete = world
        .engine
        .entities(json!([
            team(json!([])),
            lee,
            members,
            organization(ACME, ("Iam::OrgMembers", ACME.1)),
        ]))
        .unwrap();

    let grant = link(Role::Member, RoleScope::TeamOrProject, LEE, ENG);
    let policies = world.compose(&[grant], &[]);
    let request = world
        .engine
        .request(&fixture::evaluation(LEE, "iam:group-read", ENG))
        .unwrap();

    let decision = world.engine.decide(&request, &policies, &orphaned);
    assert_eq!(decision.outcome, Outcome::Deny);
    assert!(
        errored_in(&decision.reasons, "tenant-isolation"),
        "{decision:?}"
    );

    let control = world.engine.decide(&request, &policies, &complete);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn a_resource_missing_from_the_store_is_denied_by_isolation_erroring() {
    const UNKNOWN: Named = ("Iam::Project", "grn:acme/iam/project/gone");
    let world = World::new();
    let known = link(Role::Member, RoleScope::TeamOrProject, LEE, WEB);
    let gone = link(Role::Member, RoleScope::TeamOrProject, LEE, UNKNOWN);
    let policies = world.compose(&[known, gone], &[]);

    let decision = world.ask(&policies, LEE, "iam:project-read", UNKNOWN);
    assert_eq!(decision.outcome, Outcome::Deny);
    assert!(
        errored_in(&decision.reasons, "tenant-isolation"),
        "{decision:?}"
    );

    let control = world.ask(&policies, LEE, "iam:project-read", WEB);
    assert_eq!(control.outcome, Outcome::Allow);
}
