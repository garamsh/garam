// Internal test package: what the intent records is the supervisor's
// own state, and no exported surface reaches an intent whose control
// token is empty or malformed — the drain in recover keeps one from
// being served. The guard on the presented token must hold without
// that, so the test puts the record there itself.
package workload

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
)

// fixedTokens is an agent token source the caller check never reads.
type fixedTokens struct{}

func (fixedTokens) Current(context.Context) (string, error) { return "", nil }

// TestCaller_RefusesAControlTokenThatIsNotOneBeforeComparing holds the
// token mode's caller check to the presented token's spelling: an empty
// token against an empty record, and a malformed one against the same
// malformed record, are each refused with 401 although the
// constant-time comparison alone would accept them. A well-formed
// token matching the record is the control.
func TestCaller_RefusesAControlTokenThatIsNotOneBeforeComparing(t *testing.T) {
	agent := grn.New("acme", grn.DefaultProject, "agent", "0a1b2c3d4e5f6071")
	s := &Supervisor{cfg: Config{Tokens: fixedTokens{}, Desired: Desired{Agent: agent}}}
	wellFormed := controlTokenPrefix + strings.Repeat("a", 64)
	for name, c := range map[string]struct {
		recorded, presented string
		want                int
	}{
		"an empty token against an empty record":        {"", "", http.StatusUnauthorized},
		"a malformed token against the same record":     {"garam_at_xyz", "garam_at_xyz", http.StatusUnauthorized},
		"the control: a well-formed token that matches": {wellFormed, wellFormed, http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/agents/"+agent.String()+"/activations", nil)
			r.SetPathValue("agent", agent.String())
			r.Header.Set("Authorization", "Garam-Token "+c.presented)
			w := httptest.NewRecorder()
			_, ok := s.caller(w, r, &intent{ControlToken: c.recorded})
			assert.Equal(t, c.want == http.StatusOK, ok)
			assert.Equal(t, c.want, w.Code)
		})
	}
}

// TestControlServer_CloseLeavesNoListenerBehindForTheNextGeneration is
// #1420's cause made certain: a generation that fails before its
// control's serving goroutine ever ran. On one processor that goroutine
// has not run when close is called, which the test checks. Closing the
// server alone leaves the listener open, and the goroutine, once it
// runs, closes it and removes the socket path, by then the next
// control's socket, so a supervisor run after a failed one loses its
// control or fails to make it. close waits for that goroutine, so the
// socket is removed and the goroutine has returned when close does, and
// the next control's socket survives the scheduler running everything
// left behind; reaching it is the control.
func TestControlServer_CloseLeavesNoListenerBehindForTheNextGeneration(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	// A unix socket's path must fit its address, which a test's own
	// temporary directory may not.
	dir, err := os.MkdirTemp("", "wl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	genDir := filepath.Join(dir, "generation")
	serving, _, err := servingMaterial(genDir)
	require.NoError(t, err)
	s := &Supervisor{}
	path := filepath.Join(genDir, socketDir, socketName)

	first, err := s.serveControl(filepath.Join(genDir, socketDir), serving, &intent{})
	require.NoError(t, err)
	// The precondition: the serving goroutine has not tracked the
	// listener yet, so closing the server alone leaves the socket.
	require.NoError(t, first.srv.Close())
	_, err = os.Stat(path)
	require.NoError(t, err, "precondition: the serving goroutine had not run when close was called")
	first.close()
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist, "close removed the socket before returning")
	select {
	case <-first.served:
	default:
		require.Fail(t, "close returned while the serving goroutine had not")
	}

	next, err := s.serveControl(filepath.Join(genDir, socketDir), serving, &intent{})
	require.NoError(t, err)
	t.Cleanup(next.close)
	for range 100 {
		runtime.Gosched()
	}
	_, err = os.Stat(path)
	require.NoError(t, err, "nothing left of the first control removed the next one's socket")
	conn, err := net.Dial("unix", path)
	require.NoError(t, err, "the control: the next control answers on its socket")
	_ = conn.Close()
}
