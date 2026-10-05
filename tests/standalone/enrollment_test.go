package standalone_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStandalone_EnrollsOnlyAnApprovedHost holds the shipped `garam
// standalone enroll` to an administrator's current approval of the
// host's own keys (#1157 scenario 2): a preparation nobody approved, an
// approval past its expiry and a withdrawn one each refuse and install
// no certificate. The control is the approved preparation, which
// enrolls and installs both.
func TestStandalone_EnrollsOnlyAnApprovedHost(t *testing.T) {
	s := newStack(t)
	// What garam answers every bootstrap no current approval holds
	// (agent.ErrBootstrapRefused), as the shipped CLI renders it: the
	// status and garam's own words, so a failure of anything else
	// cannot pass for the refusal.
	const refusal = "garam answered 401 Unauthorized: standalone bootstrap is unknown, expired or withdrawn"
	refused := func(h *host, why string) {
		t.Helper()
		out, err := h.enroll()
		require.Error(t, err, "%s: %s", why, out)
		assert.Contains(t, out, refusal, "%s is garam's refusal", why)
		_, err = os.Stat(filepath.Join(h.agent, "agent.crt"))
		assert.True(t, os.IsNotExist(err), "%s installs no agent certificate", why)
		_, err = os.Stat(filepath.Join(h.supervisor, "supervisor.pem"))
		assert.True(t, os.IsNotExist(err), "%s installs no supervisor certificate", why)
	}

	unapproved := s.newHost("unapproved")
	unapproved.prepare()
	// The assertion's own control: the same preparation failing for
	// another reason, a server root that does not verify garam, is not
	// read as garam's refusal.
	root := s.rootFile
	s.rootFile = s.file("other-root.pem", string(otherRoot(t)))
	out, err := unapproved.enroll()
	require.Error(t, err)
	assert.NotContains(t, out, refusal, "a failure that is not garam's refusal reads differently: %s", out)
	s.rootFile = root
	refused(unapproved, "an unapproved preparation")

	expired := s.newHost("expired")
	expired.prepare()
	expired.approve(time.Now().Add(3 * time.Second))
	time.Sleep(4 * time.Second)
	refused(expired, "an expired approval")

	withdrawn := s.newHost("withdrawn")
	withdrawn.prepare()
	id := withdrawn.approve(time.Now().Add(time.Hour))
	status, body := s.browse(http.MethodDelete, "/orgs/"+s.org+"/agent-enrollments/"+id, "")
	require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, status, body)
	refused(withdrawn, "a withdrawn approval")

	approved := s.newHost("approved")
	approved.prepare()
	approved.approve(time.Now().Add(time.Hour))
	out, err = approved.enroll()
	require.NoError(t, err, "the control: the approved preparation enrolls: %s", out)
	for _, path := range []string{filepath.Join(approved.agent, "agent.crt"), filepath.Join(approved.supervisor, "supervisor.pem")} {
		_, err := os.Stat(path)
		assert.NoError(t, err, path)
	}
}

// otherRoot is a root certificate nothing here signed with.
func otherRoot(t *testing.T) []byte {
	t.Helper()
	_, _, rootPEM := mintRoot(t)
	return rootPEM
}
