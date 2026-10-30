#![cfg(test)]

mod fixture;

use cedar_policy::PolicyId;
use decision::{Outcome, Reason, Role, RoleScope};
use fixture::{ACME, BOT, GLOBEX, KIM, LEE, MIA, Named, RIVAL, World, link, policy};

const EVERYTHING: &str = "permit (principal, action, resource);";

fn decided_by_isolation() -> Vec<Reason> {
    vec![Reason::Policy(PolicyId::new("tenant-isolation"))]
}

#[test]
fn a_role_linked_across_organizations_is_refused() {
    let world = World::new();
    let outsider = link(Role::Owner, RoleScope::Organization, LEE, GLOBEX);
    let insider = link(Role::Owner, RoleScope::Organization, MIA, GLOBEX);
    let policies = world.compose(&[outsider, insider], &[]);

    let refused = world.ask(&policies, LEE, "agent:read", RIVAL);
    assert_eq!(refused.outcome, Outcome::Deny);
    assert_eq!(refused.reasons, decided_by_isolation());

    let control = world.ask(&policies, MIA, "agent:read", RIVAL);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn a_user_in_two_organizations_is_admitted_to_each_it_holds_a_grant_in() {
    let world = World::new();
    let in_acme = link(Role::Member, RoleScope::Organization, KIM, ACME);
    let in_globex = link(Role::Member, RoleScope::Organization, KIM, GLOBEX);

    let one = world.compose(std::slice::from_ref(&in_acme), &[]);
    assert_eq!(
        world.ask(&one, KIM, "agent:read", BOT).outcome,
        Outcome::Allow
    );
    let ungranted = world.ask(&one, KIM, "agent:read", RIVAL);
    assert_eq!(ungranted.outcome, Outcome::Deny);
    assert_eq!(ungranted.reasons, Vec::new());

    let both = world.compose(&[in_acme, in_globex], &[]);
    assert_eq!(
        world.ask(&both, KIM, "agent:read", BOT).outcome,
        Outcome::Allow
    );
    assert_eq!(
        world.ask(&both, KIM, "agent:read", RIVAL).outcome,
        Outcome::Allow
    );
}

#[test]
fn no_policy_overrides_isolation_on_any_action_an_organization_answers() {
    let world = World::new();
    let policies = world.compose(&[], &[policy("everything", EVERYTHING)]);
    let mut decided = 0;

    for action in world.engine.schema().actions() {
        let name = AsRef::<str>::as_ref(action.id());
        let ask = |org: Named| {
            let question = fixture::evaluation(LEE, name, org);
            let request = world.engine.request(&question).ok()?;
            Some(world.engine.decide(&request, &policies, &world.entities))
        };
        let (Some(foreign), Some(own)) = (ask(GLOBEX), ask(ACME)) else {
            continue;
        };
        decided += 1;
        assert_eq!(foreign.outcome, Outcome::Deny, "{name} crossed");
        assert_eq!(foreign.reasons, decided_by_isolation(), "{name}");
        assert_eq!(own.outcome, Outcome::Allow, "{name} control");
    }

    assert!(decided > 50, "only {decided} actions were decided");
}
