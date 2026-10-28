package cli

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/workload"
)

// tokenGaram is garam's exchange, mint and activation as a v2
// supervisor meets them, recording what each call carried.
type tokenGaram struct {
	mu        sync.Mutex
	life      time.Duration
	calls     []*http.Request
	bodies    []string
	exchanged int
	minted    int
	// refuseMint answers mints 401 while set, and failMint 503.
	refuseMint, failMint bool
}

func (g *tokenGaram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, r)
	g.bodies = append(g.bodies, string(body))
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/tokens":
		g.exchanged++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(machinegen.MachineToken{
			Token: fmt.Sprintf("garam_st_%064x", g.exchanged), ExpiresAt: time.Now().Add(g.life),
		})
	case strings.HasSuffix(r.URL.Path, "/tokens"):
		if g.refuseMint {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(machinegen.Error{Kind: "unauthenticated", Message: "token is not valid"})
			return
		}
		if g.failMint {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		g.minted++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(machinegen.AgentToken{
			Token: fmt.Sprintf("garam_at_%064x", g.minted), Grn: testStandaloneAgent.String(), ExpiresAt: time.Now().Add(g.life),
		})
	default:
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(machinegen.Activation{
			ActivationId: "activation-1", Token: "execution", TokenVersion: 1, Grn: testStandaloneAgent.String(),
		})
	}
}

func (g *tokenGaram) set(f func(*tokenGaram)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f(g)
}

func (g *tokenGaram) counts() (exchanged, minted int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.exchanged, g.minted
}

var testStandaloneAgent = grn.New("acme", grn.DefaultProject, "agent", "0a1b2c3d4e5f6071")

// testCredential is a supervisor credential as garam spells one.
var testCredential = "garam_sc_" + strings.Repeat("c", 64)

// newTokenSession is a v2 supervisor's directory over g: its server
// root and its owner-only credential.
func newTokenSession(t *testing.T, g *tokenGaram) *supervisorTokens {
	t.Helper()
	server := httptest.NewTLSServer(g)
	t.Cleanup(server.Close)
	dir := t.TempDir()
	root := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	require.NoError(t, os.WriteFile(filepath.Join(dir, serverRootFile), root, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, credentialFile), []byte(testCredential), 0o600))
	session, err := newSupervisorTokens(dir, server.URL, testStandaloneAgent)
	require.NoError(t, err)
	return session
}

// TestSupervisorTokens_ExchangeAndMintInsideTheRefreshWindow holds a
// v2 supervisor to ADR-0100's refresh: the credential is exchanged for
// the supervisor's token and that token mints the agent's, each held
// until a point inside two thirds to three quarters of its life and
// replaced after it. No credential or token is in any URL.
func TestSupervisorTokens_ExchangeAndMintInsideTheRefreshWindow(t *testing.T) {
	// Internal: run reaches these only after garam enrolls the host.
	g := &tokenGaram{life: 400 * time.Millisecond}
	session := newTokenSession(t, g)

	first, err := session.Current(context.Background())
	require.NoError(t, err)
	again, err := session.Current(context.Background())
	require.NoError(t, err)
	assert.Equal(t, first, again, "the control: a token inside its window is held")
	exchanged, minted := g.counts()
	assert.Equal(t, 1, exchanged)
	assert.Equal(t, 1, minted)

	time.Sleep(310 * time.Millisecond)
	next, err := session.Current(context.Background())
	require.NoError(t, err)
	assert.NotEqual(t, first, next, "past three quarters of its life the agent token is minted again")
	exchanged, minted = g.counts()
	assert.Equal(t, 2, exchanged, "and the supervisor's own is exchanged again")
	assert.Equal(t, 2, minted)

	g.mu.Lock()
	defer g.mu.Unlock()
	assert.Equal(t, "Garam-Credential "+testCredential, g.calls[0].Header.Get("Authorization"))
	assert.Equal(t, "machine-token.v1", g.calls[0].Header.Get("Garam-Contract-Version"))
	assert.Equal(t, "/agents/"+testStandaloneAgent.String()+"/tokens", g.calls[1].URL.Path)
	assert.Equal(t, "Garam-Token garam_st_"+fmt.Sprintf("%064x", 1), g.calls[1].Header.Get("Authorization"))
	for _, call := range g.calls {
		assert.NotContains(t, call.URL.String(), "garam_", "no secret in a URL")
	}
}

