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

	// Reading the operators an org has registered, and registering one
	// (ADR-0045). The org owns the constants for the reason it owns the
	// policy ones, and internal/operator does not import this package.
	ActionOperatorRead = "operator:read"
	// ActionOperatorRegister is recording that the org has an operator.
	// It mints no certificate: what the record admits, it admits for a
	// certificate the org's authority signed and this action does not
	// sign (ADR-0070).
	ActionOperatorRegister = "operator:register"
	// ActionOperatorDelete is withdrawing that record, which stops the
	// operator authenticating (ADR-0070). It is an action of its own
	// rather than the registering one, because it is the first thing a
	// member does here that takes a machine identity away: registering
	// grants nothing and this withdraws what an operator runs on.
	ActionOperatorDelete = "operator:delete"
	// ActionOperatorUpdate is changing what the org says about an
	// operator: its name and description (ADR-0074). It is an action of
	// its own because it neither grants what registering does nor
	// withdraws what deleting does.
	ActionOperatorUpdate = "operator:update"

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
	// grants it: a policy carries it or nothing does.
	ActionAgentWatch = "agent:watch"
)

// Agent and operator actions are granted one at a time and never as
// `agent:*` or `operator:*`: ADR-0012 puts provisioning outside garam,
// so a wildcard would grant in advance the actions no endpoint may
// offer. Deleting an operator is what that caution was for — it
// arrived after registering and reading, and a wildcard would have
// granted it before anyone decided it (ADR-0070).
var (
	// adminMachineActions is what an owner or an admin holds.
	adminMachineActions = []string{
		ActionOperatorRead, ActionOperatorRegister, ActionOperatorUpdate,
		ActionOperatorDelete, ActionAgentRead, ActionAgentDefine,
	}
	// memberMachineActions is what a member holds: it reads the
	// operators an org has registered and registers none, because
	// standing a machine identity up for the organization is the
	// administrative act registering already looks like.
	memberMachineActions = []string{
		ActionOperatorRead, ActionAgentRead, ActionAgentDefine,
	}
)

// Actions returns the action patterns the role grants on an org and
// on the groups, projects, operators and agents it contains. The
// patterns feed IAM-style Allow statements; the evaluator resolves
// wildcards.
func (r Role) Actions() []string {
	switch r {
	case RoleOwner:
		return slices.Concat([]string{
			"org:*", "group:*", "project:*", "policy:*", "consent:*",
		}, adminMachineActions)
	case RoleAdmin:
		return slices.Concat([]string{
			ActionOrgRead, ActionOrgUpdate, ActionOrgMemberList, ActionOrgInvite,
			"group:*", "project:*", "policy:*", "consent:*",
		}, adminMachineActions)
	case RoleMember:
		return slices.Concat([]string{
			ActionOrgRead, ActionOrgMemberList,
			ActionGroupRead, ActionGroupMemberList,
			ActionProjectRead, ActionProjectMemberList,
		}, memberMachineActions)
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
