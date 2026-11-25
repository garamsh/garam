package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	apppg "github.com/garamsh/garam/platform/internal/app/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
)

// TestAppStore_TheSeededConnectorIsPublicAndReachesTheConnectionRoutes
// holds ADR-0192 decision 11's seed: garam-bot-connector is the
// platform's, any organization sees it, and its machine operations
// are the two cluster-connection routes a connector calls beyond the
// app system's own.
func TestAppStore_TheSeededConnectorIsPublicAndReachesTheConnectionRoutes(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := apppg.New(p)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	g, err := app.AppGRN(grn.RootOwner, connectorSlug)
	require.NoError(t, err)
	connector, err := s.GetApp(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, app.VisibilityPublic, connector.Visibility)
	assert.Equal(t, app.InstancesMany, connector.Instances)
	assert.Equal(t, app.DeliveryPull, connector.Delivery)
	assert.True(t, connector.RequestsBotHosting)
	assert.Empty(t, connector.RequestedActions)
	assert.Equal(t, []string{
		"GET /cluster-connection/v1alpha1/clusters/self/declarations",
		"POST /cluster-connection/v1alpha1/clusters/self/reports",
	}, connector.MachineOperations)
	assert.True(t, connector.InstallableIn(orgRootGRN("acme")))

	listed, err := s.ListApps(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, g, listed[0].GRN)
}

// TestAppStore_AnOrganizationSeesItsOwnPrivateAppAndNoOtherOrganizations
// holds ADR-0192 decision 1's visibility: a private app is listed to
// the organization owning it and to no other.
func TestAppStore_AnOrganizationSeesItsOwnPrivateAppAndNoOtherOrganizations(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := apppg.New(p)
	orgs := orgpg.New(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))
	privateApp(t, p, "acme", "acme-deployer", app.InstancesOne)

	slugs := func(orgSlug string) []string {
		listed, err := s.ListApps(ctx, orgRootGRN(orgSlug))
		require.NoError(t, err)
		var out []string
		for _, a := range listed {
			out = append(out, a.Slug)
		}
		return out
	}
	assert.ElementsMatch(t, []string{connectorSlug, "acme-deployer"}, slugs("acme"))
	assert.Equal(t, []string{connectorSlug}, slugs("globex"), "another organization's private app is not listed")

	// The organization's deletion takes its app.
	require.NoError(t, orgs.DeleteOrg(ctx, orgRootGRN("acme")))
	_, err := s.GetApp(ctx, grn.New("acme", app.AppType, "acme-deployer"))
	assert.ErrorIs(t, err, app.ErrAppNotFound)
}

