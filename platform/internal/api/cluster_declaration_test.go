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
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/org"
)

// declaredBot is a Bot named name that DECL-7 to DECL-9 admit.
func declaredBot(name string) string {
	return `{"apiVersion":"garam.sh/v1alpha1","kind":"Bot","metadata":{"namespace":"team-a","name":"` + name +
		`"},"spec":{"replicas":12345678901234567890,"tokenSecretRef":"bot-token/value"}}`
}

// declarationWrite is a declaration route's body placing declaredBot.
func declarationWrite(name string) string {
	return `{"object":` + declaredBot(name) + `}`
}

// declare places declaredBot(name) on g under id through the store.
func (f *fixture) declare(t *testing.T, clusterGRN, id, name string) {
	t.Helper()
	g, err := grn.Parse(clusterGRN)
	require.NoError(t, err)
	object, err := cluster.ParseDeclaredObject([]byte(declaredBot(name)))
	require.NoError(t, err)
	_, _, err = f.declared.Declare(testCtx(t), g, id, object)
	require.NoError(t, err)
}

// declaringOrg is an organization whose owner may read and declare on
// the cluster it creates, and the cluster's declarations path.
func declaringOrg(t *testing.T, f *fixture) (owner *http.Cookie, slug, clusterGRN, path string) {
	t.Helper()
	owner = f.sessionCookieFor(t, "owner-sub", "Owner")
	slug = createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionClusterRegister},
		permission{userGRNOf("owner-sub"), org.ActionClusterRead},
		permission{userGRNOf("owner-sub"), org.ActionClusterDelete},
		permission{userGRNOf("owner-sub"), org.ActionClusterDeclare},
	)
	rec := f.do(t, "POST", "/orgs/"+slug+"/clusters", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	clusterGRN = decodeBody[apigen.CreatedCluster](t, rec).Cluster.Grn
	return owner, slug, clusterGRN, "/orgs/" + slug + "/clusters/" + clusterGRN[len(clusterGRN)-16:] + "/declarations"
}

