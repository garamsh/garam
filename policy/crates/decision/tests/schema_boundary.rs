#![cfg(test)]

mod fixture;

use decision::{Engine, Error, Role, RoleScope};
use fixture::{ACME, ENG, LEE, WEB, World, evaluation, link};
use serde_json::{Value, json};

fn engine() -> Engine {
    Engine::load().expect("the system tier loads")
}

#[test]
fn an_action_the_schema_does_not_name_is_refused() {
    let engine = engine();

    let refused = engine.request(&evaluation(LEE, "org:rename", ACME));
    assert!(matches!(refused, Err(Error::Evaluation(_))), "{refused:?}");

    let control = engine.request(&evaluation(LEE, "org:read", ACME));
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn an_action_asked_of_a_resource_type_it_does_not_apply_to_is_refused() {
    let engine = engine();

    let refused = engine.request(&evaluation(LEE, "org:read", WEB));
    assert!(matches!(refused, Err(Error::Evaluation(_))), "{refused:?}");

    let control = engine.request(&evaluation(LEE, "project:read", WEB));
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_context_attribute_the_schema_does_not_name_is_refused() {
    let engine = engine();
    let mut question = evaluation(LEE, "org:read", ACME);
    question.context = json!({"hour": 3});

    let refused = engine.request(&question);
    assert!(matches!(refused, Err(Error::Evaluation(_))), "{refused:?}");
}

fn team(id: &str, attrs: Value, parents: Value) -> Value {
    json!([
        {"uid": {"type": "Org", "id": ACME.1},
         "attrs": {"org": {"__entity": {"type": "Org", "id": ACME.1}}},
         "parents": []},
        {"uid": {"type": "Team", "id": id}, "attrs": attrs, "parents": parents},
    ])
}

#[test]
fn a_resource_without_its_organization_is_refused() {
    let engine = engine();
    let org = json!({"org": {"__entity": {"type": "Org", "id": ACME.1}}});
    let parents = json!([{"type": "Org", "id": ACME.1}]);

    let refused = engine.entities(team(ENG.1, json!({}), parents.clone()));
    assert!(matches!(refused, Err(Error::Entities(_))), "{refused:?}");

    let control = engine.entities(team(ENG.1, org, parents));
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_team_inside_a_team_is_refused() {
    let engine = engine();
    let org = json!({"org": {"__entity": {"type": "Org", "id": ACME.1}}});

    let refused = engine.entities(team(
        "grn:acme:default:group:nested",
        org.clone(),
        json!([{"type": "Team", "id": "grn:acme:default:group:outer"}]),
    ));
    assert!(matches!(refused, Err(Error::Entities(_))), "{refused:?}");

    let control = engine.entities(team(
        "grn:acme:default:group:flat",
        org,
        json!([{"type": "Org", "id": ACME.1}]),
    ));
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_role_link_outside_its_template_set_is_refused() {
    let world = World::new();

    let org_role_on_a_team = link(Role::Admin, RoleScope::Organization, LEE, ENG);
    let refused = world.engine.compose(&[org_role_on_a_team], &[]);
    assert!(
        matches!(refused, Err(Error::RoleLinkScope { .. })),
        "{refused:?}"
    );

    let scoped_role_on_an_org = link(Role::Admin, RoleScope::TeamOrProject, LEE, ACME);
    let refused = world.engine.compose(&[scoped_role_on_an_org], &[]);
    assert!(
        matches!(refused, Err(Error::RoleLinkScope { .. })),
        "{refused:?}"
    );

    let control = link(Role::Admin, RoleScope::Organization, LEE, ACME);
    assert!(world.engine.compose(&[control], &[]).is_ok());
}
