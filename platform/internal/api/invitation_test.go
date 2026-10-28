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
