package org_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/org"
)

func TestParseRole_KnownRoles(t *testing.T) {
	for _, s := range []string{"owner", "admin", "member"} {
		r, err := org.ParseRole(s)
		require.NoError(t, err)
		assert.Equal(t, s, string(r))
	}
}

// TestParseRole_Unknown reads the name the vocabulary has against one
// it does not: the same call, differing only in the string, so what
// refuses is the name rather than a parse that refuses every name.
func TestParseRole_Unknown(t *testing.T) {
	r, err := org.ParseRole("member")
	require.NoError(t, err)
	require.Equal(t, "member", string(r))

	_, err = org.ParseRole("superuser")
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// TestActionConstants_SpellTheirWireStrings pins the action vocabulary
// as text. An action string is what a stored policy document names and
// what `api/openapi.yaml` and `client/src/api.ts` spell out, so
// renaming one breaks a contract already published; every other
// assertion in this file compares a constant to itself and reports
// nothing when it moves. Each is also one IsAction answers for, so a
// constant left out of the vocabulary fails here.
func TestActionConstants_SpellTheirWireStrings(t *testing.T) {
	cases := []struct {
		constant string
		want     string
	}{
		{org.ActionOrgRead, "iam:org.read"},
		{org.ActionOrgUpdate, "iam:org.update"},
		{org.ActionOrgDelete, "iam:org.delete"},
		{org.ActionOrgMemberList, "iam:org.member-list"},
		{org.ActionOrgInvite, "iam:org.invite"},

		{org.ActionGroupCreate, "iam:group.create"},
		{org.ActionGroupRead, "iam:group.read"},
		{org.ActionGroupUpdate, "iam:group.update"},
		{org.ActionGroupDelete, "iam:group.delete"},
		{org.ActionGroupMemberList, "iam:group.member-list"},
		{org.ActionGroupMemberAdd, "iam:group.member-add"},
		{org.ActionGroupMemberRemove, "iam:group.member-remove"},

		{org.ActionProjectCreate, "iam:project.create"},
		{org.ActionProjectRead, "iam:project.read"},
		{org.ActionProjectUpdate, "iam:project.update"},
		{org.ActionProjectDelete, "iam:project.delete"},
		{org.ActionProjectMemberList, "iam:project.member-list"},
		{org.ActionProjectMemberAdd, "iam:project.member-add"},
		{org.ActionProjectMemberRemove, "iam:project.member-remove"},

		{org.ActionPolicyCreate, "iam:policy.create"},
		{org.ActionPolicyRead, "iam:policy.read"},
		{org.ActionPolicyUpdate, "iam:policy.update"},
		{org.ActionPolicyAttach, "iam:policy.attach"},
		{org.ActionPolicyDetach, "iam:policy.detach"},
		{org.ActionPolicyDelete, "iam:policy.delete"},
		{org.ActionAuditRead, "iam:audit.read"},

		{org.ActionConsentRead, "iam:consent.read"},
		{org.ActionConsentGrant, "iam:consent.grant"},
		{org.ActionConsentWithdraw, "iam:consent.withdraw"},

		{org.ActionOperatorRead, "iam:operator.read"},
		{org.ActionOperatorRegister, "iam:operator.register"},
		{org.ActionOperatorUpdate, "iam:operator.update"},
		{org.ActionOperatorDelete, "iam:operator.delete"},
		{org.ActionOperatorReissue, "iam:operator.reissue"},
		{org.ActionOperatorDelegate, "iam:operator.delegate"},

		{org.ActionAgentRead, "iam:agent.read"},
		{org.ActionAgentWatch, "iam:agent.watch"},
		{org.ActionAgentRename, "iam:agent.rename"},
		{org.ActionAgentCreate, "iam:agent.create"},
		{org.ActionAgentConfigure, "iam:agent.configure"},
		{org.ActionAgentRecover, "iam:agent.recover"},
		{org.ActionAgentReplace, "iam:agent.replace"},

		{org.ActionAgentEnroll, "iam:agent.enroll"},
		{org.ActionAgentTemplateRead, "iam:agent-template.read"},
		{org.ActionAgentTemplatePublish, "iam:agent-template.publish"},
		{org.ActionExecutionProfileRead, "iam:execution-profile.read"},
		{org.ActionAgentExecutionRead, "iam:agent.execution-read"},

		{org.ActionCoworkPageRead, "cowork:page.read"},
		{org.ActionCoworkPageWrite, "cowork:page.write"},
		{org.ActionCoworkChannelRead, "cowork:channel.read"},
		{org.ActionCoworkChannelPost, "cowork:channel.post"},
		{org.ActionCoworkTaskRead, "cowork:task.read"},
		{org.ActionCoworkTaskWrite, "cowork:task.write"},
		{org.ActionCoworkArtifactRead, "cowork:artifact.read"},
		{org.ActionCoworkArtifactWrite, "cowork:artifact.write"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			assert.Equal(t, c.want, c.constant)
			assert.True(t, org.IsAction(c.constant))
		})
	}
}

