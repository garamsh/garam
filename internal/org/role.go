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
)

// Actions returns the action patterns the role grants on an org. The
// patterns feed IAM-style Allow statements; the evaluator resolves
// wildcards.
func (r Role) Actions() []string {
	switch r {
	case RoleOwner:
		return []string{"org:*"}
	case RoleAdmin:
		return []string{ActionOrgRead, ActionOrgUpdate, ActionOrgMemberList, ActionOrgInvite}
	case RoleMember:
		return []string{ActionOrgRead, ActionOrgMemberList}
	}
	return nil
}