// TestAppStore_AnInstallationIsOneAndTakesItsInstancesOut holds
// ADR-0192 decisions 2 and 3 at the store: an app is installed once
// in an organization, an instance's record round-trips, and the
// installation's deletion takes its instances, with another
// installation's the control.
func TestAppStore_AnInstallationIsOneAndTakesItsInstancesOut(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := apppg.New(p)
	installation := connectorInstallation(t, p, "acme")
	elsewhere := connectorInstallation(t, p, "globex")
	assert.NotEmpty(t, installation.Incarnation)
	assert.True(t, installation.Scope.IsOrganization())

	connector, err := s.GetApp(ctx, installation.App)
	require.NoError(t, err)
	again, err := app.NewInstallation(orgRootGRN("acme"), connector, scopedGRN("acme", "user", "ada"), app.Scope{})
	require.NoError(t, err)
	_, err = s.CreateInstallation(ctx, again)
	assert.ErrorIs(t, err, app.ErrInstallationExists)

	name, description, kind := "prod", "the main one", "eks"
	details, err := app.NewDetailsPatch(&name, &description, &kind)
	require.NoError(t, err)
	one, err := app.NewInstance(installation, scopedGRN("acme", "user", "ada"), details)
	require.NoError(t, err)
	created, err := s.CreateInstance(ctx, one)
	require.NoError(t, err)
	stored, err := s.GetInstance(ctx, one.GRN)
	require.NoError(t, err)
	assert.Equal(t, created, stored)
	assert.Equal(t, installation.GRN, stored.Installation)
	assert.Equal(t, orgRootGRN("acme"), stored.Organization)
	assert.Equal(t, "prod", stored.Name)
	assert.Equal(t, "the main one", stored.Description)
	assert.Equal(t, "eks", stored.Kind)

	renamed := "renamed"
	patch, err := app.NewDetailsPatch(&renamed, nil, nil)
	require.NoError(t, err)
	updated, err := s.UpdateInstance(ctx, one.GRN, patch)
	require.NoError(t, err)
	assert.Equal(t, "renamed", updated.Name)
	assert.Equal(t, "eks", updated.Kind, "a kind left out keeps its value")

	theirs := instancesOf(t, p, elsewhere, 1)[0]
	listed, err := s.ListInstances(ctx, installation.GRN)
	require.NoError(t, err)
	require.Len(t, listed, 1, "this installation's instances and no other's")

	removed, err := s.DeleteInstallation(ctx, installation.GRN)
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{one.GRN}, removed, "it answers the instance it took, and not another installation's")
	_, err = s.GetInstance(ctx, one.GRN)
	assert.ErrorIs(t, err, app.ErrInstanceNotFound, "the installation took its instance")
	_, err = s.DeleteInstallation(ctx, installation.GRN)
	assert.ErrorIs(t, err, app.ErrInstallationNotFound)
	_, err = s.GetInstance(ctx, theirs)
	assert.NoError(t, err, "the control: another installation's instance")

	// Installed again, the app's installation is another incarnation.
	reinstalled, err := s.CreateInstallation(ctx, again)
	require.NoError(t, err)
	assert.NotEqual(t, installation.Incarnation, reinstalled.Incarnation)
}

