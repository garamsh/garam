package api_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// declaredBot is an interface-1 Bot named name, which the connector
// admits.
func declaredBot(name string) string {
	return `{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot","metadata":{"namespace":"team-a","name":"` + name +
		`"},"spec":{"persona":{"role":"Reviewer"},"agent":{"harness":"claude-code"},"permissions":{"secrets":["bot-token/value"]}}}`
}

// declarationWrite is a declaration route's body placing declaredBot.
func declarationWrite(name string) string {
	return `{"object":` + declaredBot(name) + `}`
}

// declare places declaredBot(name) on g under id through the store.
func (f *fixture) declare(t *testing.T, instanceGRN, id, name string) {
	t.Helper()
	g, err := grn.Parse(instanceGRN)
	require.NoError(t, err)
	object, err := bot.ParseDeclaredObject([]byte(declaredBot(name)))
	require.NoError(t, err)
	_, _, err = f.declared.Declare(testCtx(t), g, id, object)
	require.NoError(t, err)
}

// hosting is slug's installation of garam-bot-connector holding
// iam:bot.host, which its owner grants (ADR-0192 decision 10).
func hosting(slug string) permission {
	return permission{grn.New(slug, app.InstallationType, "garam-bot-connector"), bot.ActionHost}
}

// declaringOrg is an organization whose owner may read and declare on
// the instance of garam-bot-connector it creates, and the instance's
// declarations path.
func declaringOrg(t *testing.T, f *fixture) (owner *http.Cookie, slug, instanceGRN, path string) {
	t.Helper()
	owner = f.sessionCookieFor(t, "owner-sub", "Owner")
	slug = createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionInstallationCreate},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRegister},
		permission{userGRNOf("owner-sub"), org.ActionInstanceRead},
		permission{userGRNOf("owner-sub"), org.ActionInstanceDelete},
		permission{userGRNOf("owner-sub"), org.ActionBotPlace},
		hosting(slug),
	)
	installConnector(t, f, slug, owner)
	rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	instanceGRN = decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn
	return owner, slug, instanceGRN, connectorPath(slug) + "/instances/" + instanceGRN[len(instanceGRN)-16:] + "/declarations"
}

