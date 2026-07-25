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

func TestRoleAllows_Ordering(t *testing.T) {
	cases := []struct {
		held, required org.Role
		allowed        bool
	}{
		{org.RoleOwner, org.RoleOwner, true},
		{org.RoleOwner, org.RoleAdmin, true},
		{org.RoleOwner, org.RoleMember, true},
		{org.RoleAdmin, org.RoleOwner, false},
		{org.RoleAdmin, org.RoleAdmin, true},
		{org.RoleAdmin, org.RoleMember, true},
		{org.RoleMember, org.RoleOwner, false},
		{org.RoleMember, org.RoleAdmin, false},
		{org.RoleMember, org.RoleMember, true},
	}
	for _, c := range cases {
		t.Run(string(c.held)+"/"+string(c.required), func(t *testing.T) {
			assert.Equal(t, c.allowed, c.held.Allows(c.required))
		})
	}
}
