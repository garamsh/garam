package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
	"github.com/garamsh/garam/platform/internal/policyrelay"
)

// privateAppOf registers a private app of orgSlug under slug in the
// fixture's store, as the registration #2083 builds will.
func privateAppOf(f *fixture, orgSlug, slug string) {
	a := connectorApp()
	a.GRN, a.Slug, a.Visibility = grn.New(orgSlug, app.AppType, slug), slug, app.VisibilityPrivate
	f.apps.addApp(a)
}

// TestListApps_AnswersThePublicAppsAndTheOrganizationsOwn holds
// ADR-0192 decision 3's visibility on the routes: an organization reads
// every public app and its own private ones, and another
// organization's private app is answered 404, as one that is not
// there is. Reading is `iam:app.read` on the organization.
func TestListApps_AnswersThePublicAppsAndTheOrganizationsOwn(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	privateAppOf(f, slug, "acme-deployer")
	privateAppOf(f, otherSlug, "globex-deployer")
	var asked []policydecision.Question
	f.answers(func(q policydecision.Question) (bool, error) {
		asked = append(asked, q)
		return q.Subject == userGRNOf("owner-sub") && q.Action == org.ActionAppRead, nil
	})
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("stranger-sub"), org.RoleMember)))

	rec := f.do(t, "GET", "/orgs/"+slug+"/apps", "", owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var slugs []string
	for _, a := range decodeBody[[]apigen.App](t, rec) {
		slugs = append(slugs, a.Slug)
	}
	assert.ElementsMatch(t, []string{"garam-bot-connector", "acme-deployer"}, slugs)
	require.NotEmpty(t, asked)
	assert.Equal(t, orgGRNOf(slug), asked[len(asked)-1].Resource, "decided on the organization")

	rec = f.do(t, "GET", "/orgs/"+slug+"/apps/garam-bot-connector", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	connector := decodeBody[apigen.App](t, rec)
	assert.Equal(t, "grn:root/iam/app/garam-bot-connector", connector.Grn)
	assert.Equal(t, apigen.AppInstancesMany, connector.Instances)
	assert.True(t, connector.RequestsBotHosting)

	rec = f.do(t, "GET", "/orgs/"+slug+"/apps/acme-deployer", "", owner)
	assert.Equal(t, http.StatusOK, rec.Code, "the control: its own private app")
	rec = f.do(t, "GET", "/orgs/"+slug+"/apps/globex-deployer", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "another organization's private app")

	rec = f.do(t, "GET", "/orgs/"+slug+"/apps", "", stranger)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a member not permitted to read")
}

// TestCreateInstallation_InstallsAnAppOnceUnderTheCreateAction holds
// ADR-0192 decisions 1 and 4: the installation is named by its app's
// slug and carries its organization, its app, its installer and an
// incarnation; an app is installed once in an organization; and
// installing is `iam:installation.create` on the organization, which
// the administrator is refused where the owner is not.
func TestCreateInstallation_InstallsAnAppOnceUnderTheCreateAction(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	privateAppOf(f, otherSlug, "globex-deployer")
	var asked []policydecision.Question
	f.answers(func(q policydecision.Question) (bool, error) {
		asked = append(asked, q)
		return q.Subject == userGRNOf("owner-sub") && q.Action == org.ActionInstallationCreate, nil
	})
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	path := "/orgs/" + slug + "/installations"

	rec := f.do(t, "POST", path, `{"app":"garam-bot-connector"}`, admin)
	assert.Equal(t, http.StatusForbidden, rec.Code, "an administrator not permitted to install")

	rec = f.do(t, "POST", path, `{"app":"garam-bot-connector"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	created := decodeBody[apigen.Installation](t, rec)
	assert.Equal(t, "grn:"+slug+"/iam/installation/garam-bot-connector", created.Grn)
	assert.Equal(t, "grn:root/iam/app/garam-bot-connector", created.AppGrn)
	assert.Equal(t, orgGRNOf(slug).String(), created.OrgGrn)
	assert.Equal(t, userGRNOf("owner-sub").String(), created.CreatedBy)
	assert.NotEmpty(t, created.Incarnation)
	assert.Empty(t, created.Projects, "the whole organization")
	assert.Equal(t, orgGRNOf(slug), asked[len(asked)-1].Resource, "decided on the organization")

	rec = f.do(t, "POST", path, `{"app":"garam-bot-connector"}`, owner)
	assert.Equal(t, http.StatusConflict, rec.Code, "a second installation of the app")
	rec = f.do(t, "POST", path, `{"app":"globex-deployer"}`, owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "another organization's private app")
	rec = f.do(t, "POST", path, `{"app":"absent"}`, owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestCreateInstallation_ScopesItsGrantsToProjectsTheOrganizationHolds
// holds the scope of ADR-0192 decision 4: an install naming projects
// is scoped to them, and one naming a project the organization does
// not hold, or naming none in a list, is refused and installs nothing.
func TestCreateInstallation_ScopesItsGrantsToProjectsTheOrganizationHolds(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionProjectCreate},
		permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
	)
	rec := f.do(t, "POST", "/orgs/"+slug+"/projects", `{"name":"frontend"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	project, err := grn.Parse(decodeBody[apigen.Project](t, rec).Grn)
	require.NoError(t, err)
	path := "/orgs/" + slug + "/installations"

	rec = f.do(t, "POST", path, `{"app":"garam-bot-connector","projects":["absent"]}`, owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "a project the organization does not hold")
	rec = f.do(t, "POST", path, `{"app":"garam-bot-connector","projects":[]}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a list naming none: the whole organization is the field left out")

	rec = f.do(t, "POST", path, `{"app":"garam-bot-connector","projects":["`+project.Identifier+`"]}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Equal(t, []string{project.String()}, decodeBody[apigen.Installation](t, rec).Projects)
}

// TestDeleteInstallation_TakesItsInstancesAndAReinstallIsAnotherIncarnation
// holds ADR-0192 decision 4's uninstall on the routes: it is
// `iam:installation.delete` on the installation, which the
// administrator holds; the installation's instances go with it; and
// installing the app again is another incarnation holding none of
// them.
func TestDeleteInstallation_TakesItsInstancesAndAReinstallIsAnotherIncarnation(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	installation := grn.New(slug, app.InstallationType, "garam-bot-connector")
	var asked []policydecision.Question
	allowed := []permission{
		{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		{userGRNOf("owner-sub"), org.ActionInstallationRead},
		{userGRNOf("owner-sub"), org.ActionInstanceRegister},
		{userGRNOf("owner-sub"), org.ActionInstanceRead},
		{userGRNOf("admin-sub"), org.ActionInstallationDelete},
	}
	f.answers(func(q policydecision.Question) (bool, error) {
		asked = append(asked, q)
		for _, p := range allowed {
			if p.subject == q.Subject && p.action == q.Action {
				return true, nil
			}
		}
		return false, nil
	})
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	installConnector(t, f, slug, owner)
	rec := f.do(t, "GET", connectorPath(slug), "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	first := decodeBody[apigen.Installation](t, rec).Incarnation
	assert.Equal(t, installation, asked[len(asked)-1].Resource, "a read is decided on the installation")
	rec = f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	instance := decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn

	rec = f.do(t, "DELETE", connectorPath(slug), "", owner)
	assert.Equal(t, http.StatusForbidden, rec.Code, "the owner here is not permitted to uninstall")
	rec = f.do(t, "DELETE", connectorPath(slug), "", admin)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, installation, asked[len(asked)-1].Resource, "decided on the installation")
	g, err := grn.Parse(instance)
	require.NoError(t, err)
	_, err = f.apps.GetInstance(testCtx(t), g)
	assert.ErrorIs(t, err, app.ErrInstanceNotFound, "its instance went with it")
	rec = f.do(t, "DELETE", connectorPath(slug), "", admin)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	installConnector(t, f, slug, owner)
	rec = f.do(t, "GET", connectorPath(slug), "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotEqual(t, first, decodeBody[apigen.Installation](t, rec).Incarnation, "another incarnation")
	rec = f.do(t, "GET", connectorPath(slug)+"/instances", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[[]apigen.Instance](t, rec), "holding none of the earlier one's instances")
}

// requestable stands for manifests declaring two actions an
// installation may be granted, which no manifest does today (ADR-0192
// decision 6).
var requestable = []grn.ActionInfo{
	{Name: "iam:org.read", Principals: []string{"Iam::Installation", "Iam::Instance"}, Resources: []string{"Iam::Org"}},
	{Name: "iam:project.read", Principals: []string{"Iam::Installation", "Iam::Instance"},
		Resources: []string{"Iam::Org", "Iam::Project"}},
}

// requestingApp registers acme's private app deployer, requesting both
// of requestable's actions.
func requestingApp(t *testing.T, f *fixture) (string, *http.Cookie) {
	t.Helper()
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	privateAppOf(f, slug, "deployer")
	a, err := f.apps.GetApp(testCtx(t), grn.New(slug, app.AppType, "deployer"))
	require.NoError(t, err)
	a.RequestedActions = []string{"iam:org.read", "iam:project.read"}
	f.apps.addApp(a)
	f.server.DeclareActions(requestable)
	return slug, owner
}

// TestCreateInstallation_WritesAPermitOfEachRequestedActionAsTheOwner
// holds ADR-0192 decision 4's grants on the route: the install writes
// and activates one `permit` per requested action, as the owner who
// installed, its principal the installation. It purges what names the
// installation before it grants, so a reinstall inherits nothing.
func TestCreateInstallation_WritesAPermitOfEachRequestedActionAsTheOwner(t *testing.T) {
	f := newFixture(t)
	slug, owner := requestingApp(t, f)
	f.permit(permission{userGRNOf("owner-sub"), org.ActionInstallationCreate})
	installation := grn.New(slug, app.InstallationType, "deployer")

	rec := f.do(t, "POST", "/orgs/"+slug+"/installations", `{"app":"deployer"}`, owner)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	incarnation := decodeBody[apigen.Installation](t, rec).Incarnation
	ids := []string{"install-" + incarnation + "-0", "install-" + incarnation + "-1"}
	assert.Equal(t, ids, f.policies.held())
	assert.Equal(t, ids, f.policies.active, "each is activated")
	for _, id := range ids {
		assert.Equal(t, policyrelay.KindPermit, f.policies.created[id].Kind)
		assert.Contains(t, f.policies.created[id].Text, `principal == Iam::Installation::"`+installation.String()+`"`)
		assert.Equal(t, userGRNOf("owner-sub"), f.policies.writers[id], "written as the owner")
	}
	require.Len(t, f.policies.purges, 1)
	assert.Equal(t, purge{userGRNOf("owner-sub"), orgGRNOf(slug), []grn.GRN{installation}}, f.policies.purges[0],
		"what names the installation is purged before it is granted")
}

// TestCreateInstallation_AFailedPermitUndoesTheInstall holds the
// install as one act: a `permit` the policy service refuses, here the
// second, answers the refusal and leaves neither the installation nor
// the `permit` written before it.
func TestCreateInstallation_AFailedPermitUndoesTheInstall(t *testing.T) {
	f := newFixture(t)
	slug, owner := requestingApp(t, f)
	f.permit(permission{userGRNOf("owner-sub"), org.ActionInstallationCreate})
	f.policies.failAt = 2

	rec := f.do(t, "POST", "/orgs/"+slug+"/installations", `{"app":"deployer"}`, owner)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	_, err := f.apps.GetInstallation(testCtx(t), grn.New(slug, app.InstallationType, "deployer"))
	assert.ErrorIs(t, err, app.ErrInstallationNotFound, "the installation is undone")
	assert.Empty(t, f.policies.held(), "the first permit is undone")

	f.policies.failAt = 0
	rec = f.do(t, "POST", "/orgs/"+slug+"/installations", `{"app":"deployer"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code, "the control: the same install, unrefused")
	assert.Len(t, f.policies.held(), 2)
}

// TestCreateInstallation_AnAppRequestingWhatNoInstallationMayHoldIsNotInstalled
// holds ADR-0192 decision 6 where an install reads it: against what the
// manifests declare, no action is requestable today, so an app whose
// record requests one is garam's fault and installs nothing.
func TestCreateInstallation_AnAppRequestingWhatNoInstallationMayHoldIsNotInstalled(t *testing.T) {
	f := newFixture(t)
	slug, owner := requestingApp(t, f)
	f.permit(permission{userGRNOf("owner-sub"), org.ActionInstallationCreate})
	f.server.DeclareActions(grn.Actions())

	rec := f.do(t, "POST", "/orgs/"+slug+"/installations", `{"app":"deployer"}`, owner)

	assert.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	_, err := f.apps.GetInstallation(testCtx(t), grn.New(slug, app.InstallationType, "deployer"))
	assert.ErrorIs(t, err, app.ErrInstallationNotFound)
	assert.Empty(t, f.policies.held())
	assert.Empty(t, f.policies.purges)
}

// TestDeleteInstallation_PurgesWhatNamesTheInstallationOrItsInstances
// holds the uninstall's purge (ADR-0192 decision 4, and F4 of #2113's
// review): the policies naming the installation or an instance it took
// go, whoever wrote them, for the administrator who uninstalled, and a
// policy naming neither stays.
func TestDeleteInstallation_PurgesWhatNamesTheInstallationOrItsInstances(t *testing.T) {
	f := newFixture(t)
	slug, owner := requestingApp(t, f)
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRegister},
		permission{userGRNOf("admin-sub"), org.ActionInstallationDelete},
	)
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	path := "/orgs/" + slug + "/installations/deployer"
	rec := f.do(t, "POST", "/orgs/"+slug+"/installations", `{"app":"deployer"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	rec = f.do(t, "POST", path+"/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	instance, err := grn.Parse(decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn)
	require.NoError(t, err)
	person := func(id, resource string) {
		w := policyrelay.Write{Writer: userGRNOf("owner-sub"), Org: orgGRNOf(slug), ID: id}
		require.NoError(t, f.policies.Create(testCtx(t), w, id, policyrelay.Body{Kind: policyrelay.KindGuardrail,
			Text: `forbid (principal, action, resource == ` + resource + `);`}))
	}
	person("p-instance", `Iam::Instance::"`+instance.String()+`"`)
	person("p-org", `Iam::Org::"`+orgGRNOf(slug).String()+`"`)

	rec = f.do(t, "DELETE", path, "", admin)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"p-org"}, f.policies.held(), "the install's permits and the instance's guardrail go")
	last := f.policies.purges[len(f.policies.purges)-1]
	assert.Equal(t, purge{userGRNOf("admin-sub"), orgGRNOf(slug),
		[]grn.GRN{grn.New(slug, app.InstallationType, "deployer"), instance}}, last)
}
