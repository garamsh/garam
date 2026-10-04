package integration_test

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/grn"
)

// standaloneCommand runs `garam standalone args...` as the CLI ships it
// and answers what it printed.
func standaloneCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := cli.NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"standalone"}, args...))
	err := root.Execute()
	return out.String(), err
}

// hostPaths is a standalone host's two directories and the garam
// server root it was handed out of band.
type hostPaths struct {
	supervisor string
	agent      string
	root       string
}

func newHostPaths(t *testing.T, s *executionStack) hostPaths {
	t.Helper()
	base := t.TempDir()
	h := hostPaths{supervisor: filepath.Join(base, "supervisor"), agent: filepath.Join(base, "agent"), root: filepath.Join(base, "root.pem")}
	presented, ok := s.config.Certificate.(listenerCertificate)
	require.True(t, ok, "the stack presents a loopback certificate")
	require.NoError(t, os.WriteFile(h.root, certificatePEM(presented), 0o644))
	return h
}

func (h hostPaths) prepare(t *testing.T) map[string]string {
	t.Helper()
	out, err := standaloneCommand(t, "prepare", "--dir", h.supervisor, "--agent-dir", h.agent)
	require.NoError(t, err, out)
	var request map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &request))
	return request
}

func (h hostPaths) enroll(t *testing.T, s *executionStack) (string, error) {
	t.Helper()
	return standaloneCommand(t, "enroll", "--dir", h.supervisor, "--agent-dir", h.agent,
		"--machine-url", s.machine, "--server-root-file", h.root)
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info.Mode().Perm()
}

// TestStandaloneHost_PrepareEnrollAndRenewRecoverFromInterruption
// drives the shipped command against the machine listener: a repeated
// prepare answers the same request, a repeated enrollment installs the
// result garam recorded, and a renewal interrupted before its request,
// after garam answered it, and after it was installed each completes
// without a second successor.
func TestStandaloneHost_PrepareEnrollAndRenewRecoverFromInterruption(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newHostPaths(t, s)

	request := h.prepare(t)
	assert.Equal(t, request, h.prepare(t), "a repeated prepare answers the request already bound to its keys")
	assert.NotContains(t, request, "secret", "only the secret's hash is printed")
	for _, path := range []string{filepath.Join(h.supervisor, "prepared.json"), filepath.Join(h.agent, "agent.key")} {
		assert.Equal(t, os.FileMode(0o600), mode(t, path), path)
	}

	// Spending before the approval exists is refused and installs
	// nothing.
	out, err := h.enroll(t, s)
	require.Error(t, err, out)
	assert.NoFileExists(t, filepath.Join(h.supervisor, "supervisor.pem"))

	approveHost(t, s, request)

	out, err = h.enroll(t, s)
	require.NoError(t, err, out)
	first, err := os.ReadFile(filepath.Join(h.supervisor, "supervisor.pem"))
	require.NoError(t, err)

	// Interrupted before the enrollment was recorded: a retry sends the
	// same request and installs the result garam recorded.
	require.NoError(t, os.Remove(filepath.Join(h.supervisor, "enrollment.json")))
	require.NoError(t, os.Remove(filepath.Join(h.supervisor, "supervisor.pem")))
	out, err = h.enroll(t, s)
	require.NoError(t, err, "an interrupted enrollment is completed: %s", out)
	again, err := os.ReadFile(filepath.Join(h.supervisor, "supervisor.pem"))
	require.NoError(t, err)
	assert.Equal(t, first, again, "the recorded result is installed again")
	assert.Equal(t, os.FileMode(0o600), mode(t, filepath.Join(h.supervisor, "supervisor.pem")))
	var enrolled map[string]any
	record, err := os.ReadFile(filepath.Join(h.supervisor, "enrollment.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(record, &enrolled))
	renewals := `SELECT count(*) FROM agent_supervisor_renewals WHERE supervisor_grn = $1`
	supervisorGRN := enrolled["supervisorGrn"]

	s.clock.shift(17 * time.Hour)

	// Interrupted before the request reached garam: the pending
	// renewal is written first and sent again unchanged.
	out, err = standaloneCommand(t, "renew", "--dir", h.supervisor, "--machine-url", "https://127.0.0.1:1")
	require.Error(t, err, out)
	pending, err := os.ReadFile(filepath.Join(h.supervisor, "renewal.json"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), mode(t, filepath.Join(h.supervisor, "renewal.json")))

	// Interrupted after garam answered: the request is sent here and
	// the answer dropped, as a lost response or a crash would.
	var pendingRenewal struct {
		RequestID  string `json:"requestId"`
		RequestPEM string `json:"certificateRequestPem"`
	}
	require.NoError(t, json.Unmarshal(pending, &pendingRenewal))
	current := clientOverFile(t, filepath.Join(h.supervisor, "supervisor.pem"))
	lost := s.renewal(t, current, pendingRenewal.RequestID, pendingRenewal.RequestPEM)
	require.Equal(t, http.StatusCreated, lost.status, lost.body)

	out, err = standaloneCommand(t, "renew", "--dir", h.supervisor, "--machine-url", s.machine)
	require.NoError(t, err, out)
	assert.NoFileExists(t, filepath.Join(h.supervisor, "renewal.json"))
	installed, err := os.ReadFile(filepath.Join(h.supervisor, "supervisor.pem"))
	require.NoError(t, err)
	assert.NotEqual(t, first, installed)
	assert.Equal(t, 1, countRows(t, s.pool, renewals, supervisorGRN), "the lost answer was replayed, not reissued")

	// Interrupted after installing, before clearing: the pending
	// renewal names the key in use, so it is cleared and nothing is
	// sent.
	require.NoError(t, os.WriteFile(filepath.Join(h.supervisor, "renewal.json"), pending, 0o600))
	out, err = standaloneCommand(t, "renew", "--dir", h.supervisor, "--machine-url", s.machine)
	require.NoError(t, err, out)
	assert.NoFileExists(t, filepath.Join(h.supervisor, "renewal.json"))
	assert.Equal(t, 1, countRows(t, s.pool, renewals, supervisorGRN))
	unchanged, err := os.ReadFile(filepath.Join(h.supervisor, "supervisor.pem"))
	require.NoError(t, err)
	assert.Equal(t, installed, unchanged)

	// A completed enrollment is never replayed over what renewed since,
	// although its approval still stands and garam would answer it.
	out, err = h.enroll(t, s)
	require.NoError(t, err, out)
	afterReplay, err := os.ReadFile(filepath.Join(h.supervisor, "supervisor.pem"))
	require.NoError(t, err)
	assert.Equal(t, installed, afterReplay, "the renewed credential is kept")

	// The renewed certificate is the supervisor's credential from here.
	s.agent = mustParse(t, enrolled["grn"].(string))
	s.epoch = 1
	agentClient := clientOverPair(t, filepath.Join(h.agent, "agent.crt"), filepath.Join(h.agent, "agent.key"))
	leaf, err := os.ReadFile(filepath.Join(h.agent, "agent.crt"))
	require.NoError(t, err)
	got := s.activate(t, clientOverFile(t, filepath.Join(h.supervisor, "supervisor.pem")), "host-1", generationOf(1), nil, string(leaf))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var activation machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &activation))
	s.open(t, agentClient)
	allAdmitted(t, s.protectedCalls(t, agentClient, activation.Token), "the agent's installed credential")
}