// TestClusterDeclaration_EachWriteIsTheNextGenerationAndATombstoneIsFinal
// reads each write back (ADR-0187): a POST draws the identifier at
// generation 1, a PUT to it is the next generation, a tombstoned
// declaration is read as gone and its identifier is never placed again.
func TestClusterDeclaration_EachWriteIsTheNextGenerationAndATombstoneIsFinal(t *testing.T) {
	f := newFixture(t)
	owner, _, clusterGRN, path := declaringOrg(t, f)

	rec := f.do(t, "POST", path, declarationWrite("reviewer"), owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.ClusterDeclaration](t, rec)
	assert.Regexp(t, regexp.MustCompile(`^decl-[0-9a-f]{16}$`), created.Id)
	assert.Equal(t, clusterGRN, created.Cluster)
	assert.Equal(t, int64(1), created.Generation)
	assert.Contains(t, string(created.Object), "12345678901234567890", "the object is answered as kept, its numbers unrounded")

	rec = f.do(t, "PUT", path+"/"+created.Id, `{"object":`+strings.Replace(declaredBot("reviewer"), `"replicas"`, `"paused":true,"replicas"`, 1)+`}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	next := decodeBody[apigen.ClusterDeclaration](t, rec)
	assert.Equal(t, int64(2), next.Generation)
	assert.Contains(t, string(next.Object), `"paused":true`)

	rec = f.do(t, "PUT", path+"/chosen-id", declarationWrite("writer"), owner)
	require.Equal(t, http.StatusCreated, rec.Code, "an identifier never placed is placed")
	assert.Equal(t, int64(1), decodeBody[apigen.ClusterDeclaration](t, rec).Generation)

	rec = f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	var ids []string
	for _, d := range decodeBody[apigen.ClusterDeclarationList](t, rec).Declarations {
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

// TestClusterDeclaration_RefusesWhatNoDeclarationMayBe covers each
// refusal a write answers before anything is stored; the accepted
// write is the control.
func TestClusterDeclaration_RefusesWhatNoDeclarationMayBe(t *testing.T) {
	f := newFixture(t)
	owner, _, _, path := declaringOrg(t, f)
	for name, tc := range map[string]struct {
		method, path, body string
	}{
		"an image (DECL-9)":          {"POST", "", `{"object":` + strings.Replace(declaredBot("a"), `"replicas"`, `"image":"x","replicas"`, 1) + `}`},
		"a secret's value":           {"POST", "", `{"object":` + strings.Replace(declaredBot("a"), `"bot-token/value"`, `"hunter2"`, 1) + `}`},
		"a kind not declared":        {"POST", "", `{"object":` + strings.Replace(declaredBot("a"), `"Bot"`, `"Pod"`, 1) + `}`},
		"no object":                  {"POST", "", `{}`},
		"an object not an object":    {"POST", "", `{"object":[]}`},
		"an identifier too long":     {"PUT", "/" + strings.Repeat("a", 129), declarationWrite("a")},
		"an identifier with a space": {"PUT", "/a%20b", declarationWrite("a")},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(t, tc.method, path+tc.path, tc.body, owner)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
	rec := f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[apigen.ClusterDeclarationList](t, rec).Declarations, "nothing refused was stored")
	rec = f.do(t, "PUT", path+"/"+strings.Repeat("a", 128), declarationWrite("a"), owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the control: the longest identifier is placed")
}

// TestClusterDeclaration_AnIdentifierAndAnObjectNameOneDeclaration
// holds the two conflicts: an identifier placed on another record, and
// an object another live declaration of the record names. Tombstoning
// the latter frees its object, which is the control.
func TestClusterDeclaration_AnIdentifierAndAnObjectNameOneDeclaration(t *testing.T) {
	f := newFixture(t)
	owner, slug, _, path := declaringOrg(t, f)
	rec := f.do(t, "POST", "/orgs/"+slug+"/clusters", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	other := decodeBody[apigen.CreatedCluster](t, rec).Cluster.Grn
	otherPath := "/orgs/" + slug + "/clusters/" + other[len(other)-16:] + "/declarations"

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

// TestClusterDeclaration_AnotherOrganizationsRecordIsReachedByNoRoute
// mirrors the record's own cross-organization test for the declaration
// routes: acme's admin holding every action is refused on globex's
// path and answered 404 with globex's identifier under acme.
func TestClusterDeclaration_AnotherOrganizationsRecordIsReachedByNoRoute(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	var grants []permission
	for _, a := range []string{org.ActionClusterRead, org.ActionClusterDeclare} {
		grants = append(grants, permission{userGRNOf("holder-sub"), a}, permission{userGRNOf("other-owner-sub"), a})
	}
	f.permit(append(grants, permission{userGRNOf("other-owner-sub"), org.ActionClusterRegister})...)
	holder := f.sessionCookieFor(t, "holder-sub", "Holder")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("holder-sub"), org.RoleAdmin)))

	rec := f.do(t, "POST", "/orgs/"+otherSlug+"/clusters", `{}`, otherOwner)
	require.Equal(t, http.StatusCreated, rec.Code)
	theirs := decodeBody[apigen.CreatedCluster](t, rec).Cluster.Grn
	id := theirs[len(theirs)-16:]
	f.declare(t, theirs, "theirs", "reviewer")

	for _, route := range []struct{ method, path, body string }{
		{"GET", "", ""}, {"POST", "", declarationWrite("mine")},
		{"GET", "/theirs", ""}, {"PUT", "/theirs", declarationWrite("mine")}, {"DELETE", "/theirs", ""},
	} {
		rec = f.do(t, route.method, "/orgs/"+otherSlug+"/clusters/"+id+"/declarations"+route.path, route.body, holder)
		assert.Equal(t, http.StatusForbidden, rec.Code, route.method+route.path+" on globex's path")
		rec = f.do(t, route.method, "/orgs/"+slug+"/clusters/"+id+"/declarations"+route.path, route.body, holder)
		assert.Equal(t, http.StatusNotFound, rec.Code, route.method+route.path+" with globex's identifier under acme")
	}

	rec = f.do(t, "GET", "/orgs/"+otherSlug+"/clusters/"+id+"/declarations/theirs", "", otherOwner)
	require.Equal(t, http.StatusOK, rec.Code, "the control: globex's owner reads it")
	var object map[string]any
	require.NoError(t, json.Unmarshal(decodeBody[apigen.ClusterDeclaration](t, rec).Object, &object))
	assert.Equal(t, "reviewer", object["metadata"].(map[string]any)["name"], "globex's declaration is unchanged")
}
