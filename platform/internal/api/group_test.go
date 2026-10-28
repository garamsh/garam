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

// createGroup creates a group via the API and returns its identifier.
func createGroup(t *testing.T, f *fixture, admin *http.Cookie, orgSlug, name string) string {
	t.Helper()
	rec := f.do(t, "POST", "/orgs/"+orgSlug+"/groups", `{"name":"`+name+`"}`, admin)
	require.Equal(t, http.StatusCreated, rec.Code)
	g, err := grn.Parse(decodeBody[apigen.Group](t, rec).Grn)
	require.NoError(t, err)
	return g.Identifier
}

func TestGroupFlow_RoleEnforcement(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	memberGRN := grn.New("root", "default", user.ServiceType, "member-sub")
	f.permit(
		permission{ownerGRN, org.ActionGroupCreate},
		permission{ownerGRN, org.ActionGroupRead},
		permission{ownerGRN, org.ActionGroupUpdate},
		permission{ownerGRN, org.ActionGroupDelete},
		permission{memberGRN, org.ActionGroupRead},
	)

	// A member of the org reads groups but cannot change them.
	member := f.sessionCookieFor(t, "member-sub", "Member")
	orgGRN := grn.Org(slug)
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/groups", `{"name":"eng"}`, member)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	group := createGroup(t, f, owner, slug, "eng")

	rec = f.do(t, "GET", "/orgs/"+slug+"/groups/"+group, "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeBody[apigen.Group](t, rec)
	assert.Equal(t, "eng", got.Name)
	assert.Equal(t, "grn:"+slug+":default:group:"+group, got.Grn)
	assert.Equal(t, orgGRN.String(), got.OrgGrn)

	rec = f.do(t, "GET", "/orgs/"+slug+"/groups", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeBody[[]apigen.Group](t, rec), 1)

	rec = f.do(t, "PATCH", "/orgs/"+slug+"/groups/"+group, `{"name":"platform"}`, member)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/groups/"+group, "", member)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = f.do(t, "PATCH", "/orgs/"+slug+"/groups/"+group, `{"name":"platform"}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "platform", decodeBody[apigen.Group](t, rec).Name)

	rec = f.do(t, "DELETE", "/orgs/"+slug+"/groups/"+group, "", owner)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/groups/"+group, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGroupMembers_Lifecycle(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(
		permission{ownerGRN, org.ActionGroupCreate},
		permission{ownerGRN, org.ActionGroupMemberList},
		permission{ownerGRN, org.ActionGroupMemberAdd},
		permission{ownerGRN, org.ActionGroupMemberRemove},
	)
	group := createGroup(t, f, owner, slug, "eng")
	ada := grn.New(slug, "default", user.ServiceType, "ada").String()

	rec := f.do(t, "POST", "/orgs/"+slug+"/groups/"+group+"/members", `{"memberGrn":"`+ada+`"}`, owner)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = f.do(t, "GET", "/orgs/"+slug+"/groups/"+group+"/members", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	members := decodeBody[[]apigen.MemberRef](t, rec)
	require.Len(t, members, 1)
	assert.Equal(t, ada, members[0].MemberGrn)

	// The same member twice conflicts.
	rec = f.do(t, "POST", "/orgs/"+slug+"/groups/"+group+"/members", `{"memberGrn":"`+ada+`"}`, owner)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// A malformed GRN is rejected at the boundary.
	rec = f.do(t, "POST", "/orgs/"+slug+"/groups/"+group+"/members", `{"memberGrn":"ada"}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	// Members of a group that does not exist are a 404, not an empty
	// list.
	rec = f.do(t, "GET", "/orgs/"+slug+"/groups/nope/members", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = f.do(t, "DELETE", "/orgs/"+slug+"/groups/"+group+"/members/"+ada, "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/groups/"+group+"/members", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[[]apigen.MemberRef](t, rec))

	// The two removals that match nothing, against the one above as
	// their control: it took a membership away and answered 204, and
	// neither of these has one to take.
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/groups/"+group+"/members/"+ada, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "ada is no longer a member")
	rec = f.do(t, "DELETE", "/orgs/"+slug+"/groups/nope/members/"+ada, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code, "the group does not exist")
}

// TestGroups_RequireOrgMembership reads one route's three answers
// together: the owner is a member holding the action, and neither
// refusal below is his. Without the first, a listing refused to
// everyone satisfies the other two.
func TestGroups_RequireOrgMembership(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{grn.New("root", "default", user.ServiceType, "owner-sub"), org.ActionGroupRead})

	rec := f.do(t, "GET", "/orgs/"+slug+"/groups", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "GET", "/orgs/"+slug+"/groups", "", stranger)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "permission_denied", decodeBody[apigen.Error](t, rec).Kind)

	rec = f.do(t, "GET", "/orgs/"+slug+"/groups", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGroupRoles_AssignedOverHTTP(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(
		permission{ownerGRN, org.ActionGroupCreate},
		permission{ownerGRN, org.ActionGroupUpdate},
	)

	rec := f.do(t, "POST", "/orgs/"+slug+"/groups", `{"name":"eng","roles":["admin"]}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.Group](t, rec)
	assert.Equal(t, apigen.ScopedRoles{apigen.Role(org.RoleAdmin)}, created.Roles)
	g, err := grn.Parse(created.Grn)
	require.NoError(t, err)
	path := "/orgs/" + slug + "/groups/" + g.Identifier

	// A patch without roles leaves them as they are.
	rec = f.do(t, "PATCH", path, `{"name":"platform"}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, apigen.ScopedRoles{apigen.Role(org.RoleAdmin)}, decodeBody[apigen.Group](t, rec).Roles)

	rec = f.do(t, "PATCH", path, `{"roles":["member"]}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, apigen.ScopedRoles{apigen.Role(org.RoleMember)}, decodeBody[apigen.Group](t, rec).Roles)

	// An empty array clears them.
	rec = f.do(t, "PATCH", path, `{"roles":[]}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[apigen.Group](t, rec).Roles)
}

func TestGroupRoles_RejectUnknownRole(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(
		permission{ownerGRN, org.ActionGroupCreate},
		permission{ownerGRN, org.ActionGroupUpdate},
	)

	// Each refusal is read against the same route taking a name
	// org.ParseRole knows: what stops being accepted if it stops
	// parsing anything.
	rec := f.do(t, "POST", "/orgs/"+slug+"/groups", `{"name":"ops","roles":["admin"]}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = f.do(t, "POST", "/orgs/"+slug+"/groups", `{"name":"eng","roles":["root"]}`, owner)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_argument", decodeBody[apigen.Error](t, rec).Kind)

	group := createGroup(t, f, owner, slug, "eng")
	rec = f.do(t, "PATCH", "/orgs/"+slug+"/groups/"+group, `{"roles":["member"]}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "PATCH", "/orgs/"+slug+"/groups/"+group, `{"roles":["root"]}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
