//! Two organizations and the entities the system-policy tests decide
//! on. GRNs take lib/grn's form.

#![allow(dead_code)]

use std::str::FromStr;

use cedar_policy::{Entities, EntityId, EntityTypeName, EntityUid, Policy, PolicyId};
use decision::{Decision, Engine, EntityRef, Evaluation, Policies, Role, RoleLink, RoleScope};
use serde_json::{Value, json};

pub type Named = (&'static str, &'static str);

pub const ACME: Named = ("Org", "grn:root:default:org:acme");
pub const GLOBEX: Named = ("Org", "grn:root:default:org:globex");
pub const ACME_MEMBERS: Named = ("OrgMembers", ACME.1);
pub const GLOBEX_MEMBERS: Named = ("OrgMembers", GLOBEX.1);
pub const ENG: Named = ("Team", "grn:acme:default:group:eng");
pub const WEB: Named = ("Project", "grn:acme:default:project:web");
pub const API: Named = ("Project", "grn:acme:default:project:api");
/// Belongs to acme and to globex.
pub const KIM: Named = ("User", "grn:acme:default:user:kim");
/// Belongs to acme alone.
pub const LEE: Named = ("User", "grn:acme:default:user:lee");
/// Belongs to globex alone.
pub const MIA: Named = ("User", "grn:globex:default:user:mia");
/// In acme's eng team and in no organization.
pub const OUTSIDER: Named = ("User", "grn:acme:default:user:outsider");
/// In acme's web project and in no organization.
pub const STRANGER: Named = ("User", "grn:acme:default:user:stranger");
/// Acme's agent, in the eng team.
pub const BOT: Named = ("Agent", "grn:acme:default:agent:bot");
pub const PEER: Named = ("Agent", "grn:acme:default:agent:peer");
pub const RIVAL: Named = ("Agent", "grn:globex:default:agent:rival");

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
    json!({
        "uid": parent(named),
        "attrs": {
            "org": {"__entity": parent(named)},
            "members": {"__entity": parent(members)},
        },
        "parents": [],
    })
}

fn entity(named: Named, attrs: Value, parents: &[Named]) -> Value {
    let parents: Vec<Value> = parents.iter().copied().map(parent).collect();
    json!({"uid": parent(named), "attrs": attrs, "parents": parents})
}

pub fn directory() -> Value {
    json!([
        entity(ACME_MEMBERS, json!({}), &[]),
        entity(GLOBEX_MEMBERS, json!({}), &[]),
        organization(ACME, ACME_MEMBERS),
        organization(GLOBEX, GLOBEX_MEMBERS),
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