// TestSupervisorTokens_KeepsALiveTokenThroughAFailedRefresh holds an
// unanswered refresh to the token still live, and a refused supervisor
// token to an exchange afresh: the mint that follows is the control.
func TestSupervisorTokens_KeepsALiveTokenThroughAFailedRefresh(t *testing.T) {
	// Internal: run reaches these only after garam enrolls the host.
	g := &tokenGaram{life: 400 * time.Millisecond}
	session := newTokenSession(t, g)
	first, err := session.Current(context.Background())
	require.NoError(t, err)

	g.set(func(g *tokenGaram) { g.failMint = true })
	time.Sleep(310 * time.Millisecond)
	held, err := session.Current(context.Background())
	require.NoError(t, err, "a live token outlasts a refresh garam did not answer")
	assert.Equal(t, first, held)

	time.Sleep(150 * time.Millisecond)
	_, err = session.Current(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "an expired one is not handed out: %v", err)

	g.set(func(g *tokenGaram) { g.failMint, g.refuseMint = false, true })
	_, err = session.Current(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), "%v", err)
	exchanged, _ := g.counts()
	g.set(func(g *tokenGaram) { g.refuseMint = false })
	_, err = session.Current(context.Background())
	require.NoError(t, err, "the control: the next mint succeeds")
	again, _ := g.counts()
	assert.Equal(t, exchanged+1, again, "a refused supervisor token is exchanged afresh")
}

// TestSupervisorTokens_ActivatesUnderExecutionFenceV2 covers the
// supervisor's activation in the token mode: its own token in
// Authorization, execution-fence.v2, and the agent token it forwards in
// the body in place of a certificate.
func TestSupervisorTokens_ActivatesUnderExecutionFenceV2(t *testing.T) {
	// Internal: run reaches these only after garam enrolls the host.
	g := &tokenGaram{life: time.Hour}
	session := newTokenSession(t, g)
	token, err := session.Current(context.Background())
	require.NoError(t, err)

	activation, err := session.Activate(context.Background(), workload.ActivationRequest{
		RequestID: "request-1", Epoch: "3", Generation: strings.Repeat("a", 32), Token: token,
	})
	require.NoError(t, err)
	assert.Equal(t, "activation-1", activation.ID)

	g.mu.Lock()
	defer g.mu.Unlock()
	last := len(g.calls) - 1
	assert.Equal(t, "execution-fence.v2", g.calls[last].Header.Get("Garam-Contract-Version"))
	assert.True(t, strings.HasPrefix(g.calls[last].Header.Get("Authorization"), "Garam-Token garam_st_"))
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(g.bodies[last]), &body))
	assert.Equal(t, token, body["token"])
	assert.NotContains(t, body, "certificatePem")
}

// TestNewSupervisorTokens_ReadsTheCredentialOwnerOnly refuses a
// credential file others can read, as every key file is (ADR-0015);
// the owner-only file beside it is the control.
func TestNewSupervisorTokens_ReadsTheCredentialOwnerOnly(t *testing.T) {
	// Internal: run reaches these only after garam enrolls the host.
	server := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	dir := t.TempDir()
	root := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	require.NoError(t, os.WriteFile(filepath.Join(dir, serverRootFile), root, 0o644))
	credential := filepath.Join(dir, credentialFile)
	require.NoError(t, os.WriteFile(credential, []byte(testCredential), 0o640))
	_, err := newSupervisorTokens(dir, server.URL, testStandaloneAgent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "supervisor credential")
	assert.NotContains(t, err.Error(), testCredential)

	require.NoError(t, os.Chmod(credential, 0o600))
	_, err = newSupervisorTokens(dir, server.URL, testStandaloneAgent)
	require.NoError(t, err, "the control: the same file owner-only")
}

// TestSupervisorTokens_DropsTheAgentTokenGaramRefusedToRenew holds a
// refused mint to dropping the token held: a refusal is a revocation
// or a replacement, which refuses that token too, so it is not handed
// out for the rest of its life. A mint garam did not answer, which
// keeps the live token, is the control.
func TestSupervisorTokens_DropsTheAgentTokenGaramRefusedToRenew(t *testing.T) {
	// Internal: run reaches these only after garam enrolls the host.
	g := &tokenGaram{life: 400 * time.Millisecond}
	session := newTokenSession(t, g)
	first, err := session.Current(context.Background())
	require.NoError(t, err)

	time.Sleep(310 * time.Millisecond)
	g.set(func(g *tokenGaram) { g.failMint = true })
	held, err := session.Current(context.Background())
	require.NoError(t, err, "the control: an unanswered refresh keeps the live token")
	assert.Equal(t, first, held)

	g.set(func(g *tokenGaram) { g.failMint, g.refuseMint = false, true })
	_, err = session.Current(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), "%v", err)
	_, err = session.Current(context.Background())
	require.Error(t, err, "the refused token is not handed out while it is still live")
}
