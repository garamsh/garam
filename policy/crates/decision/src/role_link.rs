use std::collections::HashMap;

use cedar_policy::{EntityUid, PolicyId, SlotId};

use crate::error::Error;

/// A role a template grants.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Role {
    /// `role-owner`.
    Owner,
    /// `role-admin`.
    Admin,
    /// `role-member`.
    Member,
}

/// Which template set a role links through.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RoleScope {
    /// An organization and all it contains.
    Organization,
    /// One team or project and nothing inside it.
    TeamOrProject,
}

/// A role assignment: one template linked to one subject and one
/// resource, which makes one readable policy.
#[derive(Debug, Clone)]
pub struct RoleLink {
    /// The linked policy's id.
    pub id: PolicyId,
    /// The role granted.
    pub role: Role,
    /// The template set.
    pub scope: RoleScope,
    /// Who holds it: a user, an agent, or a team or project whose
    /// members hold it.
    pub principal: EntityUid,
    /// What it is held on.
    pub resource: EntityUid,
}

impl RoleLink {
    pub(crate) fn template_id(&self) -> PolicyId {
        PolicyId::new(match (self.scope, self.role) {
            (RoleScope::Organization, Role::Owner) => "role-org-owner",
            (RoleScope::Organization, Role::Admin) => "role-org-admin",
            (RoleScope::Organization, Role::Member) => "role-org-member",
            (RoleScope::TeamOrProject, Role::Owner) => "role-scoped-owner",
            (RoleScope::TeamOrProject, Role::Admin) => "role-scoped-admin",
            (RoleScope::TeamOrProject, Role::Member) => "role-scoped-member",
        })
    }

    pub(crate) fn slots(&self) -> HashMap<SlotId, EntityUid> {
        HashMap::from([
            (SlotId::principal(), self.principal.clone()),
            (SlotId::resource(), self.resource.clone()),
        ])
    }

    /// Refuses a link its templates would misread: an organization
    /// template on a team would reach the team's member agents, and an
    /// organization as principal would grant through containment what
    /// only `OrgMembers` may state (ADR-0137).
    pub(crate) fn check_scope(&self) -> Result<(), Error> {
        let principal = self.principal.type_name().to_string();
        if !matches!(principal.as_str(), "User" | "Agent" | "Team" | "Project") {
            return Err(Error::RoleLinkPrincipal {
                link: self.id.to_string(),
                principal,
            });
        }
        let resource = self.resource.type_name().to_string();
        let (scope, fits) = match self.scope {
            RoleScope::Organization => ("organization", resource == "Org"),
            RoleScope::TeamOrProject => (
                "team or project",
                resource == "Team" || resource == "Project",
            ),
        };
        if fits {
            return Ok(());
        }
        Err(Error::RoleLinkScope {
            link: self.id.to_string(),
            scope,
            resource,
        })
    }
}
