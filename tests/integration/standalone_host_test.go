package integration_test

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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

	body := map[string]any{
		"requestId": request["requestId"], "hostName": "laptop", "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"secretSha256": request["secretSha256"], "supervisorKeySha256": request["supervisorKeySha256"],
		"agentKeySha256": request["agentKeySha256"],
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	status, approved := s.approve(t, s.owner, "acme", string(raw))
	require.Equal(t, http.StatusCreated, status, approved)

	out, err = h.enroll(t, s)
	require.NoError(t, err, out)
	first, err := os.ReadFile(filepath.Join(h.supervisor, "supervisor.pem"))
	require.NoError(t, err)
	out, err = h.enroll(t, s)
	require.NoError(t, err, "a repeated enrollment is answered again: %s", out)
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
