package org

import (
	"github.com/garamsh/gaon/internal/errorx"
)

// Role is the org-level permission carried by a Membership. Roles are
// ordered: owner > admin > member.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// ParseRole parses a Role from its string form.
func ParseRole(s string) (Role, error) {
	switch r := Role(s); r {
	case RoleOwner, RoleAdmin, RoleMember:
		return r, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "org: unknown role")
}

// Org-scoped action constants. Domains own their actions (ADR-0003);
// the authz engine matches the strings only.
const (
	ActionOrgRead       = "org:read"
	ActionOrgUpdate     = "org:update"
	ActionOrgDelete     = "org:delete"
	ActionOrgMemberList = "org:member-list"
	ActionOrgInvite     = "org:invite"

	ActionGroupCreate       = "group:create"
	ActionGroupRead         = "group:read"
	ActionGroupUpdate       = "group:update"
	ActionGroupDelete       = "group:delete"
	ActionGroupMemberList   = "group:member-list"
	ActionGroupMemberAdd    = "group:member-add"
	ActionGroupMemberRemove = "group:member-remove"

	ActionProjectCreate       = "project:create"
	ActionProjectRead         = "project:read"
	ActionProjectUpdate       = "project:update"
	ActionProjectDelete       = "project:delete"
	ActionProjectMemberList   = "project:member-list"
	ActionProjectMemberAdd    = "project:member-add"
	ActionProjectMemberRemove = "project:member-remove"
)

// Actions returns the action patterns the role grants on an org and
// on the groups and projects it contains. The patterns feed IAM-style
// Allow statements; the evaluator resolves wildcards.
func (r Role) Actions() []string {
	switch r {
	case RoleOwner:
		return []string{"org:*", "group:*", "project:*"}
	case RoleAdmin:
		return []string{
			ActionOrgRead, ActionOrgUpdate, ActionOrgMemberList, ActionOrgInvite,
			"group:*", "project:*",
		}
	case RoleMember:
		return []string{
			ActionOrgRead, ActionOrgMemberList,
			ActionGroupRead, ActionGroupMemberList,
			ActionProjectRead, ActionProjectMemberList,
		}
	}
	return nil
}
