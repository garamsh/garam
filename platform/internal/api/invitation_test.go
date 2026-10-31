package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
)

// invitedOrg creates an organization named "Acme Robotics" and an
// invitation into it that expires at expiresAt, answering the org's
// identifier and the token.
func invitedOrg(t *testing.T, f *fixture, token string, expiresAt time.Time) string {
	t.Helper()
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "Acme Robotics")
	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	inv := org.NewInvitation(token, orgGRNOf(slug), org.RoleAdmin, creator, expiresAt)
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t), inv))
	return slug
}

// TestGetInvitation_NamesTheOrganizationAndRoleWithoutASession reads a
// live invitation with no cookie at all: possession of the token is
// the whole claim (ADR-0006), so the answer cannot wait on an account
// the holder may not have yet.
func TestGetInvitation_NamesTheOrganizationAndRoleWithoutASession(t *testing.T) {
	f := newFixture(t)
	expires := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	slug := invitedOrg(t, f, "live-tok", expires)

	rec := f.do(t, "GET", "/invitations/live-tok", "")

	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeBody[apigen.InvitationPreview](t, rec)
	assert.Equal(t, apigen.InvitationPreview{
		Org:       slug,
		OrgName:   "Acme Robotics",
		Role:      apigen.RoleAdmin,
		State:     apigen.InvitationPreviewStatePending,
		ExpiresAt: expires,
	}, got)
	assert.JSONEq(t,
		`{"org":"`+slug+`","orgName":"Acme Robotics","role":"admin","state":"pending","expiresAt":"`+expires.Format(time.RFC3339)+`"}`,
		rec.Body.String(),
		"the answer carries these five fields and nothing about the inviter or the members")
}

// TestGetInvitation_SaysAnUnusedInvitationPastItsExpiryIsExpired holds
// the expired state apart from the pending one; the accepted state is
// the store's to record, so tests/integration reads it from Postgres
// (TestGetInvitation_ReadsEachStateFromPostgres). The accept
// operation's refusal cannot tell expired and accepted apart: both are
// failed_precondition there.
func TestGetInvitation_SaysAnUnusedInvitationPastItsExpiryIsExpired(t *testing.T) {
	f := newFixture(t)
	invitedOrg(t, f, "old-tok", time.Now().UTC().Add(-time.Minute))

	rec := f.do(t, "GET", "/invitations/old-tok", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, apigen.InvitationPreviewStateExpired, decodeBody[apigen.InvitationPreview](t, rec).State)
}

// TestGetInvitation_AnswersAnUnknownTokenAsAnyNotFound compares the
// unknown token's body against the accept operation's answer to the
// same token, so a preview that told an unknown token apart from the
// accept path would fail here. An invitation whose organization is
// gone admits to nothing and is answered with the same body, rather
// than with a sentence saying the organization is missing.
func TestGetInvitation_AnswersAnUnknownTokenAsAnyNotFound(t *testing.T) {
	f := newFixture(t)
	invitee := f.sessionCookieFor(t, "invitee-sub", "Invitee")
	accept := f.do(t, "POST", "/invitations/nope/accept", "", invitee)
	require.Equal(t, http.StatusNotFound, accept.Code)

	unknown := f.do(t, "GET", "/invitations/nope", "")
	require.Equal(t, http.StatusNotFound, unknown.Code)
	assert.JSONEq(t, accept.Body.String(), unknown.Body.String())

	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	orphan := org.NewInvitation("orphan-tok", orgGRNOf("gone"), org.RoleMember, creator, time.Now().Add(time.Hour))
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t), orphan))
	gone := f.do(t, "GET", "/invitations/orphan-tok", "")
	require.Equal(t, http.StatusNotFound, gone.Code)
	assert.JSONEq(t, accept.Body.String(), gone.Body.String())
}

// acmeInvitations creates an organization with an owner, a member and
// a pending invitation, answering the org's identifier, the two
// cookies and the invitation.
func acmeInvitations(t *testing.T, f *fixture) (string, *http.Cookie, *http.Cookie, *org.Invitation) {
	t.Helper()
	f.enforce()
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "Acme Robotics")
	member := f.sessionCookieFor(t, "member-sub", "Member")
	memberGRN := grn.New("root", "default", user.ServiceType, "member-sub")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), memberGRN, org.RoleMember)))
	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	inv := org.NewInvitation("pending-tok", orgGRNOf(slug), org.RoleAdmin, creator, time.Now().UTC().Add(time.Hour))
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t), inv))
	return slug, owner, member, inv
}

