//! The organization every grant test shares: who holds which role,
//! and the in-memory stores a decision reads.

#![allow(dead_code)]

use std::str::FromStr;

use cedar_policy::{Entities, EntityId, EntityTypeName, EntityUid, PolicyId};
use decision::{
    Cause, Engine, EntityRef, EntitySlice, EntitySource, OrgGrn, OrgPolicies, PolicySource, Role,
    RoleLink, RoleScope,
};
use grant::{Error, PermitRight};
use serde_json::{Value, json};

pub type Named = (&'static str, &'static str);

pub const ACME: &str = "grn:root/iam/org/acme";
pub const GLOBEX: &str = "grn:root/iam/org/globex";
pub const ORG: Named = ("Iam::Org", ACME);
pub const MEMBERS: Named = ("Iam::OrgMembers", ACME);
pub const OWNER: Named = ("Iam::User", "grn:acme/iam/user/lee");
pub const ADMIN: Named = ("Iam::User", "grn:acme/iam/user/kim");
pub const MEMBER: Named = ("Iam::User", "grn:acme/iam/user/pat");
pub const STRANGER: Named = ("Iam::User", "grn:globex/iam/user/mia");
pub const BOT: Named = ("Iam::Agent", "grn:acme/iam/agent/bot");
pub const ENG: Named = ("Iam::Team", "grn:acme/iam/group/eng");
pub const GLOBEX_ORG: Named = ("Iam::Org", GLOBEX);
pub const GLOBEX_MEMBERS: Named = ("Iam::OrgMembers", GLOBEX);

pub const WATCH_BOT: &str = r#"permit (
    principal,
    action == Iam::Action::"agent.watch",
    resource == Iam::Agent::"grn:acme/iam/agent/bot"
);"#;

pub fn uid(named: Named) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(named.0).unwrap(),
        EntityId::new(named.1),
    )
}

pub fn reference(named: Named) -> Value {
    json!({"type": named.0, "id": named.1})
}

pub fn entity(named: Named, attrs: Value, parents: &[Named]) -> Value {
    let parents: Vec<Value> = parents.iter().copied().map(reference).collect();
    json!({"uid": reference(named), "attrs": attrs, "parents": parents})
}

pub fn organization(named: Named, members: Named) -> Value {
    let attrs = json!({
        "org": {"__entity": reference(named)},
        "members": {"__entity": reference(members)},
    });
    entity(named, attrs, &[])
}

/// Lee holds `role-owner` and kim `role-admin` in acme, pat is a
/// member with no role, and mia belongs to globex alone.
pub struct World {
    pub engine: Engine,
    pub entities: Entities,
    pub held: OrgPolicies,
}

pub fn link(role: Role, principal: Named) -> RoleLink {
    RoleLink {
        id: PolicyId::new(format!("grn:acme/iam/policy/{}", principal.1)),
        role,
        scope: RoleScope::Organization,
        principal: uid(principal),
        resource: uid(ORG),
    }
}

impl World {
    pub fn new() -> Self {
        let engine = Engine::load().unwrap();
        let org = json!({"org": {"__entity": reference(ORG)}});
        let entities = engine
            .entities(json!([
                entity(MEMBERS, json!({}), &[]),
                entity(GLOBEX_MEMBERS, json!({}), &[]),
                organization(ORG, MEMBERS),
                organization(GLOBEX_ORG, GLOBEX_MEMBERS),
                entity(ENG, org.clone(), &[ORG]),
                entity(BOT, org, &[MEMBERS, ORG]),
                entity(OWNER, json!({}), &[MEMBERS]),
                entity(ADMIN, json!({}), &[MEMBERS]),
                entity(MEMBER, json!({}), &[MEMBERS]),
                entity(STRANGER, json!({}), &[GLOBEX_MEMBERS]),
            ]))
            .unwrap();
        let held = OrgPolicies {
            links: vec![link(Role::Owner, OWNER), link(Role::Admin, ADMIN)],
            ..OrgPolicies::default()
        };
        Self {
            engine,
            entities,
            held,
        }
    }

    pub async fn right(&self, writer: Named) -> Result<PermitRight, Error> {
        let writer = EntityRef {
            entity_type: writer.0,
            id: writer.1,
        };
        PermitRight::ask(&self.engine, self, self, &OrgGrn::new(ACME), writer).await
    }

    pub async fn owner_right(&self) -> PermitRight {
        self.right(OWNER).await.unwrap()
    }
}

impl PolicySource for World {
    async fn active(&self, org: &OrgGrn) -> Result<OrgPolicies, Cause> {
        if org.as_str() == ACME {
            return Ok(self.held.clone());
        }
        Ok(OrgPolicies::default())
    }
}

impl EntitySource for World {
    async fn slice(&self, _: &EntityUid, resource: &EntityUid) -> Result<EntitySlice, Cause> {
        let org = if resource.id().unescaped().contains("globex") {
            GLOBEX
        } else {
            ACME
        };
        Ok(EntitySlice {
            entities: self.entities.clone(),
            org: Some(OrgGrn::new(org)),
        })
    }
}
