//! What a report lists, over fake stores: it reads no database.

#![cfg(test)]

use std::str::FromStr;

use access_report::{Entry, Error, for_resource, for_subject};
use cedar_policy::{EntityId, EntityTypeName, EntityUid, Policy, PolicyId};
use decision::{
    Cause, Engine, EntitySlice, EntitySource, OrgGrn, OrgPolicies, PolicySource, Role, RoleLink,
    RoleScope,
};
use serde_json::{Value, json};

type Named = (&'static str, &'static str);

const ACME: &str = "grn:root:default:org:acme";
const KIM: Named = ("User", "grn:acme:default:user:kim");
const LEE: Named = ("User", "grn:acme:default:user:lee");
const ENG: Named = ("Team", "grn:acme:default:group:eng");
const OPS: Named = ("Team", "grn:acme:default:group:ops");
const BOT: Named = ("Agent", "grn:acme:default:agent:bot");
const WEB: Named = ("Project", "grn:acme:default:project:web");
const RIVALS: Named = ("Team", "grn:globex:default:group:secret");
const ORG: Named = ("Org", ACME);
const MEMBERS: Named = ("OrgMembers", ACME);

const ON_ENG: &str =
    r#"forbid (principal in Team::"grn:acme:default:group:eng", action, resource);"#;
const ON_OPS_IN_CONDITION: &str = r#"forbid (principal, action, resource is Agent)
    when { principal in Team::"grn:acme:default:group:ops" };"#;
const EVERYONE: &str = "forbid (principal, action, resource);";
const ON_ENG_ALONE: &str =
    r#"forbid (principal == Team::"grn:acme:default:group:eng", action, resource);"#;
const ON_AGENTS: &str = "forbid (principal is Agent, action, resource);";
const ON_LEE: &str =
    r#"forbid (principal == User::"grn:acme:default:user:lee", action, resource);"#;

fn uid(named: Named) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(named.0).unwrap(),
        EntityId::new(named.1),
    )
}

fn reference(named: Named) -> Value {
    json!({"type": named.0, "id": named.1})
}

fn entity(named: Named, attrs: Value, parents: &[Named]) -> Value {
    let parents: Vec<Value> = parents.iter().copied().map(reference).collect();
    json!({"uid": reference(named), "attrs": attrs, "parents": parents})
}

/// Kim is in eng and ops, lee in no team; bot is in eng and the org.
struct Directory(Engine);

impl EntitySource for Directory {
    async fn slice(&self, _: &EntityUid, _: &EntityUid) -> Result<EntitySlice, Cause> {
        let org = json!({"org": {"__entity": reference(ORG)}});
        let organization = json!({
            "org": {"__entity": reference(ORG)},
            "members": {"__entity": reference(MEMBERS)},
        });
        let entities = self.0.entities(json!([
            entity(MEMBERS, json!({}), &[]),
            entity(ORG, organization, &[]),
            entity(ENG, org.clone(), &[ORG]),
            entity(OPS, org.clone(), &[ORG]),
            entity(KIM, json!({}), &[MEMBERS, ENG, OPS, RIVALS]),
            entity(LEE, json!({}), &[MEMBERS]),
            entity(BOT, org, &[MEMBERS, ORG, ENG]),
        ]))?;
        Ok(EntitySlice {
            entities,
            org: Some(OrgGrn::new(ACME)),
        })
    }
}

struct Held(OrgPolicies);

impl PolicySource for Held {
    async fn active(&self, _: &OrgGrn) -> Result<OrgPolicies, Cause> {
        Ok(self.0.clone())
    }
}

struct Unreadable;

impl PolicySource for Unreadable {
    async fn active(&self, _: &OrgGrn) -> Result<OrgPolicies, Cause> {
        Err("the store is down".into())
    }
}

fn link(id: &str, principal: Named, resource: Named) -> RoleLink {
    RoleLink {
        id: PolicyId::new(id),
        role: Role::Member,
        scope: if resource.0 == "Org" {
            RoleScope::Organization
        } else {
            RoleScope::TeamOrProject
        },
        principal: uid(principal),
        resource: uid(resource),
    }
}

fn links(links: Vec<RoleLink>) -> Held {
    Held(OrgPolicies {
        links,
        guardrails: vec![],
    })
}

