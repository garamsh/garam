package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
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

// TestOperatorStore_RegisterPair covers registering an Operator's two
// identities in one write (ADR-0124): both are recorded, the control
// service naming the manager, or neither is.
func TestOperatorStore_RegisterPair(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operatorpg.New(p)
	orgs := orgpg.New(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	by := scopedGRN("acme", "user", "ada")
	pair := func(manager, control string) (*operator.Operator, *operator.Operator) {
		t.Helper()
		m, err := operator.New(orgRootGRN("acme"), manager, by, operator.DetailsPatch{})
		require.NoError(t, err)
		c, err := operator.New(orgRootGRN("acme"), control, by, operator.DetailsPatch{})
		require.NoError(t, err)
		require.NoError(t, c.ServeManager(m.GRN.String()))
		return m, c
	}
	absent := func(ids ...string) {
		t.Helper()
		for _, id := range ids {
			_, err := s.Get(ctx, scopedGRN("acme", operator.ServiceType, id))
			assert.ErrorIsf(t, err, operator.ErrOperatorNotFound, "%s was not recorded", id)
		}
	}

	m, c := pair("aurora", "aurora-control")
	registered, err := s.RegisterPair(ctx, m, c)
	require.NoError(t, err)
	assert.Equal(t, m.GRN, registered.GRN)
	assert.Nil(t, registered.Manager)
	control, err := s.Get(ctx, c.GRN)
	require.NoError(t, err)
	require.NotNil(t, control.Manager)
	assert.Equal(t, m.GRN, *control.Manager)

	// A control service whose GRN is taken refuses the pair, and the
	// manager inserted before it is rolled back with it.
	m, c = pair("borealis", "aurora-control")
	_, err = s.RegisterPair(ctx, m, c)
	assert.ErrorIs(t, err, operator.ErrOperatorAlreadyRegistered)
	absent("borealis")

	// A deleted manager's control service still names it, so a pair
	// under that manager's identifier is refused whole.
	require.NoError(t, s.Delete(ctx, scopedGRN("acme", operator.ServiceType, "aurora")))
	m, c = pair("aurora", "aurora-control-two")
	_, err = s.RegisterPair(ctx, m, c)
	assert.ErrorIs(t, err, operator.ErrManagerHasControlService)
	absent("aurora", "aurora-control-two")

	// The control for the refusals: a pair of fresh identifiers.
	m, c = pair("borealis", "borealis-control")
	_, err = s.RegisterPair(ctx, m, c)
	require.NoError(t, err)

	// A pair whose control service does not name its manager is a
	// caller's bug, refused before anything is written.
	m, err = operator.New(orgRootGRN("acme"), "cirrus", by, operator.DetailsPatch{})
	require.NoError(t, err)
	c, err = operator.New(orgRootGRN("acme"), "cirrus-control", by, operator.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.RegisterPair(ctx, m, c)
	assert.Error(t, err)
	absent("cirrus", "cirrus-control")
}

// TestOperatorStore_SetManager covers linking a control service to its
// cluster manager after both were registered alone (ADR-0124): the link
// is set once, under the refusals a registration makes, and never moves
// after.
func TestOperatorStore_SetManager(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := operatorpg.New(p)
	orgs := orgpg.New(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	by := scopedGRN("acme", "user", "ada")
	op := func(id string) grn.GRN { return scopedGRN("acme", operator.ServiceType, id) }
	for _, id := range []string{"aurora", "aurora-control", "borealis", "lone", "cirrus", "cirrus-control"} {
		o, err := operator.New(orgRootGRN("acme"), id, by, operator.DetailsPatch{})
		require.NoError(t, err)
		_, err = s.Register(ctx, o)
		require.NoError(t, err)
	}

	linked, err := s.SetManager(ctx, op("aurora-control"), op("aurora"))
	require.NoError(t, err)
	require.NotNil(t, linked.Manager)
	assert.Equal(t, op("aurora"), *linked.Manager)
	// Read back, so the answer is the row and not the request.
	stored, err := s.Get(ctx, op("aurora-control"))
	require.NoError(t, err)
	require.NotNil(t, stored.Manager)
	assert.Equal(t, op("aurora"), *stored.Manager)

	// Set once: neither another manager nor the same one moves it.
	_, err = s.SetManager(ctx, op("aurora-control"), op("borealis"))
	assert.ErrorIs(t, err, operator.ErrManagerAlreadySet)
	_, err = s.SetManager(ctx, op("aurora-control"), op("aurora"))
	assert.ErrorIs(t, err, operator.ErrManagerAlreadySet)

	// The refusals a registration makes, each leaving the record naming
	// no manager.
	refusals := []struct {
		name    string
		record  string
		manager string
		want    error
	}{
		{"a cluster manager already named", "lone", "aurora", operator.ErrManagerHasControlService},
		{"a cluster manager that is a control service", "lone", "aurora-control", operator.ErrManagerIsControlService},
		{"a cluster manager not registered", "lone", "nobody", operator.ErrManagerNotRegistered},
		{"a record some control service names", "aurora", "borealis", operator.ErrIdentityIsManager},
	}
	for _, r := range refusals {
		_, err = s.SetManager(ctx, op(r.record), op(r.manager))
		assert.ErrorIsf(t, err, r.want, "%s", r.name)
		after, err := s.Get(ctx, op(r.record))
		require.NoError(t, err)
		assert.Nilf(t, after.Manager, "%s left %s naming none", r.name, r.record)
	}
	_, err = s.SetManager(ctx, op("nobody"), op("borealis"))
	assert.ErrorIs(t, err, operator.ErrOperatorNotFound)

	// The control for the refusals: a free record under a free cluster
	// manager links.
	_, err = s.SetManager(ctx, op("cirrus-control"), op("cirrus"))
	require.NoError(t, err)
}
