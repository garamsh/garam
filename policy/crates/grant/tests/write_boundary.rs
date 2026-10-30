//! What reaches the store: only a body the boundary admitted.

#![cfg(test)]

use std::str::FromStr;

use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, Role, RoleScope};
use grant::{Body, Error};

const ACME: &str = "grn:root:default:org:acme";

fn uid(entity_type: &str, id: &str) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(entity_type).unwrap(),
        EntityId::new(id),
    )
}

fn org() -> EntityUid {
    uid("Org", ACME)
}

fn user() -> EntityUid {
    uid("User", "grn:acme:default:user:lee")
}

fn team() -> EntityUid {
    uid("Team", "grn:acme:default:group:eng")
}

#[test]
fn an_org_as_a_links_principal_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::role_link(&engine, Role::Member, RoleScope::Organization, org(), org());
    assert!(
        matches!(
            refused,
            Err(Error::Refused(decision::Error::RoleLinkPrincipal { .. }))
        ),
        "{refused:?}"
    );

    for principal in [user(), team()] {
        let control = Body::role_link(
            &engine,
            Role::Member,
            RoleScope::Organization,
            principal,
            org(),
        );
        assert!(control.is_ok(), "{control:?}");
    }
}

#[test]
fn a_links_resource_outside_its_template_set_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::role_link(
        &engine,
        Role::Admin,
        RoleScope::Organization,
        user(),
        team(),
    );
    assert!(
        matches!(
            refused,
            Err(Error::Refused(decision::Error::RoleLinkScope { .. }))
        ),
        "{refused:?}"
    );

    let control = Body::role_link(
        &engine,
        Role::Admin,
        RoleScope::TeamOrProject,
        user(),
        team(),
    );
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_permit_guardrail_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::guardrail(&engine, "permit (principal, action, resource);");
    assert!(
        matches!(refused, Err(Error::GuardrailPermits)),
        "{refused:?}"
    );

    let control = Body::guardrail(&engine, "forbid (principal, action, resource);");
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_guardrail_the_schema_does_not_admit_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::guardrail(
        &engine,
        r#"forbid (principal, action == Action::"no:such", resource);"#,
    );
    assert!(
        matches!(refused, Err(Error::Refused(decision::Error::Validation(_)))),
        "{refused:?}"
    );

    let refused = Body::guardrail(
        &engine,
        "forbid (principal is User, action, resource) when { principal.org == resource.org };",
    );
    assert!(
        matches!(refused, Err(Error::Refused(decision::Error::Validation(_)))),
        "{refused:?}"
    );

    let control = Body::guardrail(
        &engine,
        r#"forbid (principal is Agent, action == Action::"message:send", resource is Agent)
           when { principal.org != resource.org };"#,
    );
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn text_that_is_not_one_policy_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::guardrail(&engine, "forbid (principal");
    assert!(
        matches!(refused, Err(Error::GuardrailSyntax(_))),
        "{refused:?}"
    );

    let two = "forbid (principal, action, resource); forbid (principal, action, resource);";
    let refused = Body::guardrail(&engine, two);
    assert!(
        matches!(refused, Err(Error::GuardrailSyntax(_))),
        "{refused:?}"
    );
}
