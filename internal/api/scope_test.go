package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/api"
	"github.com/garamsh/gaon/internal/authz"
	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
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

	g := grn.New(slug, "default", org.ServiceTypeGroup, "eng")
	require.NoError(t, f.orgs.CreateGroup(testCtx(t), org.NewGroup(g, "eng", orgGRN)))
	require.NoError(t, f.orgs.AddGroupMember(testCtx(t), g, aliceGRN))

	rec := f.do(t, "GET", "/orgs/"+slug+"/groups/eng", "", alice)
	assert.Equal(t, http.StatusOK, rec.Code)
	rec = f.do(t, "PATCH", "/orgs/"+slug+"/groups/eng", `{"name":"renamed"}`, alice)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
