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
// standalone enroll` to an administrator's current approval (#1157
// scenario 2), under standalone-enrollment.v2 (ADR-0100): a preparation
// nobody approved, an approval past its expiry and a withdrawn one each
// refuse and install no credential. The control is the approved
// preparation, which enrolls and installs the supervisor's credential
// owner-only.
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
		_, err = os.Stat(filepath.Join(h.supervisor, "credential"))
		assert.True(t, os.IsNotExist(err), "%s installs no credential", why)
		_, err = os.Stat(filepath.Join(h.supervisor, "enrollment.json"))
		assert.True(t, os.IsNotExist(err), "%s records no enrollment", why)
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
	info, err := os.Stat(filepath.Join(approved.supervisor, "credential"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	_, err = os.Stat(filepath.Join(approved.agent, "agent.crt"))
	assert.True(t, os.IsNotExist(err), "a v2 enrollment signs no certificate")
}

// otherRoot is a root certificate nothing here signed with.
func otherRoot(t *testing.T) []byte {
	t.Helper()
	_, _, rootPEM := mintRoot(t)
	return rootPEM
}
