#![cfg(test)]

mod fixture;

use cedar_policy::PolicyId;
use decision::{Outcome, Reason, Role, RoleScope};
use fixture::{ACME, LEE, Named, WEB, World, link, policy};

/// `User` declares no `nickname`, so this `forbid` errors when read.
const ERRORING_FORBID: &str = r#"
    forbid (principal is User, action, resource)
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
        world.ask(&control, LEE, "org:read", ACME).outcome,
        Outcome::Allow
    );

    let policies = world.compose(&[member], &[policy("broken", ERRORING_FORBID)]);
    let decision = world.ask(&policies, LEE, "org:read", ACME);
    assert_eq!(decision.outcome, Outcome::Deny);
    assert!(errored_in(&decision.reasons, "broken"), "{decision:?}");
}

#[test]
fn a_resource_missing_from_the_store_is_denied_by_isolation_erroring() {
    const UNKNOWN: Named = ("Project", "grn:acme:default:project:gone");
    let world = World::new();
    let known = link(Role::Member, RoleScope::TeamOrProject, LEE, WEB);
    let gone = link(Role::Member, RoleScope::TeamOrProject, LEE, UNKNOWN);
    let policies = world.compose(&[known, gone], &[]);

    let decision = world.ask(&policies, LEE, "project:read", UNKNOWN);
    assert_eq!(decision.outcome, Outcome::Deny);
    assert!(
        errored_in(&decision.reasons, "tenant-isolation"),
        "{decision:?}"
    );

    let control = world.ask(&policies, LEE, "project:read", WEB);
    assert_eq!(control.outcome, Outcome::Allow);
}
