package org_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/org"
)

func TestNewInvitationToken_Format(t *testing.T) {
	token, err := org.NewInvitationToken()
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{48}$`), token)

	other, err := org.NewInvitationToken()
	require.NoError(t, err)
	assert.NotEqual(t, token, other)
}

func TestInvitation_Lifecycle(t *testing.T) {
	orgGRN := grn.New("root", "default", "org", "acme")
	creator := grn.New("acme", "default", "user", "ada")
	expires := time.Now().Add(time.Hour)

	inv := org.NewInvitation("tok", orgGRN, org.RoleMember, creator, expires)
	assert.False(t, inv.Accepted())
	assert.False(t, inv.Expired(time.Now()))
	assert.True(t, inv.Expired(expires.Add(time.Second)))
	assert.Equal(t, orgGRN, inv.Organization())
	assert.Equal(t, org.RoleMember, inv.Role())
	assert.Equal(t, creator, inv.CreatedBy())
}
