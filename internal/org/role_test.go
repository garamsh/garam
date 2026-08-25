package org_test

import (
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

func TestParseRole_Unknown(t *testing.T) {
	_, err := org.ParseRole("superuser")
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

func TestRoleActions_GrantSets(t *testing.T) {
	assert.Equal(t,
		[]string{
			"org:*", "group:*", "project:*", "policy:*", "consent:*",
			org.ActionOperatorRead, org.ActionOperatorRegister, org.ActionAgentRead, org.ActionAgentDefine,
		},
		org.RoleOwner.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgUpdate, org.ActionOrgMemberList, org.ActionOrgInvite,
			"group:*", "project:*", "policy:*", "consent:*",
			org.ActionOperatorRead, org.ActionOperatorRegister, org.ActionAgentRead, org.ActionAgentDefine,
		},
		org.RoleAdmin.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
			org.ActionOperatorRead, org.ActionAgentRead, org.ActionAgentDefine,
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

// TestRoleActions_OnlyOwnerAndAdminRegisterAnOperator pins the split
// ADR-0044 rests on: every role reads the operators an org registered,
// because defining an agent needs the list to name one from, and only
// the two administrative roles register one. The read is the control —
// without it the member's refusal would also hold for a role that
// reaches no operator action at all.
func TestRoleActions_OnlyOwnerAndAdminRegisterAnOperator(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Contains(t, r.Actions(), org.ActionOperatorRead, string(r))
		assert.NotContains(t, r.Actions(), "operator:*", string(r))
	}
	assert.Contains(t, org.RoleOwner.Actions(), org.ActionOperatorRegister)
	assert.Contains(t, org.RoleAdmin.Actions(), org.ActionOperatorRegister)
	assert.NotContains(t, org.RoleMember.Actions(), org.ActionOperatorRegister)
}

func TestActionsFor_UnionsKnownRoles(t *testing.T) {
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
			org.ActionOperatorRead, org.ActionAgentRead, org.ActionAgentDefine,
			org.ActionOrgUpdate, org.ActionOrgInvite,
			"group:*", "project:*", "policy:*", "consent:*",
			org.ActionOperatorRegister,
		},
		org.ActionsFor([]string{"member", "admin"}))
	assert.Empty(t, org.ActionsFor([]string{"superuser"}))
	assert.Empty(t, org.ActionsFor(nil))
}