// TestIsAction_RefusesWhatOnlyAWildcardMatches holds that a name a
// role's wildcard covers is not thereby an action. The defined action
// with the same prefix is the control.
func TestIsAction_RefusesWhatOnlyAWildcardMatches(t *testing.T) {
	assert.True(t, org.IsAction(org.ActionOrgDelete))
	for _, name := range []string{"iam:org.*", "iam:org.frobnicate", ""} {
		assert.False(t, org.IsAction(name), name)
	}
}

func TestRoleActions_GrantSets(t *testing.T) {
	assert.Equal(t,
		[]string{
			"iam:org.*", "iam:group.*", "iam:project.*", "iam:policy.*", "iam:consent.*",
			org.ActionAuditRead,
			org.ActionOperatorRead, org.ActionOperatorRegister, org.ActionOperatorUpdate,
			org.ActionOperatorDelete, org.ActionOperatorReissue, org.ActionOperatorDelegate, org.ActionAgentRead,
			org.ActionAgentRename, org.ActionAgentCreate,
			org.ActionAgentConfigure, org.ActionAgentRecover, org.ActionAgentEnroll, org.ActionAgentTemplateRead,
			org.ActionAgentTemplatePublish,
			org.ActionExecutionProfileRead, org.ActionAgentExecutionRead,
			org.ActionAgentReplace,
			org.ActionCoworkPageRead, org.ActionCoworkPageWrite,
			org.ActionCoworkChannelRead, org.ActionCoworkChannelPost,
			org.ActionCoworkTaskRead, org.ActionCoworkTaskWrite,
			org.ActionCoworkArtifactRead, org.ActionCoworkArtifactWrite,
			org.ActionCodehubRepoRead, org.ActionCodehubOrgUse, org.ActionCodehubRepoWrite,
			org.ActionCodehubRepoCreate, org.ActionCodehubRunnerAdminister,
		},
		org.RoleOwner.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgUpdate, org.ActionOrgMemberList, org.ActionOrgInvite,
			"iam:group.*", "iam:project.*", "iam:policy.*", "iam:consent.*",
			org.ActionAuditRead,
			org.ActionOperatorRead, org.ActionOperatorRegister, org.ActionOperatorUpdate,
			org.ActionOperatorDelete, org.ActionOperatorReissue, org.ActionOperatorDelegate, org.ActionAgentRead,
			org.ActionAgentRename, org.ActionAgentCreate,
			org.ActionAgentConfigure, org.ActionAgentRecover, org.ActionAgentEnroll, org.ActionAgentTemplateRead,
			org.ActionAgentTemplatePublish,
			org.ActionExecutionProfileRead, org.ActionAgentExecutionRead,
			org.ActionAgentReplace,
			org.ActionCoworkPageRead, org.ActionCoworkPageWrite,
			org.ActionCoworkChannelRead, org.ActionCoworkChannelPost,
			org.ActionCoworkTaskRead, org.ActionCoworkTaskWrite,
			org.ActionCoworkArtifactRead, org.ActionCoworkArtifactWrite,
			org.ActionCodehubRepoRead, org.ActionCodehubOrgUse, org.ActionCodehubRepoWrite,
			org.ActionCodehubRepoCreate, org.ActionCodehubRunnerAdminister,
		},
		org.RoleAdmin.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
			org.ActionOperatorRead, org.ActionAgentRead,
			org.ActionAgentRename, org.ActionAgentCreate, org.ActionAgentTemplateRead,
			org.ActionExecutionProfileRead,
			org.ActionCoworkPageRead, org.ActionCoworkPageWrite,
			org.ActionCoworkChannelRead, org.ActionCoworkChannelPost,
			org.ActionCoworkTaskRead, org.ActionCoworkTaskWrite,
			org.ActionCoworkArtifactRead, org.ActionCoworkArtifactWrite,
			org.ActionCodehubRepoRead, org.ActionCodehubOrgUse,
		},
		org.RoleMember.Actions())
	assert.Empty(t, org.Role("").Actions())
}

