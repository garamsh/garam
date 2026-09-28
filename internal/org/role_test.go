package org_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/org"
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
// what `api/openapi.yaml` and `web/src/types/api.ts` spell out, so
// renaming one breaks a contract already published; every other
// assertion in this file compares a constant to itself and reports
// nothing when it moves.
func TestActionConstants_SpellTheirWireStrings(t *testing.T) {
	cases := []struct {
		constant string
		want     string
	}{
		{org.ActionOrgRead, "org:read"},
		{org.ActionOrgUpdate, "org:update"},
		{org.ActionOrgDelete, "org:delete"},
		{org.ActionOrgMemberList, "org:member-list"},
		{org.ActionOrgInvite, "org:invite"},

		{org.ActionGroupCreate, "group:create"},
		{org.ActionGroupRead, "group:read"},
		{org.ActionGroupUpdate, "group:update"},
		{org.ActionGroupDelete, "group:delete"},
		{org.ActionGroupMemberList, "group:member-list"},
		{org.ActionGroupMemberAdd, "group:member-add"},
		{org.ActionGroupMemberRemove, "group:member-remove"},

		{org.ActionProjectCreate, "project:create"},
		{org.ActionProjectRead, "project:read"},
		{org.ActionProjectUpdate, "project:update"},
		{org.ActionProjectDelete, "project:delete"},
		{org.ActionProjectMemberList, "project:member-list"},
		{org.ActionProjectMemberAdd, "project:member-add"},
		{org.ActionProjectMemberRemove, "project:member-remove"},

		{org.ActionPolicyCreate, "policy:create"},
		{org.ActionPolicyRead, "policy:read"},
		{org.ActionPolicyUpdate, "policy:update"},
		{org.ActionPolicyAttach, "policy:attach"},
		{org.ActionPolicyDetach, "policy:detach"},

		{org.ActionConsentRead, "consent:read"},
		{org.ActionConsentGrant, "consent:grant"},
		{org.ActionConsentWithdraw, "consent:withdraw"},

		{org.ActionOperatorRead, "operator:read"},
		{org.ActionOperatorRegister, "operator:register"},
		{org.ActionOperatorUpdate, "operator:update"},
		{org.ActionOperatorDelete, "operator:delete"},

		{org.ActionAgentRead, "agent:read"},
		{org.ActionAgentDefine, "agent:define"},
		{org.ActionAgentConfigure, "agent:configure"},
		{org.ActionAgentWatch, "agent:watch"},
		{org.ActionAgentRename, "agent:rename"},

		{org.ActionAgentTemplateRead, "agent-template:read"},
		{org.ActionAgentTemplateCreate, "agent-template:create"},
		{org.ActionAgentTemplateUpdate, "agent-template:update"},
		{org.ActionAgentTemplateDelete, "agent-template:delete"},
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			assert.Equal(t, c.want, c.constant)
		})
	}
}

func TestRoleActions_GrantSets(t *testing.T) {
	assert.Equal(t,
		[]string{
			"org:*", "group:*", "project:*", "policy:*", "consent:*",
			org.ActionOperatorRead, org.ActionOperatorRegister, org.ActionOperatorUpdate,
			org.ActionOperatorDelete, org.ActionAgentRead, org.ActionAgentDefine,
			org.ActionAgentRename, org.ActionAgentTemplateRead, org.ActionAgentTemplateCreate,
			org.ActionAgentTemplateUpdate, org.ActionAgentTemplateDelete,
			org.ActionAgentConfigure,
		},
		org.RoleOwner.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgUpdate, org.ActionOrgMemberList, org.ActionOrgInvite,
			"group:*", "project:*", "policy:*", "consent:*",
			org.ActionOperatorRead, org.ActionOperatorRegister, org.ActionOperatorUpdate,
			org.ActionOperatorDelete, org.ActionAgentRead, org.ActionAgentDefine,
			org.ActionAgentRename, org.ActionAgentTemplateRead, org.ActionAgentTemplateCreate,
			org.ActionAgentTemplateUpdate, org.ActionAgentTemplateDelete,
			org.ActionAgentConfigure,
		},
		org.RoleAdmin.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
			org.ActionOperatorRead, org.ActionAgentRead, org.ActionAgentDefine,
			org.ActionAgentRename, org.ActionAgentTemplateRead,
			org.ActionAgentConfigure,
		},
		org.RoleMember.Actions())
	assert.Empty(t, org.Role("").Actions())
}

