//! Two organizations and the entities the system-policy tests decide
//! on. GRNs take lib/grn's form.

#![allow(dead_code)]

use std::str::FromStr;

use cedar_policy::{Entities, EntityId, EntityTypeName, EntityUid, Policy, PolicyId};
use decision::{Decision, Engine, EntityRef, Evaluation, Policies, Role, RoleLink, RoleScope};
use serde_json::{Value, json};

pub type Named = (&'static str, &'static str);

pub const ACME: Named = ("Iam::Org", "grn:root/iam/org/acme");
pub const GLOBEX: Named = ("Iam::Org", "grn:root/iam/org/globex");
pub const ACME_MEMBERS: Named = ("Iam::OrgMembers", ACME.1);
pub const GLOBEX_MEMBERS: Named = ("Iam::OrgMembers", GLOBEX.1);
pub const ENG: Named = ("Iam::Team", "grn:acme/iam/group/eng");
pub const WEB: Named = ("Iam::Project", "grn:acme/iam/project/web");
pub const API: Named = ("Iam::Project", "grn:acme/iam/project/api");
/// Belongs to acme and to globex.
pub const KIM: Named = ("Iam::User", "grn:acme/iam/user/kim");
/// Belongs to acme alone.
pub const LEE: Named = ("Iam::User", "grn:acme/iam/user/lee");
/// Belongs to globex alone.
pub const MIA: Named = ("Iam::User", "grn:globex/iam/user/mia");
/// In acme's eng team and in no organization.
pub const OUTSIDER: Named = ("Iam::User", "grn:acme/iam/user/outsider");
/// In acme's web project and in no organization.
pub const STRANGER: Named = ("Iam::User", "grn:acme/iam/user/stranger");
/// Acme's agent, in the eng team.
pub const BOT: Named = ("Iam::Agent", "grn:acme/iam/agent/bot");
pub const PEER: Named = ("Iam::Agent", "grn:acme/iam/agent/peer");
pub const RIVAL: Named = ("Iam::Agent", "grn:globex/iam/agent/rival");

/// Acme's hosted operator, holding `agent.activate` and
/// `agent.configure`.
pub const OPERATOR: Named = ("Iam::Operator", "grn:acme/iam/operator/hosting");
/// Globex's hosted operator, holding the same two.
pub const GLOBEX_OPERATOR: Named = ("Iam::Operator", "grn:globex/iam/operator/hosting");
/// Acme's operator with no delegation: the store holds no `operations`.
pub const IDLE_OPERATOR: Named = ("Iam::Operator", "grn:acme/iam/operator/idle");
/// Acme's operator holding `agent.configure` alone.
pub const CONFIGURE_ONLY: Named = ("Iam::Operator", "grn:acme/iam/operator/configure");
/// What `OPERATOR` and `GLOBEX_OPERATOR` hold.
pub const HELD: [&str; 2] = ["iam:agent.activate", "iam:agent.configure"];

pub struct World {
    pub engine: Engine,
    pub entities: Entities,
}

impl World {
    pub fn new() -> Self {
        let engine = Engine::load().expect("the system tier loads");
        let entities = engine
            .entities(directory())
            .expect("the directory conforms");
        Self { engine, entities }
    }

    /// The directory where acme has consented to `acme` and globex to
    /// `globex`: each an organization's half of ADR-0147's consent.
    pub fn consenting(acme: &[Named], globex: &[Named]) -> Self {
        let engine = Engine::load().expect("the system tier loads");
        let entities = engine
            .entities(directory_with(acme, globex))
            .expect("the directory conforms");
        Self { engine, entities }
    }

    pub fn compose(&self, links: &[RoleLink], guardrails: &[Policy]) -> Policies {
        self.engine
            .compose(links, guardrails)
            .expect("the policies compose")
    }

    pub fn ask(&self, policies: &Policies, who: Named, action: &str, what: Named) -> Decision {
        let request = self
            .engine
            .request(&evaluation(who, action, what))
            .expect("the question fits the schema");
        self.engine.decide(&request, policies, &self.entities)
    }
}

