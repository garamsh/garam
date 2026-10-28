package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
)

func TestNewGRN_IsOwnedByTheOrgAndUnique(t *testing.T) {
	orgGRN := grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme")

	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	assert.Equal(t, "acme", g.Organization)
	assert.Equal(t, grn.DefaultProject, g.Project)
	assert.Equal(t, agent.ServiceType, g.ServiceType)
	assert.Len(t, g.Identifier, 16, "eight random bytes, hex-encoded")

	other, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	assert.NotEqual(t, g, other)
}

func TestNew_StartsBeforeProvisioningAndBeforeRunning(t *testing.T) {
	orgGRN := grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme")
	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)

	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, g, a.GRN)
	assert.Equal(t, orgGRN, a.Organization)
	assert.Equal(t, agent.ProvisioningStatePending, a.ProvisioningState)
	assert.Equal(t, agent.RuntimeStateUnknown, a.RuntimeState)
}

func TestNew_RejectsAGRNTheOrganizationDoesNotOwn(t *testing.T) {
	orgGRN := grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme")

	tests := []struct {
		name string
		g    grn.GRN
	}{
		{
			name: "another service type",
			g:    grn.New("acme", grn.DefaultProject, agent.ServiceTypeOperator, "one"),
		},
		{
			name: "another organization",
			g:    grn.New("evilcorp", grn.DefaultProject, agent.ServiceType, "deadbeef"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := agent.New(tt.g, orgGRN)
			assert.Nil(t, a)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

func TestParseProvisioningState(t *testing.T) {
	for _, state := range []agent.ProvisioningState{
		agent.ProvisioningStatePending,
		agent.ProvisioningStateProvisioned,
		agent.ProvisioningStateReady,
		agent.ProvisioningStateFailed,
	} {
		t.Run(string(state), func(t *testing.T) {
			got, err := agent.ParseProvisioningState(string(state))
			require.NoError(t, err)
			assert.Equal(t, state, got)
		})
	}

	t.Run("unknown state", func(t *testing.T) {
		_, err := agent.ParseProvisioningState("connected")
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	})
}

func TestParseRuntimeState(t *testing.T) {
	for _, state := range []agent.RuntimeState{
		agent.RuntimeStateUnknown,
		agent.RuntimeStateConnected,
		agent.RuntimeStateDisconnected,
	} {
		t.Run(string(state), func(t *testing.T) {
			got, err := agent.ParseRuntimeState(string(state))
			require.NoError(t, err)
			assert.Equal(t, state, got)
		})
	}

	t.Run("unknown state", func(t *testing.T) {
		_, err := agent.ParseRuntimeState("ready")
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	})
}
