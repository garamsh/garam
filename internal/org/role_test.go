package org_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/org"
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
	assert.Equal(t, []string{"org:*", "group:*", "project:*"}, org.RoleOwner.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgUpdate, org.ActionOrgMemberList, org.ActionOrgInvite,
			"group:*", "project:*",
		},
		org.RoleAdmin.Actions())
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
		},
		org.RoleMember.Actions())
	assert.Empty(t, org.Role("").Actions())
}

func TestActionsFor_UnionsKnownRoles(t *testing.T) {
	assert.Equal(t,
		[]string{
			org.ActionOrgRead, org.ActionOrgMemberList,
			org.ActionGroupRead, org.ActionGroupMemberList,
			org.ActionProjectRead, org.ActionProjectMemberList,
			org.ActionOrgUpdate, org.ActionOrgInvite,
			"group:*", "project:*",
		},
		org.ActionsFor([]string{"member", "admin"}))
	assert.Empty(t, org.ActionsFor([]string{"superuser"}))
	assert.Empty(t, org.ActionsFor(nil))
}
