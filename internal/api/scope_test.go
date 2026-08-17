package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// grnObject adapts a GRN to authz.Object; the engine matches strings.
type grnObject struct{ g grn.GRN }

func (o grnObject) GRN() grn.GRN { return o.g }
func (o grnObject) Type() string { return o.g.ServiceType }

// enforce answers the Evaluator seam with the real policy engine over
// the statements the server collected, so scope tests exercise
// statement collection instead of a stubbed verdict.
func (f *fixture) enforce() {
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(ctx context.Context, in api.EvalInput) (bool, error) {
			statements := make([]authz.Statement, 0, len(in.Statements))
			for _, s := range in.Statements {
				statements = append(statements, authz.Statement{
					Sid:      s.Sid,
					Effect:   authz.Effect(s.Effect),
					Action:   s.Action,
					Resource: s.Resource,
				})
			}
			d := authz.NewStatementEvaluator(statements).
				Evaluate(ctx, authz.Context{Object: grnObject{in.Object}, Action: in.Action})
			return d == authz.DecisionAllow, nil
		})
}

func TestScopedPermissions_Matrix(t *testing.T) {
	f := newFixture(t)
	f.enforce()

	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	alice := f.sessionCookieFor(t, "alice-sub", "Alice")
	bob := f.sessionCookieFor(t, "bob-sub", "Bob")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")

	slug := createOrg(t, f, owner, "acme")
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)
	aliceGRN := grn.New("root", "default", user.ServiceType, "alice-sub")
	bobGRN := grn.New("root", "default", user.ServiceType, "bob-sub")
	for _, g := range []grn.GRN{aliceGRN, bobGRN} {
		require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, g, org.RoleMember)))
	}

	// Alice administers the group "eng" and the project "web"; the
	// sibling group "ops" and project "app" grant her nothing.
	for _, name := range []string{"eng", "ops"} {
		g := grn.New(slug, "default", org.ServiceTypeGroup, name)
		require.NoError(t, f.orgs.CreateGroup(testCtx(t), org.NewGroup(g, name, orgGRN, string(org.RoleAdmin))))
	}
	for _, name := range []string{"web", "app"} {
		g := grn.New(slug, "default", org.ServiceTypeProject, name)
		require.NoError(t, f.orgs.CreateProject(testCtx(t), org.NewProject(g, name, orgGRN, string(org.RoleAdmin))))
	}
	require.NoError(t, f.orgs.AddGroupMember(testCtx(t),
		grn.New(slug, "default", org.ServiceTypeGroup, "eng"), aliceGRN))
	require.NoError(t, f.orgs.AddProjectMember(testCtx(t),
		grn.New(slug, "default", org.ServiceTypeProject, "web"), aliceGRN))
	ada := grn.New(slug, "default", user.ServiceType, "ada")

	rename := `{"name":"renamed"}`
	cases := []struct {
		name         string
		method, path string
		body         string
		cookie       *http.Cookie
		want         int
	}{
		{"owner updates any group", "PATCH", "/orgs/" + slug + "/groups/eng", rename, owner, http.StatusOK},
		{"group role updates its own group", "PATCH", "/orgs/" + slug + "/groups/eng", rename, alice, http.StatusOK},
		{"group role adds a member to its own group", "POST", "/orgs/" + slug + "/groups/eng/members", `{"memberGrn":"` + ada.String() + `"}`, alice, http.StatusNoContent},
		{"group role stops at the sibling group", "PATCH", "/orgs/" + slug + "/groups/ops", rename, alice, http.StatusForbidden},
		{"group role stops at the org", "PATCH", "/orgs/" + slug, rename, alice, http.StatusForbidden},
		{"group role stops at a project", "PATCH", "/orgs/" + slug + "/projects/app", rename, alice, http.StatusForbidden},
		{"project role updates its own project", "PATCH", "/orgs/" + slug + "/projects/web", rename, alice, http.StatusOK},
		{"project role stops at the sibling project", "PATCH", "/orgs/" + slug + "/projects/app", rename, alice, http.StatusForbidden},
		{"org member reads a group", "GET", "/orgs/" + slug + "/groups/eng", "", bob, http.StatusOK},
		{"org member does not update a group", "PATCH", "/orgs/" + slug + "/groups/eng", rename, bob, http.StatusForbidden},
		{"org member reads a project", "GET", "/orgs/" + slug + "/projects/web", "", bob, http.StatusOK},
		{"org member does not update a project", "PATCH", "/orgs/" + slug + "/projects/web", rename, bob, http.StatusForbidden},
		{"non-member reads nothing", "GET", "/orgs/" + slug + "/groups/eng", "", stranger, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := f.do(t, c.method, c.path, c.body, c.cookie)
			assert.Equal(t, c.want, rec.Code)
		})
	}
}

