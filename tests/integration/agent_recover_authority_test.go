package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// TestRecoverAuthority_IsHandedOverOnlyUnderAGrantListingIt holds that
// agent:recover is delegated like agent:configure: a grant must name
// it, the handoff binds where the agent runs, and a move of the agent
// revokes it. The grant listing all four operations and the stored
// authority are what migration 000029 widens 000028's checks for.
func TestRecoverAuthority_IsHandedOverOnlyUnderAGrantListingIt(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	g := f.grant(t)

	_, err := f.mint(operator.OperationAgentRecover, f.agent, "rec-1")
	require.ErrorIs(t, err, operator.ErrNotDelegated, "a grant without agent:recover")

	_, _, err = f.write("grant-2", grantRef(g.ID), f.terms(t, []grn.GRN{f.controller},
		operator.OperationAgentCreate, operator.OperationAgentConfigure,
		operator.OperationAgentActivate, operator.OperationAgentRecover))
	require.NoError(t, err)
	minted, err := f.mint(operator.OperationAgentRecover, f.agent, "rec-2")
	require.NoError(t, err, "control: a grant listing it")

	in, err := f.service.Introspect(ctx, f.hosted, minted.Authority)
	require.NoError(t, err)
	assert.Equal(t, operator.OperationAgentRecover, in.Operation)
	require.NotNil(t, in.Assignment)
	assert.Equal(t, operator.Assignment{Operator: f.controller, Epoch: "1"}, *in.Assignment)

	f.placements.move(f.agent, operator.Assignment{Operator: f.controller, Epoch: "2"})
	_, err = f.service.Reference(ctx, f.hosted, minted.Ref)
	assert.ErrorIs(t, err, operator.ErrAuthorityRevoked, "a recovery is bound to where the agent ran")
}

func TestRecoverAuthority_TargetsAnAgentOfTheOrganization(t *testing.T) {
	f := newDelegationFixture(t)
	_, _, err := f.write("grant-1", nil, f.terms(t, []grn.GRN{f.controller},
		operator.OperationAgentRecover))
	require.NoError(t, err)

	_, err = f.mint(operator.OperationAgentRecover, f.controller, "rec-1")
	assert.Error(t, err, "a controller is not an agent")
	_, err = f.mint(operator.OperationAgentRecover, scopedGRN("globex", "agent", "0a1b2c3d4e5f6071"), "rec-2")
	assert.Error(t, err, "another organization's agent")
	_, err = f.mint(operator.OperationAgentRecover, f.agent, "rec-3")
	assert.NoError(t, err, "control: this organization's agent")
}
