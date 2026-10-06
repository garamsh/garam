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
	// ActionOperatorDelegate is granting a hosted operator bounded
	// authority over the org's controllers, and withdrawing it
	// (ADR-0078). It is the administrative act of handing execution to
	// a service, which registering an operator alone does not do.
	ActionOperatorDelegate = "operator:delegate"

	// Reading the agents an org holds and watching one work. The org
	// owns the constants for the reason it owns the policy ones: the
	// api server enforces them, and internal/agent does not import
	// this package — it keeps its own copy of the org service type
	// rather than depend on it.
	ActionAgentRead = "agent:read"
	// ActionAgentWatch is watching an agent work (ADR-0022). Reading an
	// agent reports that it exists and what two parties say about its
	// state; watching exposes the content of its work, which an
	// organization may withhold from someone it lets read. No role
	// grants it: a policy carries it or nothing does.
	ActionAgentWatch = "agent:watch"
	// ActionAgentRename is changing what the org calls an agent it
	// defined (ADR-0075). It moves no value an operator reads. No
	// action defines an agent: garam accepts no desired value
	// (ADR-0090).
	ActionAgentRename = "agent:rename"
	// ActionAgentCreate is asking a delegated operator to create an agent
	// from an immutable template or profile it already allows
	// (ADR-0078). It chooses no execution setting.
	ActionAgentCreate = "agent:create"
	// ActionAgentConfigure is asking a delegated operator to change an
	// agent's execution settings or profile: what runs on a host, which
	// membership alone does not grant (ADR-0078).
	ActionAgentConfigure = "agent:configure"
	// ActionAgentRecover is asking a delegated operator to replace an
	// agent's credential lineage, which fences every certificate it
	// held before (ADR-0078). Membership alone does not grant it.
	ActionAgentRecover = "agent:recover"
	// ActionAgentEnroll is approving a standalone host's keys for one
	// agent, withdrawing that approval and revoking the supervisor it
	// registered (ADR-0082). It stands a machine identity up with
	// lifecycle authority over an agent, and a replacement fences every
	// certificate that agent held, so membership alone does not grant
	// it.
	ActionAgentEnroll = "agent:enroll"
	// ActionAgentCutover is asking a delegated operator to take over a
	// legacy agent's source under its existing GRN (ADR-0086). It
	// decides which service an agent's settings come from, so
	// membership alone does not grant it.
	ActionAgentCutover = "agent:cutover"

	// ActionAgentTemplateRead is reading the org's retired agent
	// templates (ADR-0075, ADR-0088) and the templates the delegated
	// Operator has published (#1170). No action writes a garam template:
	// they are retired read-only.
	ActionAgentTemplateRead = "agent-template:read"
	// ActionAgentTemplatePublish is publishing the next immutable
	// version of a template the delegated Operator holds (#1170). It
	// edits no agent created from an earlier version.
	ActionAgentTemplatePublish = "agent-template:publish"

	// ActionExecutionProfileRead is reading the execution profiles the
	// delegated Operator has published. Publishing one stays with the
	// deployment, and no action here grants it (#1170).
	ActionExecutionProfileRead = "execution-profile:read"
	// ActionAgentExecutionRead is reading an agent's pending, rendered
	// and runtime-reported execution from the delegated Operator
	// (#1170). Reading an agent is reading its identity, and grants
	// none of this.
	ActionAgentExecutionRead = "agent:execution-read"
)

// machineActions is every agent, operator and execution action a role
// grants, one row each and in the order Actions answers them. An owner
// and an admin hold every row. A member holds the rows marked for it:
// it reads the operators an org has registered and registers none,
// because standing a machine identity up for the organization is the
// administrative act registering already looks like. It may create an
// agent from an allowed profile and read the published templates and
// profiles it chooses from, and may not configure an agent, publish a
// template or read an agent's execution.
//
// Agent and operator actions are granted one at a time and never as
// `agent:*` or `operator:*`: ADR-0012 puts provisioning outside garam,
// so a wildcard would grant in advance the actions no endpoint may
// offer. Deleting an operator is what that caution was for — it
// arrived after registering and reading, and a wildcard would have
// granted it before anyone decided it (ADR-0070).
var machineActions = []struct {
	action string
	member bool
}{
	{ActionOperatorRead, true},
	{ActionOperatorRegister, false},
	{ActionOperatorUpdate, false},
	{ActionOperatorDelete, false},
	{ActionOperatorDelegate, false},
	{ActionAgentRead, true},
	{ActionAgentRename, true},
	{ActionAgentCreate, true},
	{ActionAgentConfigure, false},
	{ActionAgentRecover, false},
	{ActionAgentEnroll, false},
	{ActionAgentTemplateRead, true},
	{ActionAgentTemplatePublish, false},
	{ActionExecutionProfileRead, true},
	{ActionAgentExecutionRead, false},
	{ActionAgentCutover, false},
}

// machineActionsHeld returns the machine actions an owner or admin
// holds, or with member set, the ones a member holds.
func machineActionsHeld(member bool) []string {
	var actions []string
	for _, a := range machineActions {
		if a.member || !member {
			actions = append(actions, a.action)
		}
	}
	return actions
}

// Actions returns the action patterns the role grants on an org and
// on the groups, projects, operators and agents it contains. The
// patterns feed IAM-style Allow statements; the evaluator resolves
// wildcards.
func (r Role) Actions() []string {
	switch r {
	case RoleOwner:
		return slices.Concat([]string{
			"org:*", "group:*", "project:*", "policy:*", "consent:*",
		}, machineActionsHeld(false))
	case RoleAdmin:
		return slices.Concat([]string{
			ActionOrgRead, ActionOrgUpdate, ActionOrgMemberList, ActionOrgInvite,
			"group:*", "project:*", "policy:*", "consent:*",
		}, machineActionsHeld(false))
	case RoleMember:
		return slices.Concat([]string{
			ActionOrgRead, ActionOrgMemberList,
			ActionGroupRead, ActionGroupMemberList,
			ActionProjectRead, ActionProjectMemberList,
		}, machineActionsHeld(true))
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
