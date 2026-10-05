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
	refused := func(h *host, why string) {
		t.Helper()
		out, err := h.enroll()
		require.Error(t, err, "%s: %s", why, out)
		_, err = os.Stat(filepath.Join(h.agent, "agent.crt"))
		assert.True(t, os.IsNotExist(err), "%s installs no agent certificate", why)
		_, err = os.Stat(filepath.Join(h.supervisor, "supervisor.pem"))
		assert.True(t, os.IsNotExist(err), "%s installs no supervisor certificate", why)
	}

	unapproved := s.newHost("unapproved")
	unapproved.prepare()
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
	out, err := approved.enroll()
	require.NoError(t, err, "the control: the approved preparation enrolls: %s", out)
	for _, path := range []string{filepath.Join(approved.agent, "agent.crt"), filepath.Join(approved.supervisor, "supervisor.pem")} {
		_, err := os.Stat(path)
		assert.NoError(t, err, path)
	}
}