func TestScopedPermissions_GroupWithoutRolesGrantsNothing(t *testing.T) {
	f := newFixture(t)
	f.enforce()

	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	alice := f.sessionCookieFor(t, "alice-sub", "Alice")
	slug := createOrg(t, f, owner, "acme")
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)
	aliceGRN := grn.New("root", "default", user.ServiceType, "alice-sub")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, aliceGRN, org.RoleMember)))

	// Two groups alice belongs to, differing only in whether the group
	// carries a role. Reading them together is what separates a group
	// that grants nothing from a route that grants nothing.
	eng := grn.New(slug, "default", org.ServiceTypeGroup, "eng")
	require.NoError(t, f.orgs.CreateGroup(testCtx(t), org.NewGroup(eng, "eng", orgGRN)))
	require.NoError(t, f.orgs.AddGroupMember(testCtx(t), eng, aliceGRN))
	ops := grn.New(slug, "default", org.ServiceTypeGroup, "ops")
	require.NoError(t, f.orgs.CreateGroup(testCtx(t), org.NewGroup(ops, "ops", orgGRN, string(org.RoleAdmin))))
	require.NoError(t, f.orgs.AddGroupMember(testCtx(t), ops, aliceGRN))

	rec := f.do(t, "GET", "/orgs/"+slug+"/groups/eng", "", alice)
	assert.Equal(t, http.StatusOK, rec.Code)
	rec = f.do(t, "PATCH", "/orgs/"+slug+"/groups/eng", `{"name":"renamed"}`, alice)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "PATCH", "/orgs/"+slug+"/groups/ops", `{"name":"renamed"}`, alice)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestScopedPermissions_DirectUserRoles proves roles stored on the
// user are collected as direct grants: they carry the org role's
// scope, unknown names grant nothing, and org membership stays the
// precondition.
func TestScopedPermissions_DirectUserRoles(t *testing.T) {
	f := newFixture(t)
	f.enforce()

	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	alice := f.sessionCookieFor(t, "alice-sub", "Alice")
	bob := f.sessionCookieFor(t, "bob-sub", "Bob")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")

	slug := createOrg(t, f, owner, "acme")
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)
	aliceGRN := grn.New("root", "default", user.ServiceType, "alice-sub")
	bobGRN := grn.New("root", "default", user.ServiceType, "bob-sub")
	strangerGRN := grn.New("root", "default", user.ServiceType, "stranger-sub")
	for _, g := range []grn.GRN{aliceGRN, bobGRN} {
		require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, g, org.RoleMember)))
	}
	f.users.grantRoles(aliceGRN, string(org.RoleAdmin))
	f.users.grantRoles(bobGRN, "wizard")
	f.users.grantRoles(strangerGRN, string(org.RoleOwner))

	require.NoError(t, f.orgs.CreateGroup(testCtx(t),
		org.NewGroup(grn.New(slug, "default", org.ServiceTypeGroup, "eng"), "eng", orgGRN)))

	rename := `{"name":"renamed"}`
	cases := []struct {
		name         string
		method, path string
		body         string
		cookie       *http.Cookie
		want         int
	}{
		{"direct role administers the org's groups", "PATCH", "/orgs/" + slug + "/groups/eng", rename, alice, http.StatusOK},
		{"direct role updates the org", "PATCH", "/orgs/" + slug, rename, alice, http.StatusOK},
		{"direct role stops at deleting the org", "DELETE", "/orgs/" + slug, "", alice, http.StatusForbidden},
		{"unknown direct role grants nothing", "PATCH", "/orgs/" + slug + "/groups/eng", rename, bob, http.StatusForbidden},
		{"direct role does not admit a non-member", "GET", "/orgs/" + slug + "/groups/eng", "", stranger, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := f.do(t, c.method, c.path, c.body, c.cookie)
			assert.Equal(t, c.want, rec.Code)
		})
	}
}

