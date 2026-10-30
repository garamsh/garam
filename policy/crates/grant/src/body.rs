use std::str::FromStr;

use cedar_policy::{Effect, EntityId, EntityTypeName, EntityUid, Policy, PolicyId};
use decision::{Engine, OrgGrn, Role, RoleLink, RoleScope};
use serde::{Deserialize, Serialize};

use crate::error::Error;

/// The longest guardrail. Cedar recurses over `+`, `.` and method
/// chains, which hold no bracket: 4,375 terms overflow 1 MiB unoptimised
/// and this admits 2,048, the stack being what a request runs on.
const MAX_GUARDRAIL_BYTES: usize = 4 * 1024;
/// The deepest brackets and `if`s in a guardrail. 18 brackets overflow
/// the same 1 MiB and 186 `if`s do, and one level is the `when` block.
const MAX_GUARDRAIL_NESTING: usize = 12;

/// What one version of a policy says. Only [`Body::role_link`] and
/// [`Body::guardrail`] make one, and it neither serializes nor
/// deserializes outside this crate, so the store is given nothing the
/// schema has not admitted.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Body(pub(crate) Stored);

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub(crate) enum Stored {
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
pub(crate) enum StoredRole {
    Owner,
    Admin,
    Member,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub(crate) enum StoredScope {
    Organization,
    TeamOrProject,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub(crate) struct StoredUid {
    r#type: String,
    id: String,
}

impl Body {
    /// A role assignment of `org`, refused where it names another
    /// organization's entity, where its principal holds no role, where
    /// its resource is outside its template set, or where the schema
    /// does not admit it.
    pub fn role_link(
        engine: &Engine,
        org: &OrgGrn,
        role: Role,
        scope: RoleScope,
        principal: EntityUid,
        resource: EntityUid,
    ) -> Result<Self, Error> {
        for entity in [&principal, &resource] {
            if !org.owns(entity) {
                return Err(Error::ForeignEntity(entity.to_string()));
            }
        }
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

    /// A guardrail of `org`, refused unless it is one `forbid` the
    /// schema admits, naming no other organization's entity.
    pub fn guardrail(engine: &Engine, org: &OrgGrn, source: &str) -> Result<Self, Error> {
        let policy = parse_guardrail(PolicyId::new("candidate"), source)?;
        for entity in policy.entity_literals() {
            if entity.type_name().to_string() != "Action" && !org.owns(&entity) {
                return Err(Error::ForeignEntity(entity.to_string()));
            }
        }
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

/// Parses a guardrail on the write path and on every read of one.
fn parse_guardrail(id: PolicyId, source: &str) -> Result<Policy, Error> {
    if source.len() > MAX_GUARDRAIL_BYTES || nesting(source) > MAX_GUARDRAIL_NESTING {
        return Err(Error::GuardrailTooDeep);
    }
    let policy =
        Policy::parse(Some(id), source).map_err(|error| Error::GuardrailSyntax(error.into()))?;
    match policy.effect() {
        Effect::Forbid => Ok(policy),
        Effect::Permit => Err(Error::GuardrailPermits),
    }
}

/// The deepest nesting in the text, counting a bracket inside a string
/// too: refusing an odd literal costs less than a second lexer. An `if`
/// has no closing bracket, so each one counts for the rest of the text.
fn nesting(source: &str) -> usize {
    let (mut depth, mut deepest, mut ifs) = (0_usize, 0_usize, 0_usize);
    for (index, byte) in source.bytes().enumerate() {
        match byte {
            b'(' | b'[' | b'{' => depth += 1,
            b')' | b']' | b'}' => depth = depth.saturating_sub(1),
            b'i' if source.as_bytes()[index..].starts_with(b"if") => ifs += 1,
            _ => {}
        }
        deepest = deepest.max(depth + ifs);
    }
    deepest
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
