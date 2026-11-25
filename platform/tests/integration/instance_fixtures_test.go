package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	apppg "github.com/garamsh/garam/platform/internal/app/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
)

// connectorSlug is the app migration 000088 seeds (ADR-0192 decision
// 11), the one every connected cluster's instance is of.
const connectorSlug = "garam-bot-connector"

// connectorInstallation creates the organization slug and installs
// garam-bot-connector in it.
func connectorInstallation(t *testing.T, p *pgxpool.Pool, slug string) *app.Installation {
	t.Helper()
	ctx := context.Background()
	s := apppg.New(p)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN(slug), slug)))
	g, err := app.AppGRN(grn.RootOwner, connectorSlug)
	require.NoError(t, err)
	connector, err := s.GetApp(ctx, g)
	require.NoError(t, err)
	i, err := app.NewInstallation(orgRootGRN(slug), connector, scopedGRN(slug, "user", "ada"), app.Scope{})
	require.NoError(t, err)
	created, err := s.CreateInstallation(ctx, i)
	require.NoError(t, err)
	return created
}

// declarationRecords installs garam-bot-connector in a new acme and
// records n instances of it.
func declarationRecords(t *testing.T, p *pgxpool.Pool, n int) []grn.GRN {
	t.Helper()
	installation := connectorInstallation(t, p, "acme")
	return instancesOf(t, p, installation, n)
}

// instancesOf records n instances of installation.
func instancesOf(t *testing.T, p *pgxpool.Pool, installation *app.Installation, n int) []grn.GRN {
	t.Helper()
	s := apppg.New(p)
	out := make([]grn.GRN, 0, n)
	for range n {
		i, err := app.NewInstance(installation, installation.CreatedBy, app.DetailsPatch{})
		require.NoError(t, err)
		_, err = s.CreateInstance(context.Background(), i)
		require.NoError(t, err)
		out = append(out, i.GRN)
	}
	return out
}
