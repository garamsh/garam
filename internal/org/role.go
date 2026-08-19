package org

import (
	"slices"

	"github.com/garamsh/garam/internal/errorx"
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

	// Policy management actions. The org owns them because policies
	// are org-managed data and the api server may not import the
	// engine that stores them (ADR-0003).
	ActionPolicyCreate = "policy:create"
	ActionPolicyRead   = "policy:read"
	ActionPolicyUpdate = "policy:update"
	ActionPolicyAttach = "policy:attach"
	ActionPolicyDetach = "policy:detach"

	// Cross-organization consent management (ADR-0035). The org owns
	// the constants for the reason it owns the policy ones: the
	// relation is org-managed data the api server enforces over,
	// stored by internal/message.
	ActionConsentRead     = "consent:read"
	ActionConsentGrant    = "consent:grant"
	ActionConsentWithdraw = "consent:withdraw"

	// Reading the agents an org holds, defining one, and watching one
	// work. The org owns the constants for the reason it owns the
	// policy ones: the api
	// server enforces them, and internal/agent does not import this
	// package — it keeps its own copy of the org service type rather
	// than depend on it.
	ActionAgentRead = "agent:read"
	// ActionAgentDefine is saying what an agent should be (ADR-0026).
	// It is not provisioning one: what an operator made is reported,
	// and no action here grants it.
	ActionAgentDefine = "agent:define"
	// ActionAgentWatch is watching an agent work (ADR-0022). Reading an
	// agent reports that it exists and what two parties say about its
	// state; watching exposes the content of its work, which an
	// organization may withhold from someone it lets read. No role
	// grants it — see Actions.
	ActionAgentWatch = "agent:watch"
)

// Actions returns the action patterns the role grants on an org and
// on the groups, projects and agents it contains. The patterns feed
// IAM-style Allow statements; the evaluator resolves wildcards.
//
// Agents are named action by action rather than as `agent:*`, every
// role over: ADR-0012 puts provisioning outside garam, so a wildcard
// here would grant in advance the very actions no endpoint may offer.
//
// No role names ActionAgentWatch. ADR-0022 separates watching from
// reading so an organization may give one and withhold the other, and
// a grant arriving with membership is a grant nobody decided — which
// is the separation being invisible wherever the roles are the whole
// of the authorization. Watching is carried by a policy or not at all.
func (r Role) Actions() []string {
	switch r {
	case RoleOwner:
		return []string{"org:*", "group:*", "project:*", "policy:*", "consent:*", ActionAgentRead, ActionAgentDefine}
	case RoleAdmin:
		return []string{
			ActionOrgRead, ActionOrgUpdate, ActionOrgMemberList, ActionOrgInvite,
			"group:*", "project:*", "policy:*", "consent:*", ActionAgentRead, ActionAgentDefine,
		}
	case RoleMember:
		return []string{
			ActionOrgRead, ActionOrgMemberList,
			ActionGroupRead, ActionGroupMemberList,
			ActionProjectRead, ActionProjectMemberList,
			ActionAgentRead, ActionAgentDefine,
		}
	}
	return nil
}

// ActionsFor returns the union of the action patterns granted by the
// named roles, in the order the names appear. Groups and Projects
// carry role names as data, so an unknown name contributes nothing
// rather than failing the check.
func ActionsFor(names []string) []string {
	var actions []string
	for _, n := range names {
		r, err := ParseRole(n)
		if err != nil {
			continue
		}
		for _, a := range r.Actions() {
			if !slices.Contains(actions, a) {
				actions = append(actions, a)
			}
		}
	}
	return actions
}
