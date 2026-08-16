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
	assert.Equal(t, []string{"org:*", "group:*", "project:*", "policy:*", org.ActionAgentRead}, org.RoleOwner.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgUpdate, org.ActionOrgMemberList, org.ActionOrgInvite,
			"group:*", "project:*", "policy:*", org.ActionAgentRead,
		},
		org.RoleAdmin.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
			org.ActionAgentRead,
		},
		org.RoleMember.Actions())
	assert.Empty(t, org.Role("").Actions())
}

// TestRoleActions_NoWildcardOverAgents pins what the grant sets above
// leave implicit: no role grants `agent:*`, so an action added to this
// vocabulary later is granted deliberately rather than by a pattern
// already in place (ADR-0012).
func TestRoleActions_NoWildcardOverAgents(t *testing.T) {
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.NotContains(t, r.Actions(), "agent:*", string(r))
		assert.Contains(t, r.Actions(), org.ActionAgentRead, string(r))
	}
}

func TestActionsFor_UnionsKnownRoles(t *testing.T) {
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
			org.ActionAgentRead,
			org.ActionOrgUpdate, org.ActionOrgInvite,
			"group:*", "project:*", "policy:*",
		},
		org.ActionsFor([]string{"member", "admin"}))
	assert.Empty(t, org.ActionsFor([]string{"superuser"}))
	assert.Empty(t, org.ActionsFor(nil))
}
