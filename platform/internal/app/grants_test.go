package app_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
)

// declared stands for the manifests. No action they declare today
// lists the installation and the instance among its principals, so
// the requestable one is this test's own.
var declared = []grn.ActionInfo{
	{Name: "iam:org.read", Principals: []string{"Iam::User", "Iam::Agent", "Iam::Installation", "Iam::Instance"},
		Resources: []string{"Iam::Org"}},
	{Name: "iam:project.read", Principals: []string{"Iam::User", "Iam::Installation", "Iam::Instance"},
		Resources: []string{"Iam::Org", "Iam::Project"}},
	{Name: "iam:org.delete", Principals: []string{"Iam::User", "Iam::Installation"}, Resources: []string{"Iam::Org"}},
	{Name: "iam:org.update", Principals: []string{"Iam::User", "Iam::Instance"}, Resources: []string{"Iam::Org"}},
}

// TestRequestable_AnActionAppliesToTheInstallationAndItsInstances holds
// ADR-0192 decision 6's requestable action: one whose manifest lists
// both the installation and the instance among its principals. One
// listing either alone, and one no manifest declares, are refused as
// garam's fault, since garam writes the record that requests them.
func TestRequestable_AnActionAppliesToTheInstallationAndItsInstances(t *testing.T) {
	assert.NoError(t, app.Requestable("iam:org.read", declared))
	for _, action := range []string{"iam:org.delete", "iam:org.update", "iam:org.leave", "iam:org.*"} {
		err := app.Requestable(action, declared)
		assert.ErrorIs(t, err, app.ErrUnrequestable, action)
		assert.True(t, errorx.IsKind(err, errorx.KindInternal), action)
	}
	assert.Error(t, app.Requestable("iam:org.read", grn.Actions()),
		"no action the manifests declare today is requestable")
}

// TestGrants_OnePermitPerActionAndScopeNamedByTheIncarnation holds
// ADR-0192 decision 4's `permit`s: one per requested action and per
// entity of the scope, the installation its principal and its resource
// in the organization or in each project, under identifiers the
// incarnation names. A reinstall's names others, and an app requesting
// what no installation may be granted writes none.
func TestGrants_OnePermitPerActionAndScopeNamedByTheIncarnation(t *testing.T) {
	acme := grn.Org("acme")
	deployer := &app.App{GRN: grn.New("acme", app.AppType, "deployer"), Slug: "deployer",
		Visibility: app.VisibilityPrivate, RequestedActions: []string{"iam:org.read", "iam:project.read"}}
	frontend, backend := grn.New("acme", "project", "frontend"), grn.New("acme", "project", "backend")
	installation, err := app.NewInstallation(acme, deployer, grn.New("acme", "user", "ada"), app.Scope{})
	require.NoError(t, err)
	installation.Incarnation = "0d1e"

	grants, err := installation.Grants(deployer, declared)
	require.NoError(t, err)
	assert.Equal(t, []app.Grant{
		{ID: "install-0d1e-0", Name: "deployer iam:org.read", Text: `permit (principal in Iam::Installation::"grn:acme/iam/installation/deployer", ` +
			`action == Iam::Action::"org.read", resource in Iam::Org::"grn:root/iam/org/acme");`},
		{ID: "install-0d1e-1", Name: "deployer iam:project.read", Text: `permit (principal in Iam::Installation::"grn:acme/iam/installation/deployer", ` +
			`action == Iam::Action::"project.read", resource in Iam::Org::"grn:root/iam/org/acme");`},
	}, grants)

	installation.Scope, err = app.ProjectScope(acme, []grn.GRN{frontend, backend})
	require.NoError(t, err)
	installation.Incarnation = "7a2b"
	grants, err = installation.Grants(deployer, declared)
	require.NoError(t, err)
	require.Len(t, grants, 4, "two actions over two projects")
	assert.Equal(t, "install-7a2b-3", grants[3].ID, "another incarnation names other policies")
	assert.Contains(t, grants[0].Text, `resource in Iam::Project::"grn:acme/iam/project/frontend"`)
	assert.Contains(t, grants[3].Text, `action == Iam::Action::"project.read", `+
		`resource in Iam::Project::"grn:acme/iam/project/backend"`)
	for _, g := range grants {
		assert.NotContains(t, g.Text, "Iam::Org::", "a project scope names nothing outside its projects")
		assert.Contains(t, g.Text, `permit (principal in Iam::Installation::"grn:acme/iam/installation/deployer", `,
			"each instance, in the installation, holds the grant")
	}

	deployer.RequestedActions = []string{"iam:org.read", "iam:org.delete"}
	_, err = installation.Grants(deployer, declared)
	assert.ErrorIs(t, err, app.ErrUnrequestable)
	_, err = installation.Grants(&app.App{GRN: grn.New("acme", app.AppType, "other")}, declared)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), "another app's grants")
}
