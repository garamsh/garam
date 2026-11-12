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

fn put(uid: EntityUid, upsert: Upsert) -> Result<EntityChange, Error> {
    EntityChange::upsert(&Engine::load().unwrap(), uid, 1, upsert)
}

fn organization(consents: Vec<EntityUid>) -> Upsert {
    Upsert {
        org: acme(),
        members: Some(uid("Iam::OrgMembers", ACME)),
        consents,
        ..Upsert::default()
    }
}

fn operator(operations: &[&str]) -> Upsert {
    Upsert {
        org: acme(),
        parents: vec![uid("Iam::OrgMembers", ACME), uid("Iam::Org", ACME)],
        operations: operations.iter().map(ToString::to_string).collect(),
        ..Upsert::default()
    }
}

#[test]
fn an_organization_states_the_organizations_it_consents_to() {
    let globex = uid("Iam::Org", "grn:root/iam/org/globex");
    let org = uid("Iam::Org", ACME);

    let consenting = put(org.clone(), organization(vec![globex]));
    assert!(consenting.is_ok(), "{consenting:?}");
    let silent = put(org, organization(vec![]));
    assert!(silent.is_ok(), "{silent:?}");

    // A consent names an organization: an agent in its place is not
    // one the schema admits.
    let bot = uid("Iam::Agent", "grn:acme/iam/agent/bot");
    let refused = put(uid("Iam::Org", ACME), organization(vec![bot]));
    assert!(matches!(refused, Err(Error::Refused(_))), "{refused:?}");
}

#[test]
fn consents_belong_to_an_organization_alone() {
    let globex = uid("Iam::Org", "grn:root/iam/org/globex");
    let bot = uid("Iam::Agent", "grn:acme/iam/agent/bot");
    let with = |consents: Vec<EntityUid>| Upsert {
        org: acme(),
        parents: vec![uid("Iam::OrgMembers", ACME)],
        consents,
        ..Upsert::default()
    };

    let refused = put(bot.clone(), with(vec![globex]));
    assert!(
        matches!(refused, Err(Error::Attribute("consents"))),
        "{refused:?}"
    );
    let control = put(bot, with(vec![]));
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn an_operator_states_the_operations_its_delegation_holds() {
    let hosting = uid("Iam::Operator", "grn:acme/iam/operator/hosting");

    let held = put(hosting.clone(), operator(&["iam:agent.activate"]));
    assert!(held.is_ok(), "{held:?}");

    let wire_form = put(hosting.clone(), operator(&["agent.activate"]));
    assert!(
        matches!(wire_form, Err(Error::NotDelegable(_))),
        "{wire_form:?}"
    );
    let refused = put(hosting, operator(&["iam:agent.read"]));
    assert!(
        matches!(refused, Err(Error::NotDelegable(ref name)) if name == "iam:agent.read"),
        "{refused:?}"
    );
}

#[test]
fn operations_belong_to_an_operator_alone() {
    let bot = uid("Iam::Agent", "grn:acme/iam/agent/bot");
    let with = |operations: Vec<String>| Upsert {
        org: acme(),
        parents: vec![uid("Iam::OrgMembers", ACME)],
        operations,
        ..Upsert::default()
    };

    let refused = put(bot.clone(), with(vec!["iam:agent.activate".to_owned()]));
    assert!(
        matches!(refused, Err(Error::Attribute("operations"))),
        "{refused:?}"
    );
    let control = put(bot, with(vec![]));
    assert!(control.is_ok(), "{control:?}");
}
