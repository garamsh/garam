package api_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/org"
)

// mintedCluster matches a GRN of slug's whose identifier garam
// minted: sixteen lowercase hex digits under the iam cluster type.
func mintedCluster(slug string) *regexp.Regexp {
	return regexp.MustCompile(`^grn:` + regexp.QuoteMeta(slug) + `/iam/cluster/[0-9a-f]{16}$`)
}

// TestCreateCluster_MintsTheIdentifierUnderTheRegisterAction holds
// ADR-0184 decision 1: garam mints the identifier, and creating is
// decided as `iam:cluster.register` on the organization.
//
// The stranger is a member permitted to read and not to register, and
// the outsider is permitted to register in another organization; each
// isolates one half of the check, and the admin is the control.
func TestCreateCluster_MintsTheIdentifierUnderTheRegisterAction(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	f.permit(
		permission{userGRNOf("admin-sub"), org.ActionClusterRegister},
		permission{userGRNOf("stranger-sub"), org.ActionClusterRead},
		permission{userGRNOf("outsider-sub"), org.ActionClusterRegister},
	)
	admin := f.sessionCookieFor(t, "admin-sub", "Admin")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("admin-sub"), org.RoleAdmin)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("stranger-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(otherSlug), userGRNOf("outsider-sub"), org.RoleMember)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/clusters", `{"name":" EKS prod ","description":"the main one"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.CreatedCluster](t, rec).Cluster
	assert.Regexp(t, mintedCluster(slug), created.Grn)
	assert.Equal(t, "EKS prod", created.Name)
	assert.Equal(t, "the main one", created.Description)
	assert.Equal(t, orgGRNOf(slug).String(), created.OrgGrn)
	assert.Equal(t, userGRNOf("admin-sub").String(), created.CreatedBy)

	// Two creations mint two identifiers.
	rec = f.do(t, "POST", "/orgs/"+slug+"/clusters", `{}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	second := decodeBody[apigen.CreatedCluster](t, rec).Cluster
	assert.NotEqual(t, created.Grn, second.Grn)
	assert.Regexp(t, mintedCluster(slug), second.Grn)

	rec = f.do(t, "POST", "/orgs/"+slug+"/clusters", `{}`, stranger)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/clusters", "", stranger)
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "POST", "/orgs/"+slug+"/clusters", `{}`, outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "POST", "/orgs/"+otherSlug+"/clusters", `{}`, outsider)
	assert.Equal(t, http.StatusCreated, rec.Code)

	rec = f.do(t, "POST", "/orgs/"+slug+"/clusters", `{}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestCreateCluster_NamesItByItsIdentifierWhenGivenNone holds that a
// name left out is the identifier, as an operator's is.
func TestCreateCluster_NamesItByItsIdentifierWhenGivenNone(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionClusterRegister})

	rec := f.do(t, "POST", "/orgs/"+slug+"/clusters", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.CreatedCluster](t, rec).Cluster
	assert.Equal(t, created.Grn[len(created.Grn)-16:], created.Name)
	assert.Empty(t, created.Description)
}

// TestCreateCluster_RefusesDetailsItCannotStore covers the details'
// bounds. The accepted creation is the control.
func TestCreateCluster_RefusesDetailsItCannotStore(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionClusterRegister})

	long := make([]byte, 101)
	for i := range long {
		long[i] = 'a'
	}
	for name, body := range map[string]string{
		"an empty name":            `{"name":"   "}`,
		"a name over 100":          `{"name":"` + string(long) + `"}`,
		"a NUL in the name":        `{"name":"a\u0000b"}`,
		"a body not an object":     `[]`,
		"a name that is a list":    `{"name":[]}`,
		"a description over 1000":  `{"description":"` + strings.Repeat("d", 1001) + `"}`,
		"a NUL in the description": `{"description":"a\u0000b"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(t, "POST", "/orgs/"+slug+"/clusters", body, owner)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
	rec := f.do(t, "POST", "/orgs/"+slug+"/clusters", `{"name":"`+string(long[:100])+`"}`, owner)
	assert.Equal(t, http.StatusCreated, rec.Code)
	rec = f.do(t, "POST", "/orgs/"+slug+"/clusters", `{"description":"`+strings.Repeat("d", 1000)+`"}`, owner)
	assert.Equal(t, http.StatusCreated, rec.Code, "the control: a description of 1000")
	assert.Len(t, f.clustersOf(t, slug), 2, "no refused creation was stored")
}

// TestCluster_EachRouteOnARecordTakesItsOwnAction holds ADR-0184
// decision 9 route by route: each is decided on its own action, and a
// caller holding every other cluster action is refused it.
func TestCluster_EachRouteOnARecordTakesItsOwnAction(t *testing.T) {
	all := []string{org.ActionClusterRead, org.ActionClusterRegister, org.ActionClusterUpdate, org.ActionClusterReissue,
		org.ActionClusterDelete, org.ActionClusterDeclare}
	tests := []struct {
		name   string
		action string
		method string
		path   string
		body   string
		want   int
	}{
		{"get", org.ActionClusterRead, "GET", "", "", http.StatusOK},
		{"list", org.ActionClusterRead, "GET", "list", "", http.StatusOK},
		{"update", org.ActionClusterUpdate, "PATCH", "", `{"name":"renamed"}`, http.StatusOK},
		{"delete", org.ActionClusterDelete, "DELETE", "", "", http.StatusNoContent},
		{"reissue", org.ActionClusterReissue, "POST", "/registration-token", "", http.StatusCreated},
		{"list keys", org.ActionClusterRead, "GET", "/keys", "", http.StatusOK},
		{"revoke a key", org.ActionClusterReissue, "DELETE", "/keys/held", "", http.StatusNoContent},
		{"list declarations", org.ActionClusterRead, "GET", "/declarations", "", http.StatusOK},
		{"read a declaration", org.ActionClusterRead, "GET", "/declarations/placed", "", http.StatusOK},
		{"declare", org.ActionClusterDeclare, "POST", "/declarations", declarationWrite("other"), http.StatusCreated},
		{"declare under an id", org.ActionClusterDeclare, "PUT", "/declarations/placed", declarationWrite("reviewer"), http.StatusOK},
		{"tombstone", org.ActionClusterDeclare, "DELETE", "/declarations/placed", "", http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			owner := f.sessionCookieFor(t, "owner-sub", "Owner")
			slug := createOrg(t, f, owner, "acme")
			var others []permission
			for _, a := range all {
				if a != tt.action {
					others = append(others, permission{userGRNOf("lacking-sub"), a})
				}
			}
			f.permit(append(others,
				permission{userGRNOf("owner-sub"), org.ActionClusterRegister},
				permission{userGRNOf("holder-sub"), tt.action})...)
			lacking := f.sessionCookieFor(t, "lacking-sub", "Lacking")
			holder := f.sessionCookieFor(t, "holder-sub", "Holder")
			require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("lacking-sub"), org.RoleAdmin)))
			require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("holder-sub"), org.RoleMember)))

			rec := f.do(t, "POST", "/orgs/"+slug+"/clusters", `{"name":"prod"}`, owner)
			require.Equal(t, http.StatusCreated, rec.Code)
			path := "/orgs/" + slug + "/clusters"
			if tt.path != "list" {
				g := decodeBody[apigen.CreatedCluster](t, rec).Cluster.Grn
				f.clusterKeys.place(g, cluster.Key{KID: "held", State: cluster.KeyStateActive})
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

// TestCluster_UpdateChangesOnlyTheDetailsGivenAndDeleteRemovesIt reads
// each write back.
func TestCluster_UpdateChangesOnlyTheDetailsGivenAndDeleteRemovesIt(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionClusterRegister},
		permission{userGRNOf("owner-sub"), org.ActionClusterRead},
		permission{userGRNOf("owner-sub"), org.ActionClusterUpdate},
		permission{userGRNOf("owner-sub"), org.ActionClusterDelete},
	)
	rec := f.do(t, "POST", "/orgs/"+slug+"/clusters", `{"name":"prod","description":"first"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.CreatedCluster](t, rec).Cluster
	path := "/orgs/" + slug + "/clusters/" + created.Grn[len(created.Grn)-16:]

	rec = f.do(t, "PATCH", path, `{"description":""}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	updated := decodeBody[apigen.Cluster](t, rec)
	assert.Equal(t, "prod", updated.Name, "a name left out keeps its value")
	assert.Empty(t, updated.Description, "an empty description clears it")
	assert.Equal(t, created.Grn, updated.Grn)

	rec = f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, updated, decodeBody[apigen.Cluster](t, rec))

	rec = f.do(t, "DELETE", path, "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", path, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "DELETE", path, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/clusters/NOT-A-SLUG", "", owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// clustersOf lists what the store holds for an organization.
func (f *fixture) clustersOf(t *testing.T, slug string) []string {
	t.Helper()
	listed, err := f.clusters.ListByOrg(testCtx(t), orgGRNOf(slug))
	require.NoError(t, err)
	var out []string
	for _, c := range listed {
		out = append(out, c.GRN.String())
	}
	return out
}

// TestCluster_ARegistrationTokenIsAnsweredOnceAndStoredAsItsHash holds
// ADR-0184 decision 3 at the browser routes: creating a record and
// reissuing answer a token under no-store, the store keeps only its
// hash, and a reissue replaces the token held.
func TestCluster_ARegistrationTokenIsAnsweredOnceAndStoredAsItsHash(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionClusterRegister},
		permission{userGRNOf("owner-sub"), org.ActionClusterReissue})

	rec := f.do(t, "POST", "/orgs/"+slug+"/clusters", `{}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	created := decodeBody[apigen.CreatedCluster](t, rec)
	first := created.Registration.RegistrationToken
	assert.True(t, cluster.WellFormed(first, cluster.RegistrationTokenPrefix), first)
	assert.WithinDuration(t, time.Now().Add(cluster.RegistrationTokenTTL), created.Registration.ExpiresAt, time.Minute)
	assert.Equal(t, cluster.HashSecret(first), f.clusterKeys.tokenHash(created.Cluster.Grn), "the hash is what is stored")

	g := created.Cluster.Grn
	rec = f.do(t, "POST", "/orgs/"+slug+"/clusters/"+g[len(g)-16:]+"/registration-token", "", owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	second := decodeBody[apigen.ClusterRegistrationToken](t, rec).RegistrationToken
	assert.NotEqual(t, first, second)
	assert.Equal(t, cluster.HashSecret(second), f.clusterKeys.tokenHash(g), "a reissue replaces the token held")

	rec = f.do(t, "POST", "/orgs/"+slug+"/clusters/0000000000000000/registration-token", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "no token for a record that does not exist")
}

// TestCluster_AnotherOrganizationsRecordIsReachedByNoRoute holds that a
// record is addressed by its organization and identifier together: a
// member of acme holding every cluster action is refused 403 on
// globex's paths, and globex's identifier under acme's slug is 404, as
// a record that does not exist is. globex's record is unchanged after
// each, and its owner reading it is the control.
func TestCluster_AnotherOrganizationsRecordIsReachedByNoRoute(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	var grants []permission
	for _, a := range []string{org.ActionClusterRead, org.ActionClusterUpdate, org.ActionClusterDelete} {
		grants = append(grants, permission{userGRNOf("holder-sub"), a}, permission{userGRNOf("other-owner-sub"), a})
	}
	f.permit(append(grants, permission{userGRNOf("other-owner-sub"), org.ActionClusterRegister})...)
	holder := f.sessionCookieFor(t, "holder-sub", "Holder")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("holder-sub"), org.RoleAdmin)))

	rec := f.do(t, "POST", "/orgs/"+otherSlug+"/clusters", `{"name":"theirs"}`, otherOwner)
	require.Equal(t, http.StatusCreated, rec.Code)
	theirs := decodeBody[apigen.CreatedCluster](t, rec).Cluster
	id := theirs.Grn[len(theirs.Grn)-16:]

	for _, route := range []struct{ method, body string }{
		{"GET", ""}, {"PATCH", `{"name":"taken"}`}, {"DELETE", ""},
	} {
		rec = f.do(t, route.method, "/orgs/"+otherSlug+"/clusters/"+id, route.body, holder)
		assert.Equal(t, http.StatusForbidden, rec.Code, route.method+" on globex's path")
		rec = f.do(t, route.method, "/orgs/"+slug+"/clusters/"+id, route.body, holder)
		assert.Equal(t, http.StatusNotFound, rec.Code, route.method+" with globex's identifier under acme")
	}

	rec = f.do(t, "GET", "/orgs/"+otherSlug+"/clusters/"+id, "", otherOwner)
	require.Equal(t, http.StatusOK, rec.Code, "the control: globex's owner reads it")
	assert.Equal(t, theirs, decodeBody[apigen.Cluster](t, rec), "globex's record is unchanged")
}