// TestRoleActions_NoRoleReachesAgentWatch pins what the grant sets
// above leave implicit: no role names `agent:watch`, and none carries
// the `agent:*` that would reach it without naming it. ADR-0022
// separates watching from reading so an organization may give one and
// withhold the other, and ADR-0012 keeps provisioning out of a
// wildcard; both make an agent action a role grants a deliberate one.
//
// The read and the define are the control. Without them the two
// refusals hold for a role that grants nothing at all, which is a
// different state and the one most likely to arrive by accident.
func TestRoleActions_NoRoleReachesAgentWatch(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.NotContains(t, r.Actions(), org.ActionAgentWatch, string(r))
		assert.NotContains(t, r.Actions(), "agent:*", string(r))
		assert.Contains(t, r.Actions(), org.ActionAgentRead, string(r))
		assert.Contains(t, r.Actions(), org.ActionAgentDefine, string(r))
	}
}

// TestRoleActions_ConfigureGoesWhereDefineGoes pins that revising an
// agent's values is granted to exactly the roles that define one
// (ADR-0076): the values are what defining writes. The role with no
// name is the control, granting neither, so the pairing is not read
// off a role that grants nothing.
func TestRoleActions_ConfigureGoesWhereDefineGoes(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember, org.Role("")} {
		assert.Equal(t,
			slices.Contains(r.Actions(), org.ActionAgentDefine),
			slices.Contains(r.Actions(), org.ActionAgentConfigure),
			string(r))
	}
	assert.Contains(t, org.RoleMember.Actions(), org.ActionAgentConfigure)
	assert.NotContains(t, org.Role("").Actions(), org.ActionAgentConfigure)
}

// TestRoleActions_OnlyOwnerAndAdminRegisterAnOperator pins the split
// ADR-0045 rests on: every role reads the operators an org registered,
// because defining an agent needs the list to name one from, and only
// the two administrative roles register one. Deleting one goes with
// registering it and is named separately, because it is the action
// that stops an operator rather than records one (ADR-0070), and so is
// changing what the org calls one (ADR-0074). The read is the control
// — without it the member's refusals would also hold for a role that
// reaches no operator action at all.
func TestRoleActions_OnlyOwnerAndAdminRegisterAnOperator(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionOperatorRead, string(r))
		assert.NotContains(t, r.Actions(), "operator:*", string(r))
	}
	for _, action := range []string{org.ActionOperatorRegister, org.ActionOperatorUpdate, org.ActionOperatorDelete} {
		assert.Contains(t, org.RoleOwner.Actions(), action)
		assert.Contains(t, org.RoleAdmin.Actions(), action)
		assert.NotContains(t, org.RoleMember.Actions(), action)
	}
}

func TestActionsFor_UnionsKnownRoles(t *testing.T) {
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
			org.ActionOperatorRead, org.ActionAgentRead, org.ActionAgentDefine,
			org.ActionAgentRename, org.ActionAgentTemplateRead,
			org.ActionAgentConfigure,
			org.ActionOrgUpdate, org.ActionOrgInvite,
			"group:*", "project:*", "policy:*", "consent:*",
			org.ActionOperatorRegister, org.ActionOperatorUpdate, org.ActionOperatorDelete,
			org.ActionAgentTemplateCreate, org.ActionAgentTemplateUpdate, org.ActionAgentTemplateDelete,
		},
		org.ActionsFor([]string{"member", "admin"}))
	assert.Empty(t, org.ActionsFor([]string{"superuser"}))
	assert.Empty(t, org.ActionsFor(nil))
}
