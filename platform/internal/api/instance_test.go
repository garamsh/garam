package api_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// mintedInstance matches a GRN of slug's whose identifier garam minted:
// sixteen lowercase hex digits under the iam instance type.
func mintedInstance(slug string) *regexp.Regexp {
	return regexp.MustCompile(`^grn:` + regexp.QuoteMeta(slug) + `/iam/instance/[0-9a-f]{16}$`)
}

// connectorPath is the path of slug's installation of
// garam-bot-connector.
func connectorPath(slug string) string {
	return "/orgs/" + slug + "/installations/garam-bot-connector"
}

// installConnector installs garam-bot-connector in slug as the owner
// session, which the test's permissions let install.
func installConnector(t *testing.T, f *fixture, slug string, owner *http.Cookie) {
	t.Helper()
	rec := f.do(t, "POST", "/orgs/"+slug+"/installations", `{"app":"garam-bot-connector"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

// TestCreateInstance_MintsTheIdentifierUnderTheRegisterActionOnTheInstallation
// holds ADR-0192 decisions 1 and 9: garam mints the identifier, the
// record names its installation, and creating is decided as
// `iam:instance.register` on the installation, not on the organization.
//
// The stranger is a member permitted to read and not to register, and
// the outsider is permitted to register in another organization; each
// isolates one half of the check, and the admin is the control.
func TestCreateInstance_MintsTheIdentifierUnderTheRegisterActionOnTheInstallation(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	installation := grn.New(slug, app.InstallationType, "garam-bot-connector")
	var asked []policydecision.Question
	allowed := []permission{
		{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		{userGRNOf("other-owner-sub"), org.ActionInstallationCreate},
		{userGRNOf("admin-sub"), org.ActionInstanceRegister},
		{userGRNOf("stranger-sub"), org.ActionInstanceRead},
		{userGRNOf("outsider-sub"), org.ActionInstanceRegister},
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
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("stranger-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(otherSlug), userGRNOf("outsider-sub"), org.RoleMember)))

	rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, admin)
	assert.Equal(t, http.StatusNotFound, rec.Code, "no instance of an app the organization has not installed")
	installConnector(t, f, slug, owner)
	installConnector(t, f, otherSlug, otherOwner)

	asked = nil
	rec = f.do(t, "POST", connectorPath(slug)+"/instances", `{"name":" EKS prod ","description":"the main one","kind":" cluster "}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.CreatedInstance](t, rec).Instance
	assert.Regexp(t, mintedInstance(slug), created.Grn)
	assert.Equal(t, installation.String(), created.InstallationGrn)
	assert.Equal(t, "EKS prod", created.Name)
	assert.Equal(t, "the main one", created.Description)
	assert.Equal(t, "cluster", created.Kind)
	assert.Equal(t, orgGRNOf(slug).String(), created.OrgGrn)
	assert.Equal(t, userGRNOf("admin-sub").String(), created.CreatedBy)
	require.Len(t, asked, 1)
	assert.Equal(t, installation, asked[0].Resource, "decided on the installation")

	// Two creations mint two identifiers.
	rec = f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	second := decodeBody[apigen.CreatedInstance](t, rec).Instance
	assert.NotEqual(t, created.Grn, second.Grn)
	assert.Regexp(t, mintedInstance(slug), second.Grn)

	rec = f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, stranger)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "GET", connectorPath(slug)+"/instances", "", stranger)
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "POST", connectorPath(otherSlug)+"/instances", `{}`, outsider)
	assert.Equal(t, http.StatusCreated, rec.Code)

	rec = f.do(t, "POST", connectorPath(slug)+"/instances", `{}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestCreateInstance_NamesItByItsIdentifierWhenGivenNone holds that a
// name left out is the identifier, as an operator's is, and a kind left
// out is empty.
func TestCreateInstance_NamesItByItsIdentifierWhenGivenNone(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRegister})
	installConnector(t, f, slug, owner)

	rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.CreatedInstance](t, rec).Instance
	assert.Equal(t, created.Grn[len(created.Grn)-16:], created.Name)
	assert.Empty(t, created.Description)
	assert.Empty(t, created.Kind)
}

// TestCreateInstance_RefusesDetailsItCannotStore covers the details'
// bounds. The accepted creations are the control.
func TestCreateInstance_RefusesDetailsItCannotStore(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRegister})
	installConnector(t, f, slug, owner)

	long := strings.Repeat("a", 101)
	for name, body := range map[string]string{
		"an empty name":            `{"name":"   "}`,
		"a name over 100":          `{"name":"` + long + `"}`,
		"a NUL in the name":        `{"name":"a\u0000b"}`,
		"a body not an object":     `[]`,
		"a name that is a list":    `{"name":[]}`,
		"a description over 1000":  `{"description":"` + strings.Repeat("d", 1001) + `"}`,
		"a NUL in the description": `{"description":"a\u0000b"}`,
		"a kind over 64":           `{"kind":"` + strings.Repeat("k", 65) + `"}`,
		"a NUL in the kind":        `{"kind":"a\u0000b"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(t, "POST", connectorPath(slug)+"/instances", body, owner)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
	rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{"name":"`+long[:100]+`"}`, owner)
	assert.Equal(t, http.StatusCreated, rec.Code)
	rec = f.do(t, "POST", connectorPath(slug)+"/instances", `{"description":"`+strings.Repeat("d", 1000)+`"}`, owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the control: a description of 1000")
	rec = f.do(t, "POST", connectorPath(slug)+"/instances", `{"kind":"`+strings.Repeat("k", 64)+`"}`, owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the control: a kind of 64")
	assert.Len(t, f.instancesOf(t, slug), 3, "no refused creation was stored")
}

// TestInstance_EachRouteOnARecordTakesItsOwnAction holds ADR-0192
// decision 9 route by route: each is decided on its own action, and a
// caller holding every other instance action is refused it.
func TestInstance_EachRouteOnARecordTakesItsOwnAction(t *testing.T) {
	all := []string{org.ActionInstanceRead, org.ActionInstanceRegister, org.ActionInstanceUpdate, org.ActionInstanceReissue,
		org.ActionInstanceDelete, org.ActionBotPlace}
	tests := []struct {
		name    string
		actions []string
		method  string
		path    string
		body    string
		want    int
	}{
		{"get", []string{org.ActionInstanceRead}, "GET", "", "", http.StatusOK},
		{"list", []string{org.ActionInstanceRead}, "GET", "list", "", http.StatusOK},
		{"update", []string{org.ActionInstanceUpdate}, "PATCH", "", `{"name":"renamed"}`, http.StatusOK},
		{"delete", []string{org.ActionInstanceDelete}, "DELETE", "", "", http.StatusNoContent},
		{"reissue", []string{org.ActionInstanceReissue}, "POST", "/registration-token", "", http.StatusCreated},
		{"list keys", []string{org.ActionInstanceRead}, "GET", "/keys", "", http.StatusOK},
		{"revoke a key", []string{org.ActionInstanceReissue}, "DELETE", "/keys/held", "", http.StatusNoContent},
		{"list declarations", []string{org.ActionInstanceRead}, "GET", "/declarations", "", http.StatusOK},
		{"read a declaration", []string{org.ActionInstanceRead}, "GET", "/declarations/placed", "", http.StatusOK},
		{"declare", []string{org.ActionInstanceRead, org.ActionBotPlace}, "POST", "/declarations",
			declarationWrite("other"), http.StatusCreated},
		{"declare under an id", []string{org.ActionInstanceRead, org.ActionBotPlace}, "PUT", "/declarations/placed",
			declarationWrite("reviewer"), http.StatusOK},
		{"tombstone", []string{org.ActionInstanceRead, org.ActionBotPlace}, "DELETE", "/declarations/placed", "",
			http.StatusNoContent},
		{"read the report", []string{org.ActionInstanceRead}, "GET", "/report", "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			owner := f.sessionCookieFor(t, "owner-sub", "Owner")
			slug := createOrg(t, f, owner, "acme")
			// The lacking caller holds every action but the route's
			// last, which is the one the route adds to reading it.
			missing := tt.actions[len(tt.actions)-1]
			var perms []permission
			for _, a := range all {
				if a != missing {
					perms = append(perms, permission{userGRNOf("lacking-sub"), a})
				}
			}
			for _, a := range tt.actions {
				perms = append(perms, permission{userGRNOf("holder-sub"), a})
			}
			f.permit(append(perms,
				permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
				permission{userGRNOf("owner-sub"), org.ActionInstanceRegister}, hosting(slug))...)
			lacking := f.sessionCookieFor(t, "lacking-sub", "Lacking")
			holder := f.sessionCookieFor(t, "holder-sub", "Holder")
			require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("lacking-sub"), org.RoleAdmin)))
			require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("holder-sub"), org.RoleMember)))
			installConnector(t, f, slug, owner)

			rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{"name":"prod"}`, owner)
			require.Equal(t, http.StatusCreated, rec.Code)
			path := connectorPath(slug) + "/instances"
			if tt.path != "list" {
				g := decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn
				f.instanceKeys.place(g, app.Key{KID: "held", State: app.KeyStateActive})
				f.declare(t, g, "placed", "reviewer")
				path += "/" + g[len(g)-16:] + tt.path
			}

			rec = f.do(t, tt.method, path, tt.body, lacking)
			assert.Equal(t, http.StatusForbidden, rec.Code)
			rec = f.do(t, tt.method, path, tt.body, holder)
			assert.Equal(t, tt.want, rec.Code)
		})
	}
}