// TestRoleActions_NoRoleReachesAgentWatch pins what the grant sets
// above leave implicit: no role names `iam:agent.watch`, and none
// carries the `iam:agent.*` that would reach it without naming it.
// ADR-0022 separates watching from reading so an organization may give
// one and withhold the other, and ADR-0012 keeps provisioning out of a
// wildcard; both make an agent action a role grants a deliberate one.
//
// The read and the rename are the control. Without them the two
// refusals hold for a role that grants nothing at all, which is a
// different state and the one most likely to arrive by accident.
func TestRoleActions_NoRoleReachesAgentWatch(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.NotContains(t, r.Actions(), org.ActionAgentWatch, string(r))
		assert.NotContains(t, r.Actions(), "iam:agent.*", string(r))
		assert.Contains(t, r.Actions(), org.ActionAgentRead, string(r))
		assert.Contains(t, r.Actions(), org.ActionAgentRename, string(r))
	}
}

// TestRoleActions_NoRoleDefinesAnAgent pins ADR-0090: garam accepts no
// desired value, so no role grants the `iam:agent.define` a definition
// was written under. Creating through the control service is the
// control: every role still reaches an agent's creation.
func TestRoleActions_NoRoleDefinesAnAgent(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.NotContains(t, r.Actions(), "iam:agent.define", string(r))
		assert.Contains(t, r.Actions(), org.ActionAgentCreate, string(r))
	}
}

// TestRoleActions_OnlyOwnerAndAdminRegisterAnOperator pins the split
// ADR-0045 rests on: every role reads the operators an org registered,
// because defining an agent needed the list to name one from, and only
// the two administrative roles register one. Deleting one goes with
// registering it and is named separately, because it is the action
// that stops an operator rather than records one (ADR-0070), and so is
// changing what the org calls one (ADR-0074). The read is the control
// — without it the member's refusals would also hold for a role that
// reaches no operator action at all.
func TestRoleActions_OnlyOwnerAndAdminRegisterAnOperator(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionOperatorRead, string(r))
		assert.NotContains(t, r.Actions(), "iam:operator.*", string(r))
	}
	for _, action := range []string{org.ActionOperatorRegister, org.ActionOperatorUpdate, org.ActionOperatorDelete, org.ActionOperatorReissue} {
		assert.Contains(t, org.RoleOwner.Actions(), action)
		assert.Contains(t, org.RoleAdmin.Actions(), action)
		assert.NotContains(t, org.RoleMember.Actions(), action)
	}
}

// TestRoleActions_OnlyOwnerAndAdminConfigureOrDelegate pins ADR-0078's
// split: every role may ask a delegated operator to create an agent
// from an allowed profile, and only the two administrative roles may
// change execution settings or delegate to an operator. Creating is the
// control: without it the member's refusals would also hold for a role
// that reaches no execution action at all.
func TestRoleActions_OnlyOwnerAndAdminConfigureOrDelegate(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionAgentCreate, string(r))
	}
	for _, action := range []string{org.ActionAgentConfigure, org.ActionOperatorDelegate} {
		assert.Contains(t, org.RoleOwner.Actions(), action)
		assert.Contains(t, org.RoleAdmin.Actions(), action)
		assert.NotContains(t, org.RoleMember.Actions(), action)
	}
}

// TestRoleActions_OnlyOwnerAndAdminPublishOrReadExecution pins the
// #1170 matrix: every role reads the Operator's published templates and
// profiles it creates an agent from, and only the two administrative
// roles publish a template or read an agent's execution. The two reads
// are the control: without them the member's refusals would also hold
// for a role that reaches no Operator action at all.
func TestRoleActions_OnlyOwnerAndAdminPublishOrReadExecution(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionAgentTemplateRead, string(r))
		assert.Contains(t, r.Actions(), org.ActionExecutionProfileRead, string(r))
	}
	for _, action := range []string{org.ActionAgentTemplatePublish, org.ActionAgentExecutionRead} {
		assert.Contains(t, org.RoleOwner.Actions(), action)
		assert.Contains(t, org.RoleAdmin.Actions(), action)
		assert.NotContains(t, org.RoleMember.Actions(), action)
	}
}

// TestRoleActions_EveryRoleReadsAndWritesCoworkPages pins ADR-0127's
// default: a member of the organization reads and writes its cowork
// pages, and a policy narrows that like any other action. Each is
// named rather than granted as `cowork:*`, so an action a later
// milestone adds is granted when it is decided and not before.
func TestRoleActions_EveryRoleReadsAndWritesCoworkPages(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionCoworkPageRead, string(r))
		assert.Contains(t, r.Actions(), org.ActionCoworkPageWrite, string(r))
		assert.NotContains(t, r.Actions(), "cowork:*", string(r))
	}
}