// TestAppStore_AnAppHoldsTheInstancesItsSettingAdmits holds ADR-0192
// decision 4 at the store: an app set to one instance holds one, and
// a second is refused until the first is deleted.
func TestAppStore_AnAppHoldsTheInstancesItsSettingAdmits(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := apppg.New(p)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	single := privateApp(t, p, "acme", "acme-deployer", app.InstancesOne)
	none := privateApp(t, p, "acme", "acme-notifier", app.InstancesNone)
	install := func(a *app.App) *app.Installation {
		i, err := app.NewInstallation(orgRootGRN("acme"), a, scopedGRN("acme", "user", "ada"), app.Scope{})
		require.NoError(t, err)
		created, err := s.CreateInstallation(ctx, i)
		require.NoError(t, err)
		return created
	}
	singleInstallation := install(single)

	first := instancesOf(t, p, singleInstallation, 1)[0]
	second, err := app.NewInstance(singleInstallation, scopedGRN("acme", "user", "ada"), app.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.CreateInstance(ctx, second)
	assert.ErrorIs(t, err, app.ErrInstanceLimit)
	require.NoError(t, s.DeleteInstance(ctx, first))
	_, err = s.CreateInstance(ctx, second)
	assert.NoError(t, err, "the control: the one it held is gone")

	noneInstallation := install(none)
	refused, err := app.NewInstance(noneInstallation, scopedGRN("acme", "user", "ada"), app.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.CreateInstance(ctx, refused)
	assert.ErrorIs(t, err, app.ErrNoInstances)
}

// TestAppStore_ACreationWaitingOnTheInstallationCountsTheInstanceCommittedMeanwhile holds the
// instance setting under concurrency, on an app set to `one`. A
// creation holding the installation's row commits its instance while
// the racers wait on that row, and each racer then counts it and is
// refused ErrInstanceLimit: a count read before the wait would admit
// them. A creation after it is deleted, the control, is admitted.
func TestAppStore_ACreationWaitingOnTheInstallationCountsTheInstanceCommittedMeanwhile(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := apppg.New(p)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	single := privateApp(t, p, "acme", "acme-deployer", app.InstancesOne)
	i, err := app.NewInstallation(orgRootGRN("acme"), single, scopedGRN("acme", "user", "ada"), app.Scope{})
	require.NoError(t, err)
	installation, err := s.CreateInstallation(ctx, i)
	require.NoError(t, err)

	// The first creation holds the row, as CreateInstance does; the
	// racers start while it holds it, and it writes and commits after
	// they wait.
	held, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = held.Rollback(ctx) }()
	_, err = held.Exec(ctx, `SELECT 1 FROM installations WHERE grn = $1 FOR NO KEY UPDATE`, installation.GRN.String())
	require.NoError(t, err)

	const racers = 8
	results := make(chan error, racers)
	var wg sync.WaitGroup
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			one, err := app.NewInstance(installation, scopedGRN("acme", "user", "ada"), app.DetailsPatch{})
			if err == nil {
				_, err = s.CreateInstance(ctx, one)
			}
			results <- err
		}()
	}
	require.Eventually(t, func() bool {
		return countRows(t, p, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock'`) >= racers
	}, 10*time.Second, 20*time.Millisecond, "every racer waits on the held row")
	first, err := app.NewInstance(installation, scopedGRN("acme", "user", "ada"), app.DetailsPatch{})
	require.NoError(t, err)
	_, err = held.Exec(ctx, `INSERT INTO instances (grn, org_grn, installation_grn, created_by, created_at, name)
		VALUES ($1, $2, $3, $4, now(), $5)`, first.GRN.String(), first.Organization.String(),
		installation.GRN.String(), first.CreatedBy.String(), first.Name)
	require.NoError(t, err)
	require.NoError(t, held.Commit(ctx))
	wg.Wait()
	close(results)

	var admitted, refused int
	for err := range results {
		switch {
		case err == nil:
			admitted++
		case errors.Is(err, app.ErrInstanceLimit):
			refused++
		default:
			t.Fatal(err)
		}
	}
	assert.Equal(t, 0, admitted, "no racer is admitted beside the instance committed while it waited")
	assert.Equal(t, racers, refused)
	assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM instances WHERE installation_grn = $1`, installation.GRN.String()))

	require.NoError(t, s.DeleteInstance(ctx, first.GRN))
	again, err := app.NewInstance(installation, scopedGRN("acme", "user", "ada"), app.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.CreateInstance(ctx, again)
	assert.NoError(t, err, "the control: the one it held is gone")
}

// TestAppStore_RefusesAnInstanceNamingAnotherOrganization holds the
// table's ownership constraint, the copy no call path routes around:
// an instance's GRN of one organization stored under another is
// refused, and the same record under its own organization is the
// control.
func TestAppStore_RefusesAnInstanceNamingAnotherOrganization(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := apppg.New(p)
	installation := connectorInstallation(t, p, "acme")
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))

	i, err := app.NewInstance(installation, scopedGRN("acme", "user", "ada"), app.DetailsPatch{})
	require.NoError(t, err)
	i.Organization = orgRootGRN("globex")
	_, err = s.CreateInstance(ctx, i)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)

	i.Organization = orgRootGRN("acme")
	_, err = s.CreateInstance(ctx, i)
	require.NoError(t, err)

	operatorShaped := *i
	operatorShaped.GRN = grn.New("acme", "operator", i.GRN.Identifier)
	_, err = s.CreateInstance(ctx, &operatorShaped)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)
}

// privateApp registers a private app of orgSlug under slug, as the
// registration #2083 builds will, and answers it as the store reads
// it.
func privateApp(t *testing.T, p *pgxpool.Pool, orgSlug, slug string, instances app.Instances) *app.App {
	t.Helper()
	ctx := context.Background()
	g := grn.New(orgSlug, app.AppType, slug)
	_, err := p.Exec(ctx, `INSERT INTO apps (grn, slug, org_grn, name, visibility, instances, delivery)
		VALUES ($1, $2, $3, $2, 'private', $4, 'pull')`, g.String(), slug, orgRootGRN(orgSlug).String(), string(instances))
	require.NoError(t, err)
	a, err := apppg.New(p).GetApp(ctx, g)
	require.NoError(t, err)
	return a
}
