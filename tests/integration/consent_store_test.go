package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// TestConsentStore_RoundTrip drives the consent relation through the
// lifecycle ADR-0035 gives it: one row per pair, each half standing
// from its own side's grant, the relation standing only while both do,
// and a withdraw taking down the withdrawing side's half alone. The
// controls are the pair nobody granted and the pair two other
// organizations hold — a relation is a property of the pair, not of an
// organization.
func TestConsentStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := message.NewPostgresStore(p)
	orgs := orgpg.New(p)
	acme, globex, initech := orgRootGRN("acme"), orgRootGRN("globex"), orgRootGRN("initech")
	for _, o := range []struct {
		g    grn.GRN
		name string
	}{{acme, "acme"}, {globex, "globex"}, {initech, "initech"}} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(o.g, o.name)))
	}

	// Nothing stands where nothing was granted.
	stands, err := s.Stands(ctx, acme, globex)
	require.NoError(t, err)
	assert.False(t, stands)

	// One half does not carry the relation, whichever side grants it.
	first, err := s.Grant(ctx, acme, globex)
	require.NoError(t, err)
	assert.False(t, first.Stands())
	stands, err = s.Stands(ctx, globex, acme)
	require.NoError(t, err)
	assert.False(t, stands, "the direction asked does not matter — it is one row")

	// The second half is what stands the relation.
	second, err := s.Grant(ctx, globex, acme)
	require.NoError(t, err)
	assert.True(t, second.Stands())
	stands, err = s.Stands(ctx, acme, globex)
	require.NoError(t, err)
	assert.True(t, stands)

	// A relation between two other organizations admits nothing for
	// this pair.
	_, err = s.Grant(ctx, globex, initech)
	require.NoError(t, err)
	_, err = s.Grant(ctx, initech, globex)
	require.NoError(t, err)
	stands, err = s.Stands(ctx, acme, initech)
	require.NoError(t, err)
	assert.False(t, stands, "globex consenting with both does not join acme to initech")

	// Listing reads an org's relations from whichever side it holds.
	listed, err := s.List(ctx, globex)
	require.NoError(t, err)
	require.Len(t, listed, 2, "the pair with acme and the pair with initech")
	listed, err = s.List(ctx, acme)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.True(t, listed[0].Stands())

	// A withdraw takes the withdrawing side's half down and leaves the
	// other's standing: the relation falls, and the row lives on one
	// half.
	require.NoError(t, s.Withdraw(ctx, acme, globex))
	stands, err = s.Stands(ctx, acme, globex)
	require.NoError(t, err)
	assert.False(t, stands)
	listed, err = s.List(ctx, globex)
	require.NoError(t, err)
	require.Len(t, listed, 2, "globex's half keeps the row")
	// Withdrawing what is already down changes nothing.
	require.NoError(t, s.Withdraw(ctx, acme, globex))

	// The second withdraw empties the row, and the table holds live
	// consent only.
	require.NoError(t, s.Withdraw(ctx, globex, acme))
	assert.Zero(t, countRows(t, p,
		"SELECT count(*) FROM message_consents WHERE org_a = $1 AND org_b = $2",
		acme.String(), globex.String()), "a pair neither side stands in is gone")
	assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM message_consents"),
		"the globex-initech pair is untouched")

	// An organization that is deleted takes its halves with it.
	require.NoError(t, orgs.DeleteOrg(ctx, globex))
	assert.Zero(t, countRows(t, p, "SELECT count(*) FROM message_consents"),
		"the cascade removed the deleted org's remaining half")
}

// TestConsent_StaysARealPair covers the schema's rules for the
// relation: the pair is stored ordered, an org never consents with
// itself, a row with neither half standing is not a state the table
// holds, and neither half names an org that does not exist. A row the
// database accepted and the domain read as something else would be
// neither writable nor trustworthy.
func TestConsent_StaysARealPair(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := orgpg.New(p)
	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	ghost := orgRootGRN("ghost")
	// Each case carries the arguments its own statement has placeholders
	// for. pgx refuses a mismatched count before the row is built, so a
	// case whose arguments do not match its statement never reaches the
	// constraint it names. `constraint` is what Postgres must refuse the
	// row by, which is what tells a refusal by the named rule from a
	// refusal by anything upstream of it.
	rejected := []struct {
		name       string
		stmt       string
		args       []any
		constraint string
	}{
		{"an unordered pair", `INSERT INTO message_consents (org_a, org_b, a_consented_at)
			VALUES ($2, $1, now())`,
			[]any{acme.String(), globex.String()}, "message_consents_ordered"},
		{"an org consenting with itself", `INSERT INTO message_consents (org_a, org_b, a_consented_at)
			VALUES ($1, $1, now())`,
			[]any{acme.String()}, "message_consents_ordered"},
		{"neither half standing", `INSERT INTO message_consents (org_a, org_b)
			VALUES ($1, $2)`,
			[]any{acme.String(), globex.String()}, "message_consents_a_half_stands"},
		{"an org that does not exist", `INSERT INTO message_consents (org_a, org_b, a_consented_at)
			VALUES ($1, $2, now())`,
			[]any{acme.String(), ghost.String()}, "message_consents_org_b_fkey"},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Exec(ctx, tt.stmt, tt.args...)
			var refused *pgconn.PgError
			require.ErrorAs(t, err, &refused, "the statement never reached Postgres")
			assert.Equal(t, tt.constraint, refused.ConstraintName)
		})
	}

	// The ordered pair with one half standing is admitted, so the
	// constraints reject a row rather than every row.
	_, err := p.Exec(ctx, `INSERT INTO message_consents (org_a, org_b, b_consented_at)
		VALUES ($1, $2, now())`, acme.String(), globex.String())
	require.NoError(t, err)
}

// TestConsentStore_ClassifiesAGrantTowardAnOrgThatIsNotThere covers the
// foreign key 000015 carries: a consent row names two organizations
// that exist. What this pins is the store's own answer — a grant toward
// an organization that is not there is not found, rather than the
// driver error's default kind.
//
// The pair that stands is the control: the same call between two real
// organizations grants, so the refusal names the absent organization
// and not the call.
func TestConsentStore_ClassifiesAGrantTowardAnOrgThatIsNotThere(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	truncate(t, p)
	orgs := orgpg.New(p)
	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	consents := message.NewPostgresStore(p)
	_, err := consents.Grant(ctx, acme, globex)
	require.NoError(t, err, "a grant between two organizations that exist stands")

	_, err = consents.Grant(ctx, acme, orgRootGRN("ghost"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound),
		"granting toward an organization that does not exist names the missing row")
}