// TestRoleActions_EveryRoleReadsAndPostsInCoworkChannels pins the
// same default for messaging (ADR-0127, ADR-0128): a member reads and
// posts in the organization's channels, and a policy narrows that.
func TestRoleActions_EveryRoleReadsAndPostsInCoworkChannels(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionCoworkChannelRead, string(r))
		assert.Contains(t, r.Actions(), org.ActionCoworkChannelPost, string(r))
	}
}

// TestRoleActions_EveryRoleReadsAndWritesCoworkTasks pins the same
// default for tasks (ADR-0127): a member reads and writes the
// organization's tasks, and a policy narrows that.
func TestRoleActions_EveryRoleReadsAndWritesCoworkTasks(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionCoworkTaskRead, string(r))
		assert.Contains(t, r.Actions(), org.ActionCoworkTaskWrite, string(r))
	}
}

// TestRoleActions_EveryRoleReadsAndWritesCoworkArtifacts pins the
// same default for artifacts (ADR-0127).
func TestRoleActions_EveryRoleReadsAndWritesCoworkArtifacts(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionCoworkArtifactRead, string(r))
		assert.Contains(t, r.Actions(), org.ActionCoworkArtifactWrite, string(r))
	}
}

func TestActionsFor_UnionsKnownRoles(t *testing.T) {
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
			org.ActionOperatorRead, org.ActionAgentRead,
			org.ActionAgentRename, org.ActionAgentCreate, org.ActionAgentTemplateRead,
			org.ActionExecutionProfileRead,
			org.ActionCoworkPageRead, org.ActionCoworkPageWrite,
			org.ActionCoworkChannelRead, org.ActionCoworkChannelPost,
			org.ActionCoworkTaskRead, org.ActionCoworkTaskWrite,
			org.ActionCoworkArtifactRead, org.ActionCoworkArtifactWrite,
			org.ActionCodehubRepoRead, org.ActionCodehubOrgUse,
			org.ActionOrgUpdate, org.ActionOrgInvite,
			"iam:group.*", "iam:project.*", "iam:policy.*", "iam:consent.*",
			org.ActionAuditRead,
			org.ActionOperatorRegister, org.ActionOperatorUpdate, org.ActionOperatorDelete,
			org.ActionOperatorReissue, org.ActionOperatorDelegate, org.ActionAgentConfigure, org.ActionAgentRecover, org.ActionAgentEnroll,
			org.ActionAgentTemplatePublish, org.ActionAgentExecutionRead,
			org.ActionAgentReplace,
			org.ActionCodehubRepoWrite, org.ActionCodehubRepoCreate, org.ActionCodehubRunnerAdminister,
		},
		org.ActionsFor([]string{"member", "admin"}))
	assert.Empty(t, org.ActionsFor([]string{"superuser"}))
	assert.Empty(t, org.ActionsFor(nil))
}

// TestRoleActions_AMemberReadsRepositoriesAndOnlyOwnersAndAdminsWrite
// pins ADR-0149 item 1: every role reads and uses codehub, and an
// owner's `permit` says who else writes. Each is named rather than
// granted as `codehub:*`.
func TestRoleActions_AMemberReadsRepositoriesAndOnlyOwnersAndAdminsWrite(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionCodehubRepoRead, string(r))
		assert.Contains(t, r.Actions(), org.ActionCodehubOrgUse, string(r))
		assert.NotContains(t, r.Actions(), "codehub:*", string(r))
	}
	for _, action := range []string{
		org.ActionCodehubRepoWrite, org.ActionCodehubRepoCreate, org.ActionCodehubRunnerAdminister,
	} {
		assert.Contains(t, org.RoleOwner.Actions(), action)
		assert.Contains(t, org.RoleAdmin.Actions(), action)
		assert.NotContains(t, org.RoleMember.Actions(), action)
	}
}

// TestIsAction_NamesNoCodehubAction holds that codehub's actions are
// granted by the roles and asked of the policy service, not checked
// by an operation of this platform. The cowork action beside them is
// the control.
func TestIsAction_NamesNoCodehubAction(t *testing.T) {
	assert.True(t, org.IsAction(org.ActionCoworkPageRead))
	for _, action := range []string{
		org.ActionCodehubRepoRead, org.ActionCodehubRepoWrite, org.ActionCodehubRepoCreate,
		org.ActionCodehubRunnerAdminister, org.ActionCodehubOrgUse,
	} {
		assert.False(t, org.IsAction(action), action)
	}
}