// TestInstance_UpdateChangesOnlyTheDetailsGivenAndDeleteRemovesIt reads
// each write back.
func TestInstance_UpdateChangesOnlyTheDetailsGivenAndDeleteRemovesIt(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRegister},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRead},
		permission{userGRNOf("owner-sub"), org.ActionInstanceUpdate},
		permission{userGRNOf("owner-sub"), org.ActionInstanceDelete},
	)
	installConnector(t, f, slug, owner)
	rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{"name":"prod","description":"first","kind":"cluster"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.CreatedInstance](t, rec).Instance
	path := connectorPath(slug) + "/instances/" + created.Grn[len(created.Grn)-16:]

	rec = f.do(t, "PATCH", path, `{"description":"","kind":"pc"}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	updated := decodeBody[apigen.Instance](t, rec)
	assert.Equal(t, "prod", updated.Name, "a name left out keeps its value")
	assert.Empty(t, updated.Description, "an empty description clears it")
	assert.Equal(t, "pc", updated.Kind)
	assert.Equal(t, created.Grn, updated.Grn)

	rec = f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, updated, decodeBody[apigen.Instance](t, rec))

	rec = f.do(t, "DELETE", path, "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", path, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "DELETE", path, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "GET", connectorPath(slug)+"/instances/NOT-A-SLUG", "", owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// instancesOf lists what the store holds for slug's installation of
// garam-bot-connector.
func (f *fixture) instancesOf(t *testing.T, slug string) []string {
	t.Helper()
	listed, err := f.apps.ListInstances(testCtx(t), grn.New(slug, app.InstallationType, "garam-bot-connector"))
	require.NoError(t, err)
	var out []string
	for _, i := range listed {
		out = append(out, i.GRN.String())
	}
	return out
}

// TestInstance_ARegistrationTokenIsAnsweredOnceAndStoredAsItsHash holds
// ADR-0184 decision 3 at the browser routes: creating a record and
// reissuing answer a token under no-store, the store keeps only its
// hash, and a reissue replaces the token held.
func TestInstance_ARegistrationTokenIsAnsweredOnceAndStoredAsItsHash(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRegister},
		permission{userGRNOf("owner-sub"), org.ActionInstanceReissue})
	installConnector(t, f, slug, owner)

	rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	created := decodeBody[apigen.CreatedInstance](t, rec)
	first := created.Registration.RegistrationToken
	assert.True(t, app.WellFormed(first, app.RegistrationTokenPrefix), first)
	assert.WithinDuration(t, time.Now().Add(app.RegistrationTokenTTL), created.Registration.ExpiresAt, time.Minute)
	assert.Equal(t, app.HashSecret(first), f.instanceKeys.tokenHash(created.Instance.Grn), "the hash is what is stored")

	g := created.Instance.Grn
	rec = f.do(t, "POST", connectorPath(slug)+"/instances/"+g[len(g)-16:]+"/registration-token", "", owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	second := decodeBody[apigen.InstanceRegistrationToken](t, rec).RegistrationToken
	assert.NotEqual(t, first, second)
	assert.Equal(t, app.HashSecret(second), f.instanceKeys.tokenHash(g), "a reissue replaces the token held")

	rec = f.do(t, "POST", connectorPath(slug)+"/instances/0000000000000000/registration-token", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "no token for a record that does not exist")
}

// TestInstance_AnotherOrganizationsRecordIsReachedByNoRoute holds that
// a record is addressed by its organization and identifier together: a
// member of acme holding every instance action is refused 403 on
// globex's paths, and globex's identifier under acme's installation is
// 404, as a record that does not exist is. globex's record is unchanged
// after each, and its owner reading it is the control.
func TestInstance_AnotherOrganizationsRecordIsReachedByNoRoute(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	var grants []permission
	for _, a := range []string{org.ActionInstanceRead, org.ActionInstanceUpdate, org.ActionInstanceDelete} {
		grants = append(grants, permission{userGRNOf("holder-sub"), a}, permission{userGRNOf("other-owner-sub"), a})
	}
	f.permit(append(grants,
		permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("other-owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("other-owner-sub"), org.ActionInstanceRegister})...)
	holder := f.sessionCookieFor(t, "holder-sub", "Holder")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("holder-sub"), org.RoleAdmin)))
	installConnector(t, f, slug, owner)
	installConnector(t, f, otherSlug, otherOwner)

	rec := f.do(t, "POST", connectorPath(otherSlug)+"/instances", `{"name":"theirs"}`, otherOwner)
	require.Equal(t, http.StatusCreated, rec.Code)
	theirs := decodeBody[apigen.CreatedInstance](t, rec).Instance
	id := theirs.Grn[len(theirs.Grn)-16:]

	for _, route := range []struct{ method, body string }{
		{"GET", ""}, {"PATCH", `{"name":"taken"}`}, {"DELETE", ""},
	} {
		rec = f.do(t, route.method, connectorPath(otherSlug)+"/instances/"+id, route.body, holder)
		assert.Equal(t, http.StatusForbidden, rec.Code, route.method+" on globex's path")
		rec = f.do(t, route.method, connectorPath(slug)+"/instances/"+id, route.body, holder)
		assert.Equal(t, http.StatusNotFound, rec.Code, route.method+" with globex's identifier under acme")
	}

	rec = f.do(t, "GET", connectorPath(otherSlug)+"/instances/"+id, "", otherOwner)
	require.Equal(t, http.StatusOK, rec.Code, "the control: globex's owner reads it")
	assert.Equal(t, theirs, decodeBody[apigen.Instance](t, rec), "globex's record is unchanged")
}

// TestInstance_AnInstanceOfAnotherInstallationIsNotFoundUnderThisOne
// holds that an instance is reached under its own installation's path
// alone: one of acme's other installation, under the connector's path,
// is 404, and under its own, the control, is read.
func TestInstance_AnInstanceOfAnotherInstallationIsNotFoundUnderThisOne(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRegister},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRead})
	f.apps.addApp(&app.App{GRN: grn.New(grn.RootOwner, app.AppType, "runner"), Slug: "runner", Name: "Runner",
		Visibility: app.VisibilityPublic, Instances: app.InstancesMany, Delivery: app.DeliveryPull})
	installConnector(t, f, slug, owner)
	rec := f.do(t, "POST", "/orgs/"+slug+"/installations", `{"app":"runner"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = f.do(t, "POST", "/orgs/"+slug+"/installations/runner/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	id := decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn[len("grn:"+slug+"/iam/instance/"):]

	rec = f.do(t, "GET", connectorPath(slug)+"/instances/"+id, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/installations/runner/instances/"+id, "", owner)
	assert.Equal(t, http.StatusOK, rec.Code, "the control: under its own installation")
}

// TestCreateInstance_TakesTheAppsInstanceSetting holds ADR-0192
// decision 5: an app whose setting is none runs no instance, and one
// whose setting is one refuses a second 409 while the first stands. The
// first instance of the second app is the control.
func TestCreateInstance_TakesTheAppsInstanceSetting(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRegister},
		permission{userGRNOf("owner-sub"), org.ActionInstanceDelete})
	for slugOf, n := range map[string]app.Instances{"keyless": app.InstancesNone, "single": app.InstancesOne} {
		f.apps.addApp(&app.App{GRN: grn.New(grn.RootOwner, app.AppType, slugOf), Slug: slugOf, Name: slugOf,
			Visibility: app.VisibilityPublic, Instances: n, Delivery: app.DeliveryPull})
		rec := f.do(t, "POST", "/orgs/"+slug+"/installations", `{"app":"`+slugOf+`"}`, owner)
		require.Equal(t, http.StatusCreated, rec.Code)
	}

	rec := f.do(t, "POST", "/orgs/"+slug+"/installations/keyless/instances", `{}`, owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "none runs no instance")

	rec = f.do(t, "POST", "/orgs/"+slug+"/installations/single/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code, "the control: one runs its first")
	first := decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn
	rec = f.do(t, "POST", "/orgs/"+slug+"/installations/single/instances", `{}`, owner)
	assert.Equal(t, http.StatusConflict, rec.Code, "one refuses a second")

	rec = f.do(t, "DELETE", "/orgs/"+slug+"/installations/single/instances/"+first[len(first)-16:], "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "POST", "/orgs/"+slug+"/installations/single/instances", `{}`, owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the one goes, and another may stand")
}