// TestScopedPermissions_RolesAssignedOverHTTP proves the scoped grant
// is reachable through the API alone: a group created over HTTP with
// roles grants them to its members.
func TestScopedPermissions_RolesAssignedOverHTTP(t *testing.T) {
	f := newFixture(t)
	f.enforce()

	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	alice := f.sessionCookieFor(t, "alice-sub", "Alice")
	slug := createOrg(t, f, owner, "acme")
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)
	aliceGRN := grn.New("root", "default", user.ServiceType, "alice-sub")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, aliceGRN, org.RoleMember)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/groups", `{"name":"eng","roles":["admin"]}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	g, err := grn.Parse(decodeBody[apigen.Group](t, rec).Grn)
	require.NoError(t, err)
	path := "/orgs/" + slug + "/groups/" + g.Identifier

	rec = f.do(t, "POST", path+"/members", `{"memberGrn":"`+aliceGRN.String()+`"}`, owner)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = f.do(t, "PATCH", path, `{"name":"platform"}`, alice)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Clearing the roles takes the grant away again.
	rec = f.do(t, "PATCH", path, `{"roles":[]}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	rec = f.do(t, "PATCH", path, `{"name":"eng"}`, alice)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// TestScopedPermissions_MalformedSlugStopsBeforeStatementCollection
// pins where a path segment stops being inert: orgScope splices the
// org identifier into a resource pattern, and '*' and '?' are that
// pattern's wildcards (ADR-0002). The well-formed slug is the control
// — it is answered on the same route, so what the rows below refuse is
// the segment rather than the request.
func TestScopedPermissions_MalformedSlugStopsBeforeStatementCollection(t *testing.T) {
	t.Run("a well-formed slug is answered", func(t *testing.T) {
		f := newFixture(t)
		f.enforce()
		owner := f.sessionCookieFor(t, "owner-sub", "Owner")
		slug := createOrg(t, f, owner, "acme")

		assert.Equal(t, http.StatusOK, f.do(t, "GET", "/orgs/"+slug, "", owner).Code)
	})

	// The wildcards are percent-encoded: a raw '?' opens the query
	// string instead of reaching the path segment.
	malformed := map[string]string{
		"a wildcard":                   "%2A",
		"a single-character wildcard":  "ac%3Fe",
		"an uppercase slug":            "ACME",
		"a slug past the length bound": strings.Repeat("a", 65),
	}
	for name, slug := range malformed {
		t.Run(name+" is refused", func(t *testing.T) {
			// The evaluator carries no expectations, so a request that
			// reaches statement collection fails the test.
			f := newFixture(t)
			owner := f.sessionCookieFor(t, "owner-sub", "Owner")

			assert.Equal(t, http.StatusBadRequest, f.do(t, "GET", "/orgs/"+slug, "", owner).Code)
		})
	}
}

// TestScopedPermissions_NoRoleGrantsWatchingAnAgent covers the grant
// ADR-0022 keeps off the roles. Watching is separate from reading, and
// a grant arriving with membership is one nobody decided, so an
// organization that wants it writes a policy. The owner carries the
// widest vocabulary a role can, and this runs the real evaluator over
// the statements the server collected, so a refusal here is a refusal
// for every role and for every wildcard a role grants.
//
// The same owner reading the same agent is the control: it is what
// stops being answered if the refusal comes from the session, the
// membership, the org or the agent rather than from the action.
func TestScopedPermissions_NoRoleGrantsWatchingAnAgent(t *testing.T) {
	f := newFixture(t)
	f.enforce()

	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	held := seedAgent(t, f, slug)
	// The stream is held open rather than recorded: a grant reaching
	// this endpoint answers by streaming, and a recorder would wait on
	// it instead of reporting the status it was handed.
	server := httptest.NewServer(f.handler)
	t.Cleanup(server.Close)

	refused, _, _ := open(t, server.URL, "/orgs/"+slug+"/agents/"+held.Identifier+"/watch", owner)
	assert.Equal(t, http.StatusForbidden, refused.StatusCode)

	answered := f.do(t, "GET", "/orgs/"+slug+"/agents/"+held.Identifier, "", owner)
	assert.Equal(t, http.StatusOK, answered.Code, "the read is refused too, so the refusal above proves nothing")
}