// TestListInvitations_NamesEachPendingOneAndNeverItsToken reads the
// list as the owner: the pending invitation is named by its id, the
// spent and the lapsed are left out, and no token reaches the answer.
func TestListInvitations_NamesEachPendingOneAndNeverItsToken(t *testing.T) {
	f := newFixture(t)
	slug, owner, _, inv := acmeInvitations(t, f)
	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t),
		org.NewInvitation("lapsed-tok", orgGRNOf(slug), org.RoleMember, creator, time.Now().UTC().Add(-time.Minute))))
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t),
		org.NewInvitation("spent-tok", orgGRNOf(slug), org.RoleMember, creator, time.Now().UTC().Add(time.Hour))))
	joiner := f.sessionCookieFor(t, "joiner-sub", "Joiner")
	require.Equal(t, http.StatusOK, f.do(t, "POST", "/invitations/spent-tok/accept", "", joiner).Code)

	rec := f.do(t, "GET", "/orgs/"+slug+"/invitations", "", owner)

	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeBody[[]apigen.PendingInvitation](t, rec)
	require.Len(t, got, 1)
	assert.Equal(t, inv.ID.String(), got[0].Id.String())
	assert.Equal(t, apigen.RoleAdmin, got[0].Role)
	assert.Equal(t, inv.CreatedBy.String(), got[0].CreatedByGrn)
	assert.NotContains(t, rec.Body.String(), "-tok", "no token, of any invitation, is listed")
}

// TestInvitationManagement_RefusesAMemberWhomTheRoleGrantsNoInvite asks
// both operations as a member, who holds no org:invite, and as the
// owner, who does, so the refusal is the action's.
func TestInvitationManagement_RefusesAMemberWhomTheRoleGrantsNoInvite(t *testing.T) {
	f := newFixture(t)
	slug, owner, member, inv := acmeInvitations(t, f)
	path := "/orgs/" + slug + "/invitations"

	assert.Equal(t, http.StatusForbidden, f.do(t, "GET", path, "", member).Code)
	assert.Equal(t, http.StatusForbidden, f.do(t, "DELETE", path+"/"+inv.ID.String(), "", member).Code)
	assert.True(t, f.orgs.holdsInvitation(inv.Token), "the refused revocation deleted nothing")

	assert.Equal(t, http.StatusOK, f.do(t, "GET", path, "", owner).Code)
	assert.Equal(t, http.StatusNoContent, f.do(t, "DELETE", path+"/"+inv.ID.String(), "", owner).Code)
}

// TestRevokeInvitation_AnswersItsTokenAsUnknown revokes a live
// invitation and then presents its token: the preview and the accept
// each answer 404, where the same token answered 200 before.
func TestRevokeInvitation_AnswersItsTokenAsUnknown(t *testing.T) {
	f := newFixture(t)
	slug, owner, _, inv := acmeInvitations(t, f)
	invitee := f.sessionCookieFor(t, "invitee-sub", "Invitee")
	require.Equal(t, http.StatusOK, f.do(t, "GET", "/invitations/"+inv.Token, "").Code)

	rec := f.do(t, "DELETE", "/orgs/"+slug+"/invitations/"+inv.ID.String(), "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	assert.Equal(t, http.StatusNotFound, f.do(t, "GET", "/invitations/"+inv.Token, "").Code)
	assert.Equal(t, http.StatusNotFound, f.do(t, "POST", "/invitations/"+inv.Token+"/accept", "", invitee).Code)
	assert.Equal(t, http.StatusNotFound,
		f.do(t, "DELETE", "/orgs/"+slug+"/invitations/"+inv.ID.String(), "", owner).Code,
		"it is gone, so a second revocation finds nothing")
}

// TestRevokeInvitation_RefusesOneAlreadyUsedByItsReason answers a
// revocation after the accept with the refusal that says why, and the
// member stays.
func TestRevokeInvitation_RefusesOneAlreadyUsedByItsReason(t *testing.T) {
	f := newFixture(t)
	slug, owner, _, inv := acmeInvitations(t, f)
	invitee := f.sessionCookieFor(t, "invitee-sub", "Invitee")
	require.Equal(t, http.StatusOK, f.do(t, "POST", "/invitations/"+inv.Token+"/accept", "", invitee).Code)

	rec := f.do(t, "DELETE", "/orgs/"+slug+"/invitations/"+inv.ID.String(), "", owner)

	require.Equal(t, http.StatusConflict, rec.Code)
	reason := decodeBody[apigen.Error](t, rec).Reason
	require.NotNil(t, reason)
	assert.Equal(t, org.ReasonInvitationAccepted, *reason)
}

// TestRevokeInvitation_ReachesOnlyTheOrganizationInThePath presents
// one organization's id under another's path as that one's owner, and
// the id stays where it was.
func TestRevokeInvitation_ReachesOnlyTheOrganizationInThePath(t *testing.T) {
	f := newFixture(t)
	_, owner, _, inv := acmeInvitations(t, f)
	other := createOrg(t, f, owner, "Globex")

	rec := f.do(t, "DELETE", "/orgs/"+other+"/invitations/"+inv.ID.String(), "", owner)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.True(t, f.orgs.holdsInvitation(inv.Token))
}
