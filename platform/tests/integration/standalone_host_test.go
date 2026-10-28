package integration_test

import (
	"bytes"
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

	"github.com/garamsh/garam/platform/internal/cli"
	"github.com/garamsh/garam/platform/internal/operator"
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

// approveHost approves what prepare printed, as acme's owner.
func approveHost(t *testing.T, s *executionStack, request map[string]string) {
	t.Helper()
	approval := map[string]any{
		"requestId": request["requestId"], "hostName": "laptop", "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"secretSha256": request["secretSha256"],
	}
	raw, err := json.Marshal(approval)
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
// the agent's directory left as it was. Preparing the same pair again
// is the control.
func TestStandaloneHost_PrepareWritesNoCredentialItDoesNotOwn(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newHostPaths(t, s)
	request := h.prepare(t)
	before := snapshot(t, h.agent)

	for _, dirs := range [][2]string{
		{h.supervisor, h.supervisor},
		{h.supervisor, filepath.Join(h.supervisor, "agent")},
		{filepath.Join(h.agent, "supervisor"), h.agent},
	} {
		out, err := standaloneCommand(t, "prepare", "--dir", dirs[0], "--agent-dir", dirs[1])
		require.Error(t, err, "overlapping %v: %s", dirs, out)
	}
	assert.Equal(t, before, snapshot(t, h.agent), "the agent's directory is untouched")
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
// where the bootstrap secret may go:
// an http:// endpoint is refused before anything is sent or written,
// and an https:// endpoint answering with a redirect does not forward
// them to where it points, though both share the pinned root. The
// pinned-root enrollment at the end is the control.
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
}

// exchangeCredential presents a supervisor credential to the token
// exchange and answers its status.
func exchangeCredential(t *testing.T, s *executionStack, credential string) int {
	t.Helper()
	got, err := s.exchange(anonymousMachineClient(t), http.MethodPost, "/tokens", operator.TokenContractVersion,
		"Garam-Credential "+credential, "{}")
	require.NoError(t, err)
	return got.status
}

// TestStandaloneHost_EnrollsUnderV2ForACredential drives the shipped
// command through standalone-enrollment.v2 (ADR-0100): `prepare` binds
// no key and writes nothing in the agent's directory, `enroll` installs
// the supervisor's credential owner-only and keeps it out of the
// record, an enrollment interrupted before its record re-issues the
// credential and voids the one written before, a completed one
// installs nothing again. The credential installed last exchanging is
// the control on the voided one refusing.
func TestStandaloneHost_EnrollsUnderV2ForACredential(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newHostPaths(t, s)

	request := h.prepare(t)
	assert.Equal(t, request, h.prepare(t), "a repeated prepare answers the same request")
	assert.Len(t, request, 2, "a v2 approval binds no key: %v", request)
	assert.NotEmpty(t, request["requestId"])
	assert.NotEmpty(t, request["secretSha256"])
	written := snapshot(t, h.agent)
	delete(written, ".standalone.lock")
	assert.Empty(t, written, "nothing is generated for the agent")
	assert.Equal(t, os.FileMode(0o600), mode(t, filepath.Join(h.supervisor, "prepared.json")))

	out, err := h.enroll(t, s)
	require.Error(t, err, "spending before the approval: %s", out)
	assert.NoFileExists(t, filepath.Join(h.supervisor, "credential"))

	approveHost(t, s, request)
	out, err = h.enroll(t, s)
	require.NoError(t, err, out)
	credentialPath := filepath.Join(h.supervisor, "credential")
	first, err := os.ReadFile(credentialPath)
	require.NoError(t, err)
	assert.Regexp(t, `^garam_sc_[0-9a-f]{64}$`, string(first))
	assert.Equal(t, os.FileMode(0o600), mode(t, credentialPath))
	assert.FileExists(t, filepath.Join(h.agent, "server-root.pem"))
	record, err := os.ReadFile(filepath.Join(h.supervisor, "enrollment.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(record), string(first), "the record never holds the credential")
	assert.NotContains(t, string(record), "supervisorCertificatePem")
	assert.Contains(t, out, "enrolled ", "the command reports nothing secret: %s", out)
	assert.NotContains(t, out, string(first))

	// Interrupted after the credential was written and before the
	// record: the retry is re-issued, and the first credential voided.
	require.NoError(t, os.Remove(filepath.Join(h.supervisor, "enrollment.json")))
	out, err = h.enroll(t, s)
	require.NoError(t, err, out)
	second, err := os.ReadFile(credentialPath)
	require.NoError(t, err)
	assert.NotEqual(t, first, second, "a retried spending re-issues the credential")
	assert.Equal(t, http.StatusUnauthorized, exchangeCredential(t, s, string(first)), "the credential re-issued over is void")
	assert.Equal(t, http.StatusCreated, exchangeCredential(t, s, string(second)), "the control: the installed one exchanges")

	out, err = h.enroll(t, s)
	require.NoError(t, err, out)
	unchanged, err := os.ReadFile(credentialPath)
	require.NoError(t, err)
	assert.Equal(t, second, unchanged, "a completed enrollment installs nothing again")

	out, err = standaloneCommand(t, "renew", "--dir", h.supervisor, "--machine-url", s.machine)
	require.Error(t, err, "renew is no command of a host that holds no certificate: %s", out)
}
