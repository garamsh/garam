package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// consentActions is every action the consent endpoints enforce.
var consentActions = []string{
	org.ActionConsentRead,
	org.ActionConsentGrant,
	org.ActionConsentWithdraw,
}

// TestConsents_AdminManagesTheRelation carries an org admin through
// the relation's lifecycle over the API (ADR-0035): a grant stands the
// org's own half, the relation stands once the other organization has
// granted its own, and a withdraw takes the org's half down again
// while the other's keeps standing. The other organization's grant is
// written through the store directly, the way its own admin's API
// call would write it.
func TestConsents_AdminManagesTheRelation(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	perms := make([]permission, 0, len(consentActions))
	for _, a := range consentActions {
		perms = append(perms, permission{ownerGRN, a})
	}
	f.permit(perms...)

	// The other organization exists; its half is stood by its own
	// admin below, not by anything this org does.
	otherGRN := orgGRNForTest("globex")
	require.NoError(t, f.orgs.CreateOrg(testCtx(t), org.NewOrganization(otherGRN, "globex")))

	base := "/orgs/" + slug + "/consents"

	// Nothing relates the two yet.
	rec := f.do(t, "GET", base, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[[]apigen.OrgConsent](t, rec))

	// Granting stands this org's half alone.
	rec = f.do(t, "PUT", base+"/globex", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	granted := decodeBody[apigen.OrgConsent](t, rec)
	assert.Equal(t, otherGRN.String(), granted.OrgGrn)
	assert.True(t, granted.GrantedHere)
	assert.False(t, granted.GrantedThere)
	assert.False(t, granted.Stands, "one half does not carry a message across")

	// The other org's own grant is what makes the relation stand.
	_, err := f.consents.Grant(testCtx(t), otherGRN, orgGRNForTest(slug))
	require.NoError(t, err)
	rec = f.do(t, "GET", base, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	listed := decodeBody[[]apigen.OrgConsent](t, rec)
	require.Len(t, listed, 1)
	assert.True(t, listed[0].Stands)

	// Re-granting is accepted and changes nothing.
	rec = f.do(t, "PUT", base+"/globex", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, decodeBody[apigen.OrgConsent](t, rec).Stands)

	// Withdrawing takes only this org's half down: the relation falls
	// and the other half keeps standing for a re-grant to join.
	rec = f.do(t, "DELETE", base+"/globex", "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", base, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	listed = decodeBody[[]apigen.OrgConsent](t, rec)
	require.Len(t, listed, 1)
	assert.False(t, listed[0].GrantedHere)
	assert.True(t, listed[0].GrantedThere, "the other org's half was not this org's to withdraw")
	assert.False(t, listed[0].Stands)
}

// TestConsents_MemberIsDenied holds the management surface to the
// admin role: a member of the org is refused all three routes, beside
// the owner taking one of them — the accepted call that isolates the
// role as what refuses.
func TestConsents_MemberIsDenied(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	orgGRN := orgGRNForTest(slug)
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	memberGRN := grn.New("root", "default", user.ServiceType, "member-sub")
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))
	// The owner holds the grant action and the member holds nothing.
	f.permit(permission{ownerGRN, org.ActionConsentGrant})

	otherGRN := orgGRNForTest("globex")
	require.NoError(t, f.orgs.CreateOrg(testCtx(t), org.NewOrganization(otherGRN, "globex")))

	base := "/orgs/" + slug + "/consents"
	rec := f.do(t, "PUT", base+"/globex", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)

	for _, tc := range []struct{ method, path string }{
		{"GET", base},
		{"PUT", base + "/globex"},
		{"DELETE", base + "/globex"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := f.do(t, tc.method, tc.path, "", member)
			assert.Equal(t, http.StatusForbidden, rec.Code)
		})
	}
}

// TestConsents_RequireOrgMembership reads one route's three answers
// together: the member holding the action is answered, the signed-in
// outsider is refused, and the anonymous caller is unauthenticated.
func TestConsents_RequireOrgMembership(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionConsentRead})

	path := "/orgs/" + slug + "/consents"
	rec := f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "GET", path, "", outsider)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "permission_denied", decodeBody[apigen.Error](t, rec).Kind)

	rec = f.do(t, "GET", path, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestConsents_RefusesAnOrgThatIsNotThereOrIsItself covers the two
// grants that cannot stand: toward an organization that does not
// exist, and toward the granting organization itself. The accepted
// grant beside them is the control — the surface grants where it
// should, so each refusal is its own case rather than a surface that
// refuses everything.
func TestConsents_RefusesAnOrgThatIsNotThereOrIsItself(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionConsentGrant})

	otherGRN := orgGRNForTest("globex")
	require.NoError(t, f.orgs.CreateOrg(testCtx(t), org.NewOrganization(otherGRN, "globex")))

	base := "/orgs/" + slug + "/consents"
	rec := f.do(t, "PUT", base+"/globex", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "PUT", base+"/no-such-org", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = f.do(t, "PUT", base+"/"+slug, "", owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// orgGRNForTest builds the GRN of the org the slug names, as the
// surface's own helper does.
func orgGRNForTest(slug string) grn.GRN {
	return grn.New("root", "default", org.ServiceTypeOrg, slug)
}