// TestInstanceDeclaration_EachWriteIsTheNextGenerationAndATombstoneIsFinal
// reads each write back (ADR-0187): a POST draws the identifier at
// generation 1, a PUT to it is the next generation, a tombstoned
// declaration is read as gone and its identifier is never placed again.
func TestInstanceDeclaration_EachWriteIsTheNextGenerationAndATombstoneIsFinal(t *testing.T) {
	f := newFixture(t)
	owner, _, instanceGRN, path := declaringOrg(t, f)

	rec := f.do(t, "POST", path, declarationWrite("reviewer"), owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.Declaration](t, rec)
	assert.Regexp(t, regexp.MustCompile(`^decl-[0-9a-f]{16}$`), created.Id)
	assert.Equal(t, instanceGRN, created.Instance)
	assert.Equal(t, int64(1), created.Generation)
	assert.Contains(t, string(created.Object), `"secrets":["bot-token/value"]`, "the object is answered as kept")

	rec = f.do(t, "PUT", path+"/"+created.Id, `{"object":`+strings.Replace(declaredBot("reviewer"), `"Reviewer"`, `"Lead Reviewer"`, 1)+`}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	next := decodeBody[apigen.Declaration](t, rec)
	assert.Equal(t, int64(2), next.Generation)
	assert.Contains(t, string(next.Object), `"Lead Reviewer"`)

	rec = f.do(t, "PUT", path+"/chosen-id", declarationWrite("writer"), owner)
	require.Equal(t, http.StatusCreated, rec.Code, "an identifier never placed is placed")
	assert.Equal(t, int64(1), decodeBody[apigen.Declaration](t, rec).Generation)

	rec = f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	var ids []string
	for _, d := range decodeBody[apigen.DeclarationList](t, rec).Declarations {
		ids = append(ids, d.Id)
	}
	assert.Equal(t, []string{created.Id, "chosen-id"}, ids, "in the order each was last written")

	rec = f.do(t, "DELETE", path+"/"+created.Id, "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", path+"/"+created.Id, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "a tombstoned declaration is read as gone")
	rec = f.do(t, "DELETE", path+"/"+created.Id, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "a declaration is tombstoned once")
	rec = f.do(t, "PUT", path+"/"+created.Id, declarationWrite("reviewer"), owner)
	assert.Equal(t, http.StatusConflict, rec.Code, "a tombstoned identifier is never placed again")

	rec = f.do(t, "GET", path+"/chosen-id", "", owner)
	require.Equal(t, http.StatusOK, rec.Code, "the control: the other declaration stands")
}

// TestInstanceDeclaration_RefusesWhatNoDeclarationMayBe covers each
// refusal a write answers before anything is stored: 422 naming the
// field for an object the connector refuses, 400 for a request not in
// the required form. The accepted write is the control.
func TestInstanceDeclaration_RefusesWhatNoDeclarationMayBe(t *testing.T) {
	f := newFixture(t)
	owner, _, _, path := declaringOrg(t, f)
	withObject := func(from, to string) string {
		return `{"object":` + strings.Replace(declaredBot("a"), from, to, 1) + `}`
	}
	for name, tc := range map[string]struct {
		method, path, body string
		status             int
		field              string
	}{
		"an image (DECL-9)":          {"POST", "", withObject(`"persona"`, `"image":"x","persona"`), http.StatusUnprocessableEntity, "spec.image"},
		"a secret's value (DECL-8)":  {"POST", "", withObject(`"bot-token/value"`, `"hunter2"`), http.StatusUnprocessableEntity, "spec.permissions.secrets[0]"},
		"an unknown field":           {"POST", "", withObject(`"persona"`, `"unknownField":true,"persona"`), http.StatusUnprocessableEntity, "spec.unknownField"},
		"a kind not declared":        {"PUT", "/a", withObject(`"Bot"`, `"Pod"`), http.StatusUnprocessableEntity, "Pod"},
		"no namespace":               {"POST", "", withObject(`"namespace":"team-a",`, ``), http.StatusUnprocessableEntity, "metadata.namespace"},
		"no object":                  {"POST", "", `{}`, http.StatusUnprocessableEntity, ""},
		"an object not an object":    {"POST", "", `{"object":[]}`, http.StatusUnprocessableEntity, ""},
		"a body not JSON":            {"POST", "", `{"object":`, http.StatusBadRequest, ""},
		"an identifier too long":     {"PUT", "/" + strings.Repeat("a", 129), declarationWrite("a"), http.StatusBadRequest, ""},
		"an identifier with a space": {"PUT", "/a%20b", declarationWrite("a"), http.StatusBadRequest, ""},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(t, tc.method, path+tc.path, tc.body, owner)
			require.Equal(t, tc.status, rec.Code)
			refusal := decodeBody[apigen.Error](t, rec)
			assert.Contains(t, refusal.Message, tc.field, "the refusal names what it refused")
			assert.NotContains(t, refusal.Message, "hunter2", "a refusal never repeats a secret's value")
		})
	}
	rec := f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[apigen.DeclarationList](t, rec).Declarations, "nothing refused was stored")
	rec = f.do(t, "PUT", path+"/"+strings.Repeat("a", 128), declarationWrite("a"), owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the control: the longest identifier is placed")
}

// TestInstanceDeclaration_AnIdentifierAndAnObjectNameOneDeclaration
// holds the two conflicts: an identifier placed on another record, and
// an object another live declaration of the record names. Tombstoning
// the latter frees its object, which is the control.
func TestInstanceDeclaration_AnIdentifierAndAnObjectNameOneDeclaration(t *testing.T) {
	f := newFixture(t)
	owner, slug, _, path := declaringOrg(t, f)
	rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	other := decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn
	otherPath := connectorPath(slug) + "/instances/" + other[len(other)-16:] + "/declarations"

	rec = f.do(t, "PUT", path+"/shared", declarationWrite("reviewer"), owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	rec = f.do(t, "PUT", otherPath+"/shared", declarationWrite("reviewer"), owner)
	assert.Equal(t, http.StatusConflict, rec.Code, "an identifier placed on another record")
	rec = f.do(t, "GET", otherPath+"/shared", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "another record's declaration is not read through this one")
	rec = f.do(t, "DELETE", otherPath+"/shared", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "nor tombstoned through it")

	rec = f.do(t, "POST", path, declarationWrite("reviewer"), owner)
	assert.Equal(t, http.StatusConflict, rec.Code, "an object another live declaration names")
	rec = f.do(t, "POST", otherPath, declarationWrite("reviewer"), owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the same object on another record")

	rec = f.do(t, "DELETE", path+"/shared", "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "POST", path, declarationWrite("reviewer"), owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the control: a tombstone frees the object it named")
}

// TestInstanceDeclaration_AnIdentifierAnAgentRecordHoldsIsTaken holds
// ADR-0189's shared identifier space on the route: a declaration placed
// under an identifier an agent record of the organization holds is 409,
// and another identifier is the control.
func TestInstanceDeclaration_AnIdentifierAnAgentRecordHoldsIsTaken(t *testing.T) {
	f := newFixture(t)
	owner, _, _, path := declaringOrg(t, f)
	f.declared.agentIDs = map[string]bool{"mira": true}

	rec := f.do(t, "PUT", path+"/mira", declarationWrite("mira"), owner)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "names a bot the organization already holds")
	rec = f.do(t, "PUT", path+"/reviewer", declarationWrite("mira"), owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the control: an identifier no agent record holds")
}

// TestInstanceDeclaration_AnotherOrganizationsRecordIsReachedByNoRoute
// mirrors the record's own cross-organization test for the declaration
// routes: acme's admin holding every action is refused on globex's
// path and answered 404 with globex's identifier under acme.
func TestInstanceDeclaration_AnotherOrganizationsRecordIsReachedByNoRoute(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	var grants []permission
	for _, a := range []string{org.ActionInstanceRead, org.ActionBotPlace} {
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

	rec := f.do(t, "POST", connectorPath(otherSlug)+"/instances", `{}`, otherOwner)
	require.Equal(t, http.StatusCreated, rec.Code)
	theirs := decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn
	id := theirs[len(theirs)-16:]
	f.declare(t, theirs, "theirs", "reviewer")

	for _, route := range []struct{ method, path, body string }{
		{"GET", "", ""}, {"POST", "", declarationWrite("mine")},
		{"GET", "/theirs", ""}, {"PUT", "/theirs", declarationWrite("mine")}, {"DELETE", "/theirs", ""},
	} {
		rec = f.do(t, route.method, connectorPath(otherSlug)+"/instances/"+id+"/declarations"+route.path, route.body, holder)
		assert.Equal(t, http.StatusForbidden, rec.Code, route.method+route.path+" on globex's path")
		rec = f.do(t, route.method, connectorPath(slug)+"/instances/"+id+"/declarations"+route.path, route.body, holder)
		assert.Equal(t, http.StatusNotFound, rec.Code, route.method+route.path+" with globex's identifier under acme")
	}

	rec = f.do(t, "GET", connectorPath(otherSlug)+"/instances/"+id+"/declarations/theirs", "", otherOwner)
	require.Equal(t, http.StatusOK, rec.Code, "the control: globex's owner reads it")
	var object map[string]any
	require.NoError(t, json.Unmarshal(decodeBody[apigen.Declaration](t, rec).Object, &object))
	assert.Equal(t, "reviewer", object["metadata"].(map[string]any)["name"], "globex's declaration is unchanged")
}

// TestInstanceDeclaration_EachWriteIsDecidedAsBotPlaceOnTheInstance
// holds ADR-0192 decision 10's action on the routes: each write asks
// `iam:bot.place` with the instance as its resource, and not the
// installation or the organization. A member the decision refuses is
// answered 403 and places nothing; the owner and the administrator,
// whom role.go grants it, place.
func TestInstanceDeclaration_EachWriteIsDecidedAsBotPlaceOnTheInstance(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	var placed []policydecision.Question
	f.answers(func(q policydecision.Question) (bool, error) {
		if q.Action == org.ActionBotPlace {
			placed = append(placed, q)
		}
		return q.Subject != userGRNOf("member-sub") || q.Action == org.ActionInstanceRead, nil
	})
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))
	installConnector(t, f, slug, owner)
	rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	instance := decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn
	path := connectorPath(slug) + "/instances/" + instance[len(instance)-16:] + "/declarations"

	for _, write := range []struct{ method, path, body string }{
		{"POST", "", declarationWrite("reviewer")},
		{"PUT", "/placed", declarationWrite("mira")},
		{"DELETE", "/placed", ""},
	} {
		placed = nil
		rec = f.do(t, write.method, path+write.path, write.body, member)
		assert.Equal(t, http.StatusForbidden, rec.Code, write.method+write.path+" by a member")
		rec = f.do(t, write.method, path+write.path, write.body, owner)
		assert.Less(t, rec.Code, 300, write.method+write.path+" by the owner: "+rec.Body.String())
		require.Len(t, placed, 2, write.method+write.path)
		for _, q := range placed {
			assert.Equal(t, instance, q.Resource.String(), write.method+write.path+" is decided on the instance")
		}
	}
	rec = f.do(t, "GET", path, "", member)
	assert.Equal(t, http.StatusOK, rec.Code, "the control: a member reads what is placed")
}

// TestInstanceDeclaration_AnInstanceWhoseInstallationHostsNoBotsIsNoTarget
// holds ADR-0192 decision 10's gate on the routes: each write asks
// `iam:bot.host` with the instance's installation as principal and as
// resource, past the service's cache, and where it is refused the owner
// holding `iam:bot.place` is answered 403 and places nothing, while
// reading is unchanged. A granted installation whose app does not ask
// to host bots is refused as well. The same write once the installation
// hosts bots is the control.
func TestInstanceDeclaration_AnInstanceWhoseInstallationHostsNoBotsIsNoTarget(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	installation := grn.New(slug, app.InstallationType, "garam-bot-connector")
	hosts := false
	var asked []policydecision.Question
	f.answers(func(q policydecision.Question) (bool, error) {
		if q.Action == bot.ActionHost {
			asked = append(asked, q)
			return hosts, nil
		}
		return q.Subject == userGRNOf("owner-sub"), nil
	})
	installConnector(t, f, slug, owner)
	rec := f.do(t, "POST", connectorPath(slug)+"/instances", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	instance := decodeBody[apigen.CreatedInstance](t, rec).Instance.Grn
	f.declare(t, instance, "placed", "reviewer")
	path := connectorPath(slug) + "/instances/" + instance[len(instance)-16:] + "/declarations"

	for _, write := range []struct{ method, path, body string }{
		{"POST", "", declarationWrite("mira")},
		{"PUT", "/placed", declarationWrite("mira")},
		{"DELETE", "/placed", ""},
	} {
		asked = nil
		rec = f.do(t, write.method, path+write.path, write.body, owner)
		assert.Equal(t, http.StatusForbidden, rec.Code, write.method+write.path)
		require.Len(t, asked, 1, write.method+write.path)
		assert.Equal(t, policydecision.Question{Subject: installation, Action: bot.ActionHost,
			Resource: installation, Current: true}, asked[0], write.method+write.path)
	}
	rec = f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code, "reading is not gated")
	list := decodeBody[apigen.DeclarationList](t, rec).Declarations
	require.Len(t, list, 1, "nothing was placed")
	assert.Equal(t, int64(1), list[0].Generation, "nothing was changed")

	hosts = true
	f.apps.apps[connectorApp().GRN.String()].RequestsBotHosting = false
	rec = f.do(t, "POST", path, declarationWrite("mira"), owner)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a permit does not host bots for an app that does not ask to")

	f.apps.apps[connectorApp().GRN.String()].RequestsBotHosting = true
	rec = f.do(t, "POST", path, declarationWrite("mira"), owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the control: "+rec.Body.String())
}
