use std::str::FromStr;

use cedar_policy::{Effect, EntityId, EntityTypeName, EntityUid, Policy, PolicyId};
use decision::{Engine, Role, RoleLink, RoleScope};
use serde::{Deserialize, Serialize};

use crate::error::Error;

/// What one version of a policy says. Only [`Body::role_link`] and
/// [`Body::guardrail`] make one, so the store is given nothing the
/// schema has not admitted.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Body(Stored);

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
enum Stored {
    RoleLink {
        role: StoredRole,
        scope: StoredScope,
        principal: StoredUid,
        resource: StoredUid,
    },
    Guardrail {
        source: String,
    },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
enum StoredRole {
    Owner,
    Admin,
    Member,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
enum StoredScope {
    Organization,
    TeamOrProject,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
struct StoredUid {
    r#type: String,
    id: String,
}

impl Body {
    /// A role assignment, refused where its principal holds no role,
    /// where its resource is outside its template set, or where the
    /// schema does not admit it.
    pub fn role_link(
        engine: &Engine,
        role: Role,
        scope: RoleScope,
        principal: EntityUid,
        resource: EntityUid,
    ) -> Result<Self, Error> {
        let link = RoleLink {
            id: PolicyId::new("candidate"),
            role,
            scope,
            principal,
            resource,
        };
        engine
            .validate(std::slice::from_ref(&link), &[])
            .map_err(refused)?;
        Ok(Self(Stored::RoleLink {
            role: match role {
                Role::Owner => StoredRole::Owner,
                Role::Admin => StoredRole::Admin,
                Role::Member => StoredRole::Member,
            },
            scope: match scope {
                RoleScope::Organization => StoredScope::Organization,
                RoleScope::TeamOrProject => StoredScope::TeamOrProject,
            },
            principal: stored_uid(&link.principal),
            resource: stored_uid(&link.resource),
        }))
    }

    /// A guardrail, refused unless it is one `forbid` the schema
    /// admits.
    pub fn guardrail(engine: &Engine, source: &str) -> Result<Self, Error> {
        let policy = parse_guardrail(PolicyId::new("candidate"), source)?;
        engine.validate(&[], &[policy]).map_err(refused)?;
        Ok(Self(Stored::Guardrail {
            source: source.to_owned(),
        }))
    }

    /// The Cedar form of a stored version, under the policy's GRN.
    pub(crate) fn into_policy(self, grn: &str) -> Result<StoredPolicy, Error> {
        let id = PolicyId::new(grn);
        match self.0 {
            Stored::RoleLink {
                role,
                scope,
                principal,
                resource,
            } => {
                let uid = |stored: StoredUid| {
                    entity_uid(&stored).map_err(|source| Error::StoredBody {
                        policy: grn.to_owned(),
                        source,
                    })
                };
                Ok(StoredPolicy::Link(RoleLink {
                    id,
                    role: match role {
                        StoredRole::Owner => Role::Owner,
                        StoredRole::Admin => Role::Admin,
                        StoredRole::Member => Role::Member,
                    },
                    scope: match scope {
                        StoredScope::Organization => RoleScope::Organization,
                        StoredScope::TeamOrProject => RoleScope::TeamOrProject,
                    },
                    principal: uid(principal)?,
                    resource: uid(resource)?,
                }))
            }
            Stored::Guardrail { source } => {
                Ok(StoredPolicy::Guardrail(parse_guardrail(id, &source)?))
            }
        }
    }
}

/// A stored version as a decision composes it.
pub(crate) enum StoredPolicy {
    Link(RoleLink),
    Guardrail(Policy),
}

fn parse_guardrail(id: PolicyId, source: &str) -> Result<Policy, Error> {
    let policy =
        Policy::parse(Some(id), source).map_err(|error| Error::GuardrailSyntax(error.into()))?;
    match policy.effect() {
        Effect::Forbid => Ok(policy),
        Effect::Permit => Err(Error::GuardrailPermits),
    }
}

fn refused(error: decision::Error) -> Error {
    Error::Refused(error)
}

fn stored_uid(uid: &EntityUid) -> StoredUid {
    StoredUid {
        r#type: uid.type_name().to_string(),
        id: uid.id().unescaped().to_owned(),
    }
}

fn entity_uid(stored: &StoredUid) -> Result<EntityUid, Box<dyn std::error::Error + Send + Sync>> {
    Ok(EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(&stored.r#type)?,
        EntityId::new(&stored.id),
    ))
}