pub fn evaluation(who: Named, action: &str, what: Named) -> Evaluation<'_> {
    Evaluation {
        subject: EntityRef {
            entity_type: who.0,
            id: who.1,
        },
        action,
        resource: EntityRef {
            entity_type: what.0,
            id: what.1,
        },
        context: json!({}),
    }
}

pub fn uid(named: Named) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(named.0).expect("a type name"),
        EntityId::new(named.1),
    )
}

pub fn link(role: Role, scope: RoleScope, principal: Named, resource: Named) -> RoleLink {
    RoleLink {
        id: PolicyId::new(format!("link-{}-{}", principal.1, resource.1)),
        role,
        scope,
        principal: uid(principal),
        resource: uid(resource),
    }
}

pub fn policy(id: &str, source: &str) -> Policy {
    Policy::parse(Some(PolicyId::new(id)), source).expect("the policy parses")
}

fn parent(named: Named) -> Value {
    json!({"type": named.0, "id": named.1})
}

fn org(named: Named) -> Value {
    json!({"org": {"__entity": parent(named)}})
}

pub fn organization(named: Named, members: Named) -> Value {
    consenting(named, members, None)
}

/// An organization that states its consents, or none at all when
/// `consents` is `None`: the attribute is optional.
fn consenting(named: Named, members: Named, consents: Option<&[Named]>) -> Value {
    let mut attrs = json!({
        "org": {"__entity": parent(named)},
        "members": {"__entity": parent(members)},
    });
    if let Some(consents) = consents {
        let each: Vec<Value> = consents
            .iter()
            .map(|org| json!({"__entity": parent(*org)}))
            .collect();
        attrs["consents"] = Value::Array(each);
    }
    json!({"uid": parent(named), "attrs": attrs, "parents": []})
}

fn operator(named: Named, org: Named, members: Named, operations: Option<&[&str]>) -> Value {
    let mut attrs = json!({"org": {"__entity": parent(org)}});
    if let Some(operations) = operations {
        attrs["operations"] = json!(operations);
    }
    entity(named, attrs, &[members, org])
}

fn entity(named: Named, attrs: Value, parents: &[Named]) -> Value {
    let parents: Vec<Value> = parents.iter().copied().map(parent).collect();
    json!({"uid": parent(named), "attrs": attrs, "parents": parents})
}

pub fn directory() -> Value {
    directory_with_orgs(
        organization(ACME, ACME_MEMBERS),
        organization(GLOBEX, GLOBEX_MEMBERS),
    )
}

fn directory_with(acme: &[Named], globex: &[Named]) -> Value {
    directory_with_orgs(
        consenting(ACME, ACME_MEMBERS, Some(acme)),
        consenting(GLOBEX, GLOBEX_MEMBERS, Some(globex)),
    )
}

fn directory_with_orgs(acme: Value, globex: Value) -> Value {
    json!([
        entity(ACME_MEMBERS, json!({}), &[]),
        entity(GLOBEX_MEMBERS, json!({}), &[]),
        acme,
        globex,
        operator(OPERATOR, ACME, ACME_MEMBERS, Some(&HELD)),
        operator(GLOBEX_OPERATOR, GLOBEX, GLOBEX_MEMBERS, Some(&HELD)),
        operator(IDLE_OPERATOR, ACME, ACME_MEMBERS, None),
        operator(
            CONFIGURE_ONLY,
            ACME,
            ACME_MEMBERS,
            Some(&["iam:agent.configure"]),
        ),
        entity(ENG, org(ACME), &[ACME]),
        entity(WEB, org(ACME), &[ACME]),
        entity(API, org(ACME), &[ACME]),
        entity(KIM, json!({}), &[ACME_MEMBERS, GLOBEX_MEMBERS, ENG, WEB]),
        entity(LEE, json!({}), &[ACME_MEMBERS]),
        entity(MIA, json!({}), &[GLOBEX_MEMBERS]),
        entity(OUTSIDER, json!({}), &[ENG]),
        entity(STRANGER, json!({}), &[WEB]),
        entity(BOT, org(ACME), &[ACME_MEMBERS, ACME, ENG]),
        entity(PEER, org(ACME), &[ACME_MEMBERS, ACME]),
        entity(RIVAL, org(GLOBEX), &[GLOBEX_MEMBERS, GLOBEX]),
    ])
}
