package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
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
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)
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
}

func TestGroups_RequireOrgMembership(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	slug := createOrg(t, f, owner, "acme")

	rec := f.do(t, "GET", "/orgs/"+slug+"/groups", "", stranger)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "permission_denied", decodeBody[apigen.Error](t, rec).Kind)

	rec = f.do(t, "GET", "/orgs/"+slug+"/groups", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
