package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/operator"
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
	s := operator.NewPostgresStore(p)
	orgs := orgpg.New(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))

	registeredBy := scopedGRN("acme", "user", "ada")
	one, err := operator.New(orgRootGRN("acme"), "one", registeredBy, operator.DetailsPatch{})
	require.NoError(t, err)
	enrollment, err := operator.NewEnrollment(one.GRN(), registeredBy)
	require.NoError(t, err)
	_, err = s.Register(ctx, one, enrollment, credentialFor(t, one))
	require.NoError(t, err)

	stored, err := s.Get(ctx, one.GRN())
	require.NoError(t, err)
	assert.Equal(t, one.GRN(), stored.GRN())
	assert.Equal(t, orgRootGRN("acme"), stored.Organization())
	assert.Equal(t, registeredBy, stored.RegisteredBy())
	assert.False(t, stored.CreatedAt().IsZero())

	// Registering the same GRN twice is a conflict, not a second row —
	// and it mints no second token either, because the two are one
	// write.
	again, err := operator.NewEnrollment(one.GRN(), registeredBy)
	require.NoError(t, err)
	_, err = s.Register(ctx, one, again, credentialFor(t, one))
	assert.ErrorIs(t, err, operator.ErrOperatorAlreadyRegistered)
	_, err = s.Spend(ctx, again.TokenHash())
	assert.ErrorIs(t, err, operator.ErrEnrollmentInvalid,
		"the refused registration rolled back the token beside it")

	// A record belongs to the org that registered it.
	elsewhere, err := operator.New(orgRootGRN("globex"), "one", scopedGRN("globex", "user", "ada"), operator.DetailsPatch{})
	require.NoError(t, err)
	elsewhereEnrollment, err := operator.NewEnrollment(elsewhere.GRN(), scopedGRN("globex", "user", "ada"))
	require.NoError(t, err)
	_, err = s.Register(ctx, elsewhere, elsewhereEnrollment, credentialFor(t, elsewhere))
	require.NoError(t, err)
	listed, err := s.ListByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	require.Len(t, listed, 1, "this org's operators and no other org's")
	assert.Equal(t, one.GRN(), listed[0].GRN())

	// An operator nobody registered is a miss rather than an empty
	// record, and the registered one is the control for it.
	_, err = s.Get(ctx, scopedGRN("acme", operator.ServiceType, "two"))
	assert.ErrorIs(t, err, operator.ErrOperatorNotFound)

	// Deleting the org takes its operators and the definitions naming
	// them in one statement. The foreign key between the two is NO
	// ACTION for exactly this: RESTRICT would refuse the cascade.
	definitions := agent.NewPostgresStore(p)
	d, err := agent.NewDefinition(orgRootGRN("acme"), one.GRN(), "Field agent", agent.Values{}, registeredBy)
	require.NoError(t, err)
	keepDefinition(t, p, d)
	require.NoError(t, orgs.DeleteOrg(ctx, orgRootGRN("acme")))
	_, err = s.Get(ctx, one.GRN())
	assert.ErrorIs(t, err, operator.ErrOperatorNotFound)
	remaining, err := definitions.ListDefinitionsByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	assert.Empty(t, remaining)
	// The other org is untouched, so the cascade removed a row rather
	// than every row.
	_, err = s.Get(ctx, elsewhere.GRN())
	assert.NoError(t, err)
}
