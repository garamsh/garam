use std::str::FromStr;

use cedar_policy::{
    Effect, EntityId, EntityTypeName, EntityUid, Policy, PolicyId, ResourceConstraint,
};
use decision::{Engine, OrgGrn, Role, RoleLink, RoleScope};
use serde::{Deserialize, Serialize};

use crate::error::Error;
use crate::permit_right::PermitRight;

/// The longest guardrail or permit. Cedar recurses over `+`, `.` and method
/// chains, which hold no bracket: 4,375 terms overflow 1 MiB unoptimised
/// and this admits 2,048, the stack being what a request runs on.
const MAX_GUARDRAIL_BYTES: usize = 4 * 1024;
/// The deepest brackets and `if`s in a guardrail or permit. 18 brackets overflow
/// the same 1 MiB and 186 `if`s do, and one level is the `when` block.
const MAX_GUARDRAIL_NESTING: usize = 12;

/// What one version of a policy says. Only [`Body::role_link`],
/// [`Body::guardrail`] and [`Body::permit`] make one, and it neither
/// serializes nor deserializes outside this crate, so the store is
/// given nothing the schema has not admitted. [`Body::permit`] takes a
/// [`PermitRight`], which only an allowed evaluation of
/// `iam:permit.write` makes, so no permit exists that nobody asked for.
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
    Permit {
        source: String,
        /// The organization the right was given on.
        org: String,
        /// The writer the right was given to.
        writer: String,
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
        let policy = parse_own(Kind::Guardrail, PolicyId::new("candidate"), source)?;
        for entity in policy.entity_literals() {
            if entity.type_name().basename() != "Action" && !org.owns(&entity) {
                return Err(Error::ForeignEntity(entity.to_string()));
            }
        }
        engine.validate(&[], &[policy]).map_err(refused)?;
        Ok(Self(Stored::Guardrail {
            source: source.to_owned(),
        }))
    }

    /// A permit of the organization `right` was given on, refused
    /// unless it is one `permit` the schema admits, scoped by `resource
    /// ==`, `resource in` or `resource is T in` to an entity whose GRN
    /// names that organization or to its `Org` (ADR-0144 item 2). The
    /// principal scope is not read: tenant isolation requires a
    /// principal's membership of the resource's organization.
    pub fn permit(engine: &Engine, right: &PermitRight, source: &str) -> Result<Self, Error> {
        let policy = parse_own(Kind::Permit, PolicyId::new("candidate"), source)?;
        engine
            .validate(&[], std::slice::from_ref(&policy))
            .map_err(refused)?;
        check_resource_scope(right.org(), &policy)?;
        Ok(Self(Stored::Permit {
            source: source.to_owned(),
            org: right.org().as_str().to_owned(),
            writer: right.writer().to_owned(),
        }))
    }

    /// Refuses a permit stored under another organization or by
    /// another writer than the right it was made with.
    pub(crate) fn check_right(&self, org: &OrgGrn, writer: &str) -> Result<(), Error> {
        match &self.0 {
            Stored::Permit {
                org: given_on,
                writer: given_to,
                ..
            } if given_on != org.as_str() || given_to != writer => Err(Error::PermitRight),
            _ => Ok(()),
        }
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
            Stored::Guardrail { source } => Ok(StoredPolicy::Guardrail(parse_own(
                Kind::Guardrail,
                id,
                &source,
            )?)),
            Stored::Permit { source, .. } => {
                Ok(StoredPolicy::Permit(parse_own(Kind::Permit, id, &source)?))
            }
        }
    }
}

/// A stored version as a decision composes it.
pub(crate) enum StoredPolicy {
    Link(RoleLink),
    Guardrail(Policy),
    Permit(Policy),
}

/// What an organization's own policy is.
#[derive(Clone, Copy)]
enum Kind {
    Guardrail,
    Permit,
}

/// Parses a guardrail or a permit on the write path and on every read
/// of one.
fn parse_own(kind: Kind, id: PolicyId, source: &str) -> Result<Policy, Error> {
    if source.len() > MAX_GUARDRAIL_BYTES || nesting(source) > MAX_GUARDRAIL_NESTING {
        return Err(match kind {
            Kind::Guardrail => Error::GuardrailTooDeep,
            Kind::Permit => Error::PermitTooDeep,
        });
    }
    let policy = Policy::parse(Some(id), source).map_err(|error| match kind {
        Kind::Guardrail => Error::GuardrailSyntax(error.into()),
        Kind::Permit => Error::PermitSyntax(error.into()),
    })?;
    match (kind, policy.effect()) {
        (Kind::Guardrail, Effect::Forbid) | (Kind::Permit, Effect::Permit) => Ok(policy),
        (Kind::Guardrail, Effect::Permit) => Err(Error::GuardrailPermits),
        (Kind::Permit, Effect::Forbid) => Err(Error::PermitForbids),
    }
}

/// Reads a permit's resource scope as the engine selects policies: it
/// must name one entity of the organization, so that no resource of
/// another is in reach.
fn check_resource_scope(org: &OrgGrn, policy: &Policy) -> Result<(), Error> {
    match policy.resource_constraint() {
        ResourceConstraint::Any => Err(Error::PermitResourceUnscoped),
        ResourceConstraint::Is(entity_type) => {
            Err(Error::PermitResourceByType(entity_type.to_string()))
        }
        ResourceConstraint::Eq(entity)
        | ResourceConstraint::In(entity)
        | ResourceConstraint::IsIn(_, entity) => {
            if org.names(&entity) {
                Ok(())
            } else {
                Err(Error::ForeignEntity(entity.to_string()))
            }
        }
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
