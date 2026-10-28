package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
)

// createProject creates a project via the API and returns its
// identifier.
func createProject(t *testing.T, f *fixture, admin *http.Cookie, orgSlug, name string) string {
	t.Helper()
	rec := f.do(t, "POST", "/orgs/"+orgSlug+"/projects", `{"name":"`+name+`"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	g, err := grn.Parse(decodeBody[apigen.Project](t, rec).Grn)
	require.NoError(t, err)
	return g.Identifier
}

func TestProjectFlow_RoleEnforcement(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	memberGRN := grn.New("root", "default", user.ServiceType, "member-sub")
	f.permit(
		permission{ownerGRN, org.ActionProjectCreate},
		permission{ownerGRN, org.ActionProjectRead},
		permission{ownerGRN, org.ActionProjectUpdate},
		permission{ownerGRN, org.ActionProjectDelete},
		permission{memberGRN, org.ActionProjectRead},
	)

	member := f.sessionCookieFor(t, "member-sub", "Member")
	orgGRN := grn.Org(slug)
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/projects", `{"name":"web"}`, member)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	project := createProject(t, f, owner, slug, "web")

	rec = f.do(t, "GET", "/orgs/"+slug+"/projects/"+project, "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeBody[apigen.Project](t, rec)
	assert.Equal(t, "web", got.Name)
	assert.Equal(t, "grn:"+slug+":default:project:"+project, got.Grn)
	assert.Equal(t, orgGRN.String(), got.OrgGrn)

	rec = f.do(t, "GET", "/orgs/"+slug+"/projects", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeBody[[]apigen.Project](t, rec), 1)

	rec = f.do(t, "PATCH", "/orgs/"+slug+"/projects/"+project, `{"name":"frontend"}`, member)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/projects/"+project, "", member)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = f.do(t, "PATCH", "/orgs/"+slug+"/projects/"+project, `{"name":"frontend"}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "frontend", decodeBody[apigen.Project](t, rec).Name)

	rec = f.do(t, "DELETE", "/orgs/"+slug+"/projects/"+project, "", owner)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/projects/"+project, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestProjectMembers_Lifecycle(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	memberGRN := grn.New("root", "default", user.ServiceType, "member-sub")
	f.permit(
		permission{ownerGRN, org.ActionProjectCreate},
		permission{ownerGRN, org.ActionProjectMemberAdd},
		permission{ownerGRN, org.ActionProjectMemberRemove},
		permission{ownerGRN, org.ActionProjectMemberList},
		permission{memberGRN, org.ActionProjectMemberList},
	)
	project := createProject(t, f, owner, slug, "web")
	ada := grn.New(slug, "default", user.ServiceType, "ada").String()

	rec := f.do(t, "POST", "/orgs/"+slug+"/projects/"+project+"/members", `{"memberGrn":"`+ada+`"}`, owner)
	require.Equal(t, http.StatusNoContent, rec.Code)

	// A plain org member lists project members but cannot add them.
	member := f.sessionCookieFor(t, "member-sub", "Member")
	orgGRN := grn.Org(slug)
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))

	rec = f.do(t, "GET", "/orgs/"+slug+"/projects/"+project+"/members", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	members := decodeBody[[]apigen.MemberRef](t, rec)
	require.Len(t, members, 1)
	assert.Equal(t, ada, members[0].MemberGrn)

	rec = f.do(t, "POST", "/orgs/"+slug+"/projects/"+project+"/members", `{"memberGrn":"`+ada+`"}`, member)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/projects/"+project+"/members/"+ada, "", member)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = f.do(t, "DELETE", "/orgs/"+slug+"/projects/"+project+"/members/"+ada, "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/projects/"+project+"/members", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[[]apigen.MemberRef](t, rec))

	// The two removals that match nothing, against the one above as
	// their control: it took a membership away and answered 204, and
	// neither of these has one to take.
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/projects/"+project+"/members/"+ada, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "ada is no longer a member")
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/projects/nope/members/"+ada, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "the project does not exist")
}

func TestProjectRoles_AssignedOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(
		permission{ownerGRN, org.ActionProjectCreate},
		permission{ownerGRN, org.ActionProjectUpdate},
	)

	rec := f.do(t, "POST", "/orgs/"+slug+"/projects", `{"name":"web","roles":["admin"]}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.Project](t, rec)
	assert.Equal(t, apigen.ScopedRoles{apigen.Role(org.RoleAdmin)}, created.Roles)
	g, err := grn.Parse(created.Grn)
	require.NoError(t, err)
	path := "/orgs/" + slug + "/projects/" + g.Identifier

	// A patch without roles leaves them as they are.
	rec = f.do(t, "PATCH", path, `{"name":"frontend"}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, apigen.ScopedRoles{apigen.Role(org.RoleAdmin)}, decodeBody[apigen.Project](t, rec).Roles)

	rec = f.do(t, "PATCH", path, `{"roles":["member"]}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, apigen.ScopedRoles{apigen.Role(org.RoleMember)}, decodeBody[apigen.Project](t, rec).Roles)

	// An empty array clears them.
	rec = f.do(t, "PATCH", path, `{"roles":[]}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[apigen.Project](t, rec).Roles)
}

func TestProjectRoles_RejectUnknownRole(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(
		permission{ownerGRN, org.ActionProjectCreate},
		permission{ownerGRN, org.ActionProjectUpdate},
	)

	// Each refusal is read against the same route taking a name
	// org.ParseRole knows: what stops being accepted if it stops
	// parsing anything.
	rec := f.do(t, "POST", "/orgs/"+slug+"/projects", `{"name":"app","roles":["admin"]}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = f.do(t, "POST", "/orgs/"+slug+"/projects", `{"name":"web","roles":["root"]}`, owner)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_argument", decodeBody[apigen.Error](t, rec).Kind)

	project := createProject(t, f, owner, slug, "web")
	rec = f.do(t, "PATCH", "/orgs/"+slug+"/projects/"+project, `{"roles":["member"]}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "PATCH", "/orgs/"+slug+"/projects/"+project, `{"roles":["root"]}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