// clientOverFile presents the certificate and key one file holds.
func clientOverFile(t *testing.T, path string) *http.Client {
	t.Helper()
	return clientOverPair(t, path, path)
}

// clientOverPair presents the certificate and key two files hold.
func clientOverPair(t *testing.T, certFile, keyFile string) *http.Client {
	t.Helper()
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	return machineClient(t, pair)
}

func mustParse(t *testing.T, s string) grn.GRN {
	t.Helper()
	g, err := grn.Parse(s)
	require.NoError(t, err)
	return g
}

// approveHost approves what prepare printed, as acme's owner.
func approveHost(t *testing.T, s *executionStack, request map[string]string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"requestId": request["requestId"], "hostName": "laptop", "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"secretSha256": request["secretSha256"], "supervisorKeySha256": request["supervisorKeySha256"],
		"agentKeySha256": request["agentKeySha256"],
	})
	require.NoError(t, err)
	status, approved := s.approve(t, s.owner, "acme", string(raw))
	require.Equal(t, http.StatusCreated, status, approved)
}

// snapshot reads every file in dir, by name.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	out := map[string]string{}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NoError(t, err)
		out[e.Name()] = string(raw)
	}
	return out
}

// TestStandaloneHost_PrepareWritesNoCredentialItDoesNotOwn covers the
// directories a preparation may write: never one inside the other, and
// never an agent directory holding credentials another preparation
// wrote, whose files are left as they were. Preparing the same pair
// again is the control.
func TestStandaloneHost_PrepareWritesNoCredentialItDoesNotOwn(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newHostPaths(t, s)
	request := h.prepare(t)
	before := snapshot(t, h.agent)

	other := filepath.Join(filepath.Dir(h.supervisor), "second-supervisor")
	out, err := standaloneCommand(t, "prepare", "--dir", other, "--agent-dir", h.agent)
	require.Error(t, err, "a second preparation of an owned agent directory: %s", out)
	assert.Equal(t, before, snapshot(t, h.agent), "the agent's files are untouched")
	assert.NoFileExists(t, filepath.Join(other, "prepared.json"))

	for _, dirs := range [][2]string{
		{h.supervisor, h.supervisor},
		{h.supervisor, filepath.Join(h.supervisor, "agent")},
		{filepath.Join(h.agent, "supervisor"), h.agent},
	} {
		out, err := standaloneCommand(t, "prepare", "--dir", dirs[0], "--agent-dir", dirs[1])
		require.Error(t, err, "overlapping %v: %s", dirs, out)
	}
	assert.Equal(t, before, snapshot(t, h.agent))
	assert.Equal(t, request, h.prepare(t), "the control: the owning pair prepares again")
}

