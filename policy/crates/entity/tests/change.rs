//! An entity reaches the store only as the schema admits it.

#![cfg(test)]

use std::str::FromStr;

use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::Engine;
use entity::{EntityChange, Error, Upsert};

const ACME: &str = "grn:root/iam/org/acme";
const ENG: &str = "grn:acme/iam/group/eng";

fn uid(entity_type: &str, id: &str) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(entity_type).unwrap(),
        EntityId::new(id),
    )
}

fn change(
    uid: EntityUid,
    org: Option<EntityUid>,
    parents: Vec<EntityUid>,
) -> Result<EntityChange, Error> {
    EntityChange::upsert(
        &Engine::load().unwrap(),
        uid,
        1,
        Upsert {
            org,
            parents,
            ..Upsert::default()
        },
    )
}

fn acme() -> Option<EntityUid> {
    Some(uid("Iam::Org", ACME))
}

#[test]
fn a_resource_without_its_organization_is_refused() {
    let team = uid("Iam::Team", ENG);

    let refused = change(team.clone(), None, vec![uid("Iam::Org", ACME)]);
    assert!(matches!(refused, Err(Error::Refused(_))), "{refused:?}");

    let control = change(team, acme(), vec![uid("Iam::Org", ACME)]);
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_team_inside_a_team_is_refused() {
    let team = uid("Iam::Team", ENG);
    let other = uid("Iam::Team", "grn:acme/iam/group/web");

    let refused = change(team.clone(), acme(), vec![other]);
    assert!(matches!(refused, Err(Error::Refused(_))), "{refused:?}");

    let control = change(team, acme(), vec![uid("Iam::Org", ACME)]);
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_team_inside_an_organizations_members_is_refused() {
    let team = uid("Iam::Team", ENG);

    let refused = change(team.clone(), acme(), vec![uid("Iam::OrgMembers", ACME)]);
    assert!(matches!(refused, Err(Error::Refused(_))), "{refused:?}");

    let control = change(team, acme(), vec![uid("Iam::Org", ACME)]);
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_type_the_schema_does_not_name_is_refused() {
    let service = uid("Service", "grn:acme/iam/service/cowork");

    let refused = change(service, None, vec![]);
    assert!(matches!(refused, Err(Error::Refused(_))), "{refused:?}");

    let control = change(uid("Iam::OrgMembers", ACME), None, vec![]);
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn an_organization_carries_itself_and_its_own_members() {
    let engine = Engine::load().unwrap();
    let org = uid("Iam::Org", ACME);
    let members = uid("Iam::OrgMembers", ACME);
    let globex = "grn:root/iam/org/globex";
    let upsert = |org_attr: &EntityUid, members_attr: &EntityUid| {
        EntityChange::upsert(
            &engine,
            org.clone(),
            1,
            Upsert {
                org: Some(org_attr.clone()),
                members: Some(members_attr.clone()),
                ..Upsert::default()
            },
        )
    };

    let refused = upsert(&uid("Iam::Org", globex), &members);
    assert!(matches!(refused, Err(Error::OrgNotItself)), "{refused:?}");
    let refused = upsert(&org, &uid("Iam::OrgMembers", globex));
    assert!(matches!(refused, Err(Error::OrgNotItself)), "{refused:?}");

    let control = upsert(&org, &members);
    assert!(control.is_ok(), "{control:?}");
}