fn guardrail(id: &str, source: &str) -> Policy {
    Policy::parse(Some(PolicyId::new(id)), source).unwrap()
}

fn ids(entries: &[Entry]) -> Vec<String> {
    entries
        .iter()
        .map(|entry| entry.policy.to_string())
        .collect()
}

async fn subject_report(policies: &Held, subject: Named) -> Vec<Entry> {
    let directory = Directory(Engine::load().unwrap());
    for_subject(policies, &directory, &OrgGrn::new(ACME), &uid(subject))
        .await
        .unwrap()
}

#[tokio::test]
async fn a_subjects_report_lists_links_naming_it_and_its_teams() {
    let policies = links(vec![
        link("direct", KIM, ORG),
        link("through-eng", ENG, WEB),
        link("lees", LEE, ORG),
    ]);

    let entries = subject_report(&policies, KIM).await;
    assert_eq!(ids(&entries), ["direct", "through-eng"]);
    assert_eq!(entries[1].named, [uid(ENG)]);

    assert_eq!(ids(&subject_report(&policies, LEE).await), ["lees"]);
}

#[tokio::test]
async fn a_subjects_report_leaves_out_a_link_held_on_its_team() {
    let policies = links(vec![link("on-eng", LEE, ENG)]);

    let entries = subject_report(&policies, KIM).await;
    assert!(entries.is_empty(), "{entries:?}");

    assert_eq!(ids(&subject_report(&policies, LEE).await), ["on-eng"]);
}

#[tokio::test]
async fn a_resources_report_lists_what_reaches_it_and_no_scoped_role_above_it() {
    let policies = links(vec![
        link("on-org", LEE, ORG),
        link("on-eng", LEE, ENG),
        link("on-ops", LEE, OPS),
    ]);
    let directory = Directory(Engine::load().unwrap());

    let entries = for_resource(&policies, &directory, &OrgGrn::new(ACME), &uid(BOT))
        .await
        .unwrap();
    assert_eq!(ids(&entries), ["on-org"]);

    let entries = for_resource(&policies, &directory, &OrgGrn::new(ACME), &uid(ENG))
        .await
        .unwrap();
    assert_eq!(ids(&entries), ["on-eng", "on-org"]);
}

#[tokio::test]
async fn a_report_reads_no_membership_of_another_organization() {
    let policies = links(vec![link("probe", RIVALS, WEB), link("control", ENG, WEB)]);

    let entries = subject_report(&policies, KIM).await;

    assert_eq!(ids(&entries), ["control"]);
}

#[tokio::test]
async fn a_guardrail_scoped_past_what_is_asked_is_left_out() {
    let policies = Held(OrgPolicies {
        links: vec![],
        guardrails: vec![
            guardrail("eng-alone", ON_ENG_ALONE),
            guardrail("agents", ON_AGENTS),
        ],
    });

    let entries = subject_report(&policies, KIM).await;
    assert!(entries.is_empty(), "{entries:?}");

    assert_eq!(ids(&subject_report(&policies, ENG).await), ["eng-alone"]);
    assert_eq!(ids(&subject_report(&policies, BOT).await), ["agents"]);
}

#[tokio::test]
async fn a_guardrail_is_listed_where_it_names_or_reaches_what_is_asked() {
    let policies = Held(OrgPolicies {
        links: vec![],
        guardrails: vec![
            guardrail("on-eng", ON_ENG),
            guardrail("on-ops", ON_OPS_IN_CONDITION),
            guardrail("everyone", EVERYONE),
            guardrail("on-lee", ON_LEE),
        ],
    });

    let entries = subject_report(&policies, KIM).await;
    assert_eq!(ids(&entries), ["everyone", "on-eng", "on-ops"]);
    assert_eq!(entries[0].named, []);
    assert_eq!(entries[1].named, [uid(ENG)]);
    assert_eq!(entries[2].named, [uid(OPS)]);
}

#[tokio::test]
async fn a_store_that_cannot_be_read_fails_the_report() {
    let directory = Directory(Engine::load().unwrap());

    let failed = for_subject(&Unreadable, &directory, &OrgGrn::new(ACME), &uid(KIM)).await;
    assert!(matches!(failed, Err(Error::PolicyRead(_))), "{failed:?}");
}
