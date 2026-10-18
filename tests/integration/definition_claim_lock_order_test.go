package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// TestDefinitionClaimLockOrder_AnOrganizationDeletionAndAClaimBothComplete
// is #1441. A claim holds its definition's row and then, at the agent
// insert's foreign key, waits on the organization's row; the
// organization's deletion holds that row and its cascade then reaches
// the definition. Parked inside its trigger on the definition's
// operator, after it holds the organization's row, the deletion lets
// the claim take the definition first; on release each needed the row
// the other held. The claim now holds the organization's row before the
// definition's, the order the deletion takes them in, so it waits for
// the deletion holding nothing and finds its definition gone.
func TestDefinitionClaimLockOrder_AnOrganizationDeletionAndAClaimBothComplete(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	operatorGRN := scopedGRN("acme", agent.ServiceTypeOperator, "controller")
	d := defineAgent(t, p, s, operatorGRN, agent.Values{"model": "haiku"})
	require.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM agents WHERE grn = $1`, d.Agent().String()),
		"the definition is unclaimed: no agent row exists yet")

	// A holder of the operator's registration, as an exchange of it is:
	// the deletion's trigger (000047) waits on it after the deletion
	// holds the organization's row.
	parked, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = parked.Rollback(ctx) }()
	_, err = parked.Exec(ctx, `SELECT grn FROM operators WHERE grn = $1 FOR KEY SHARE`, operatorGRN.String())
	require.NoError(t, err)
	deleted := make(chan error, 1)
	go func() { deleted <- orgpg.New(p).DeleteOrg(ctx, orgRootGRN("acme")) }()
	awaitSettled(t, p, 1, returned(deleted))
	type claim struct {
		assignment *agent.Assignment
		err        error
	}
	claimed := make(chan claim, 1)
	go func() {
		a, err := s.Claim(ctx, d.Agent(), operatorGRN)
		claimed <- claim{a, err}
	}()
	awaitSettled(t, p, 2, returned(deleted), returned(claimed))
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, deleted), "the organization's deletion completes rather than deadlocking")
	assert.ErrorIs(t, receive(t, claimed).err, agent.ErrDefinitionNotFound,
		"the claim waited for the deletion and found its definition gone")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM agent_definitions`))
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM agents`))
}
