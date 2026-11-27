package app_test

import (
	"slices"
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

// TestRequestable_NoAppRequestsAnActionOfTheFloor holds ADR-0193: an
// action of the floor is refused in a request even where its manifest
// lists the installation and the instance among its principals, so the
// refusal is the floor's and not the principals'. iam:bot.host, which
// an installation holds by its owner's grant, is among them. The
// control is a non-floor action declared the same way.
func TestRequestable_NoAppRequestsAnActionOfTheFloor(t *testing.T) {
	assert.Contains(t, app.Floor, "iam:bot.host")
	principals := []string{"Iam::User", "Iam::Installation", "Iam::Instance"}
	for _, action := range append(app.Floor, "iam:org.read") {
		asIfRequestable := []grn.ActionInfo{{Name: action, Principals: principals, Resources: []string{"Iam::Org"}}}
		err := app.Requestable(action, asIfRequestable)
		if action == "iam:org.read" {
			assert.NoError(t, err, "the control")
			continue
		}
		assert.ErrorIs(t, err, app.ErrUnrequestable, action)
		assert.ErrorContains(t, err, "of the floor", action)
	}
}

// reviewedRequestable is every action a manifest lets an installation
// and its instances hold that a review found outside the floor's
// property, so an app may request it. An action is added here, or to
// app.Floor, in the pull request that first declares it that way.
var reviewedRequestable = []string{}

// unreviewed answers the actions of declared that list both the
// installation and the instance among their principals and stand in
// neither app.Floor nor reviewedRequestable.
func unreviewed(declared []grn.ActionInfo) []string {
	var out []string
	for _, a := range declared {
		if slices.Contains(a.Principals, "Iam::Installation") && slices.Contains(a.Principals, "Iam::Instance") &&
			!slices.Contains(app.Floor, a.Name) && !slices.Contains(reviewedRequestable, a.Name) {
			out = append(out, a.Name)
		}
	}
	return out
}

// TestFloor_EveryActionAnAppCouldRequestWasReviewed holds ADR-0193's
// review duty as a test: no action the manifests declare for an
// installation and its instances stands outside both the floor and
// the reviewed list. The control is a declaration of that shape that
// neither names, which is reported.
func TestFloor_EveryActionAnAppCouldRequestWasReviewed(t *testing.T) {
	assert.Empty(t, unreviewed(grn.Actions()), "add each to app.Floor or to reviewedRequestable")
	both := []string{"Iam::User", "Iam::Installation", "Iam::Instance"}
	assert.Equal(t, []string{"iam:org.frobnicate"}, unreviewed([]grn.ActionInfo{
		{Name: "iam:org.frobnicate", Principals: both},
		{Name: "iam:org.delete", Principals: both},
		{Name: "iam:org.read", Principals: []string{"Iam::User", "Iam::Installation"}},
	}), "the control")
}

// TestFloor_HoldsTheAgentLifecycleActions holds ADR-0193's extension
// of the floor: creating, enrolling, recovering, replacing and
// activating an agent each make or change a principal or its
// credentials.
func TestFloor_HoldsTheAgentLifecycleActions(t *testing.T) {
	for _, action := range []string{"iam:agent.create", "iam:agent.enroll", "iam:agent.recover",
		"iam:agent.replace", "iam:agent.activate"} {
		assert.Contains(t, app.Floor, action)
	}
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
