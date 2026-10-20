package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/operator"
	operatorpg "github.com/garamsh/garam/internal/operator/postgres"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// TestOperatorStore_RoundTrip covers what a registration is (ADR-0045):
// an organization's record that it has an operator, carrying no
// credential and no state anybody reports.
//
// The cascade is asserted because it is what a delete route would
// otherwise have to decide: the record follows the organization out and
// nothing else follows the record.
func TestOperatorStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operatorpg.New(p)
	orgs := orgpg.New(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))

	registeredBy := scopedGRN("acme", "user", "ada")
	one, err := operator.New(orgRootGRN("acme"), "one", registeredBy, operator.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.Register(ctx, one)
	require.NoError(t, err)

	stored, err := s.Get(ctx, one.GRN)
	require.NoError(t, err)
	assert.Equal(t, one.GRN, stored.GRN)
	assert.Equal(t, orgRootGRN("acme"), stored.Organization)
	assert.Equal(t, registeredBy, stored.RegisteredBy)
	assert.False(t, stored.CreatedAt.IsZero())

	// Registering the same GRN twice is a conflict, not a second row.
	_, err = s.Register(ctx, one)
	assert.ErrorIs(t, err, operator.ErrOperatorAlreadyRegistered)

	// A record belongs to the org that registered it.
	elsewhere, err := operator.New(orgRootGRN("globex"), "one", scopedGRN("globex", "user", "ada"), operator.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.Register(ctx, elsewhere)
	require.NoError(t, err)
	listed, err := s.ListByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	require.Len(t, listed, 1, "this org's operators and no other org's")
	assert.Equal(t, one.GRN, listed[0].GRN)

	// An operator nobody registered is a miss rather than an empty
	// record, and the registered one is the control for it.
	_, err = s.Get(ctx, scopedGRN("acme", operator.ServiceType, "two"))
	assert.ErrorIs(t, err, operator.ErrOperatorNotFound)

	// Deleting the org takes its operators with it.
	require.NoError(t, orgs.DeleteOrg(ctx, orgRootGRN("acme")))
	_, err = s.Get(ctx, one.GRN)
	assert.ErrorIs(t, err, operator.ErrOperatorNotFound)
	// The other org is untouched, so the cascade removed a row rather
	// than every row.
	_, err = s.Get(ctx, elsewhere.GRN)
	assert.NoError(t, err)
}

// TestOperatorStore_Manager covers the record that pairs an Operator's
// two identities (ADR-0124): a control service names its manager, at
// most one names each, and the pair is read back on the list.
func TestOperatorStore_Manager(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operatorpg.New(p)
	orgs := orgpg.New(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	by := scopedGRN("acme", "user", "ada")
	register := func(identifier, manager string) (*operator.Operator, error) {
		t.Helper()
		o, err := operator.New(orgRootGRN("acme"), identifier, by, operator.DetailsPatch{})
		require.NoError(t, err)
		if manager != "" {
			require.NoError(t, o.ServeManager(scopedGRN("acme", operator.ServiceType, manager).String()))
		}
		return s.Register(ctx, o)
	}

	manager, err := register("aurora", "")
	require.NoError(t, err)
	assert.Nil(t, manager.Manager, "a manager names none")
	control, err := register("aurora-control", "aurora")
	require.NoError(t, err)
	require.NotNil(t, control.Manager)
	assert.Equal(t, manager.GRN, *control.Manager)

	// The list answers the pair in one read.
	listed, err := s.ListByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	require.Len(t, listed, 2)
	byGRN := map[string]*operator.Operator{}
	for _, o := range listed {
		byGRN[o.GRN.String()] = o
	}
	assert.Nil(t, byGRN[manager.GRN.String()].Manager)
	require.NotNil(t, byGRN[control.GRN.String()].Manager)
	assert.Equal(t, manager.GRN, *byGRN[control.GRN.String()].Manager)

	// Each refusal leaves no row behind: the insert is rolled back with
	// the check that refused it.
	_, err = register("other-control", "aurora")
	assert.ErrorIs(t, err, operator.ErrManagerHasControlService)
	_, err = register("chained", "aurora-control")
	assert.ErrorIs(t, err, operator.ErrManagerIsControlService)
	_, err = register("orphan", "nobody")
	assert.ErrorIs(t, err, operator.ErrManagerNotRegistered)
	for _, id := range []string{"other-control", "chained", "orphan"} {
		_, err = s.Get(ctx, scopedGRN("acme", operator.ServiceType, id))
		assert.ErrorIsf(t, err, operator.ErrOperatorNotFound, "%s was refused and not recorded", id)
	}
	// A second identity with no manager is the control for the
	// refusals: it registers alone.
	_, err = register("standalone", "")
	require.NoError(t, err)

	// Deleting the manager is not refused for its control service, which
	// keeps naming the identity: a lone control service.
	require.NoError(t, s.Delete(ctx, manager.GRN))
	lone, err := s.Get(ctx, control.GRN)
	require.NoError(t, err)
	require.NotNil(t, lone.Manager)
	assert.Equal(t, manager.GRN, *lone.Manager)
	// The deleted manager may be registered again, and not as anyone's
	// control service while one names it.
	_, err = register("aurora", "standalone")
	assert.ErrorIs(t, err, operator.ErrIdentityIsManager)
	_, err = register("aurora", "")
	require.NoError(t, err)

	// The organization's deletion takes the pair with it.
	require.NoError(t, orgs.DeleteOrg(ctx, orgRootGRN("acme")))
	_, err = s.Get(ctx, control.GRN)
	assert.ErrorIs(t, err, operator.ErrOperatorNotFound)
}
