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

// TestInvitationRefusals_SayWhichRefusalTheyWere reads the two
// refusals an invitation answers with the same status and kind, on
// both routes that spend one, and finds them told apart by reason. An
// unknown token is the control: a refusal no rule needs to explain
// carries no reason at all.
func TestInvitationRefusals_SayWhichRefusalTheyWere(t *testing.T) {
	f := newFixture(t)
	invitee := f.sessionCookieFor(t, "invitee-sub", "Invitee")
	invitedOrg(t, f, "used-tok", time.Now().UTC().Add(time.Hour))
	require.Equal(t, http.StatusOK, f.do(t, "POST", "/invitations/used-tok/accept", "", invitee).Code)
	creator := grn.New("root", user.ServiceType, "owner-sub")
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t),
		org.NewInvitation("old-tok", orgGRNOf("acme-robotics"), org.RoleMember, creator, time.Now().UTC().Add(-time.Minute))))

	// A second person, so the used token is refused for being spent
	// rather than for a membership the first already holds.
	second := f.sessionCookieFor(t, "second-sub", "Second")
	for _, tc := range []struct {
		name, method, path, body, reason string
	}{
		{"accept, used", "POST", "/invitations/used-tok/accept", "", org.ReasonInvitationAccepted},
		{"accept, expired", "POST", "/invitations/old-tok/accept", "", org.ReasonInvitationExpired},
		{"register, expired", "POST", "/auth/password/register", registerBody("old-tok", "bob", registeredPassword), org.ReasonInvitationExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.do(t, tc.method, tc.path, tc.body, second)
			require.Equal(t, http.StatusConflict, rec.Code)
			body := decodeBody[apigen.Error](t, rec)
			assert.Equal(t, "failed_precondition", body.Kind)
			require.NotNil(t, body.Reason)
			assert.Equal(t, tc.reason, *body.Reason)
			assert.Nil(t, body.Path)
		})
	}

	unknown := decodeBody[apigen.Error](t, f.do(t, "POST", "/invitations/nope/accept", "", invitee))
	assert.Equal(t, "not_found", unknown.Kind)
	assert.Nil(t, unknown.Reason)
}
