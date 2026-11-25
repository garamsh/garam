package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
)

// TestInstances_AdmitsWhatTheSettingHolds holds ADR-0192 decision 2's
// instance setting: `many` admits any number, `one` admits the first
// alone, and `none` admits none. A setting no app holds is garam's
// failure, not the caller's.
func TestInstances_AdmitsWhatTheSettingHolds(t *testing.T) {
	for _, c := range []struct {
		setting app.Instances
		held    int
		want    error
	}{
		{app.InstancesMany, 0, nil},
		{app.InstancesMany, 7, nil},
		{app.InstancesOne, 0, nil},
		{app.InstancesOne, 1, app.ErrInstanceLimit},
		{app.InstancesNone, 0, app.ErrNoInstances},
	} {
		err := c.setting.Admit(c.held)
		if c.want == nil {
			assert.NoError(t, err, "%s holding %d", c.setting, c.held)
			continue
		}
		assert.ErrorIs(t, err, c.want, "%s holding %d", c.setting, c.held)
	}
	assert.True(t, errorx.IsKind(app.Instances("several").Admit(0), errorx.KindInternal))
}

// TestNewInstallation_InstallsAPublicAppAnywhereAndAPrivateOneAtHome
// holds ADR-0192 decision 3's visibility where an installation is
// made: a public app installs in any organization, a private one in its
// owner's alone, and the refusal is the one an app not there answers.
func TestNewInstallation_InstallsAPublicAppAnywhereAndAPrivateOneAtHome(t *testing.T) {
	acme, globex := grn.Org("acme"), grn.Org("globex")
	ada := grn.New("acme", "user", "ada")
	private := &app.App{GRN: grn.New("acme", app.AppType, "deployer"), Slug: "deployer", Visibility: app.VisibilityPrivate}
	public := &app.App{GRN: grn.New(grn.RootOwner, app.AppType, "garam-bot-connector"), Slug: "garam-bot-connector",
		Visibility: app.VisibilityPublic}

	i, err := app.NewInstallation(acme, private, ada, app.Scope{})
	require.NoError(t, err)
	assert.Equal(t, grn.New("acme", app.InstallationType, "deployer"), i.GRN, "named by its app's slug")
	assert.Equal(t, private.GRN, i.App)
	_, err = app.NewInstallation(globex, private, grn.New("globex", "user", "ada"), app.Scope{})
	assert.ErrorIs(t, err, app.ErrAppNotFound, "another organization's private app")
	_, err = app.NewInstallation(globex, public, grn.New("globex", "user", "ada"), app.Scope{})
	assert.NoError(t, err, "the control: a public app")
}

// TestProjectScope_NamesProjectsOfItsOrganizationOnce holds the scope
// an install names: projects of its own organization, each once, and
// at least one, the whole organization being the zero Scope.
func TestProjectScope_NamesProjectsOfItsOrganizationOnce(t *testing.T) {
	acme := grn.Org("acme")
	frontend := grn.New("acme", "project", "frontend")

	s, err := app.ProjectScope(acme, []grn.GRN{frontend, frontend})
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{frontend}, s.Projects())
	assert.False(t, s.IsOrganization())
	assert.True(t, app.Scope{}.IsOrganization())

	for name, projects := range map[string][]grn.GRN{
		"none":                    {},
		"another organization's":  {grn.New("globex", "project", "frontend")},
		"a group, not a project":  {grn.New("acme", "group", "frontend")},
		"one of each, one wrong ": {frontend, grn.New("globex", "project", "x")},
	} {
		_, err := app.ProjectScope(acme, projects)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%s: %v", name, err)
	}
}