// TestStandaloneHost_CommandsOnOneDirectoryNeverOverlap holds the lock
// another standalone command would hold on the supervisor's directory,
// and each command refuses rather than writing beside it; released,
// the same command runs.
func TestStandaloneHost_CommandsOnOneDirectoryNeverOverlap(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newHostPaths(t, s)
	require.NoError(t, os.MkdirAll(h.supervisor, 0o700))
	held, err := os.OpenFile(filepath.Join(h.supervisor, ".standalone.lock"), os.O_RDWR|os.O_CREATE, 0o600)
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(held.Fd()), syscall.LOCK_EX))

	for _, args := range [][]string{
		{"prepare", "--dir", h.supervisor, "--agent-dir", h.agent},
		{"enroll", "--dir", h.supervisor, "--agent-dir", h.agent, "--machine-url", s.machine, "--server-root-file", h.root},
		{"renew", "--dir", h.supervisor, "--machine-url", s.machine},
	} {
		out, err := standaloneCommand(t, args...)
		require.Error(t, err, out)
		assert.Contains(t, err.Error(), "another standalone command holds", args[0])
	}
	assert.NoFileExists(t, filepath.Join(h.supervisor, "prepared.json"))
	require.NoError(t, held.Close())
	h.prepare(t)
}

// receiver is an endpoint that records every request it is sent.
type receiver struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	w.WriteHeader(http.StatusNotFound)
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// TestStandaloneHost_CredentialsTravelOnlyToTheHTTPSEndpoint covers
// where the bootstrap secret and the supervisor's certificate may go:
// an http:// endpoint is refused before anything is sent or written,
// and an https:// endpoint answering with a redirect does not forward
// them to where it points, though both share the pinned root. The
// pinned-root enrollment in the test above is the control.
func TestStandaloneHost_CredentialsTravelOnlyToTheHTTPSEndpoint(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newHostPaths(t, s)
	request := h.prepare(t)
	approveHost(t, s, request)

	plain := &receiver{}
	plainServer := httptest.NewServer(plain)
	t.Cleanup(plainServer.Close)
	out, err := standaloneCommand(t, "enroll", "--dir", h.supervisor, "--agent-dir", h.agent,
		"--machine-url", plainServer.URL, "--server-root-file", h.root)
	require.Error(t, err, out)
	assert.Zero(t, plain.count(), "nothing reaches a plaintext endpoint")

	elsewhere := &receiver{}
	target := httptest.NewTLSServer(elsewhere)
	t.Cleanup(target.Close)
	redirecting := &receiver{}
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirecting.ServeHTTP(httptest.NewRecorder(), r)
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirector.Close)
	root := filepath.Join(t.TempDir(), "redirector.pem")
	require.NoError(t, os.WriteFile(root, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: redirector.Certificate().Raw}), 0o644))
	out, err = standaloneCommand(t, "enroll", "--dir", h.supervisor, "--agent-dir", h.agent,
		"--machine-url", redirector.URL, "--server-root-file", root)
	require.Error(t, err, out)
	require.Equal(t, 1, redirecting.count(), "the pinned endpoint was asked")
	assert.Zero(t, elsewhere.count(), "no credential follows a redirect")
	assert.NoFileExists(t, filepath.Join(h.supervisor, "enrollment.json"))

	out, err = h.enroll(t, s)
	require.NoError(t, err, "the control: the pinned listener enrolls: %s", out)
	out, err = standaloneCommand(t, "renew", "--dir", h.supervisor, "--machine-url", plainServer.URL)
	require.Error(t, err, out)
	assert.Zero(t, plain.count())
	assert.NoFileExists(t, filepath.Join(h.supervisor, "renewal.json"), "refused before a renewal is prepared")
}
