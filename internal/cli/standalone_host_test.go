package cli_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
)

// approvalRequest is what `standalone prepare` prints for an
// administrator to approve.
type approvalRequest struct {
	RequestID           string `json:"requestId"`
	SecretSHA256        string `json:"secretSha256"`
	SupervisorKeySHA256 string `json:"supervisorKeySha256"`
	AgentKeySHA256      string `json:"agentKeySha256"`
}

// prepare runs `standalone prepare` over the two directories and
// answers the request it printed.
func prepare(t *testing.T, supervisorDir, agentDir string) approvalRequest {
	t.Helper()
	out, err := runRoot(t, "standalone", "prepare", "--dir", supervisorDir, "--agent-dir", agentDir)
	require.NoError(t, err, out)
	var request approvalRequest
	require.NoError(t, json.Unmarshal([]byte(out), &request))
	return request
}

// keyAndRequest is a P-256 key and a certificate request over it, PEM
// encoded as a v1 host generated them.
func keyAndRequest(t *testing.T) (keyPEM, requestPEM string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	request, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: request}))
}

// keyedPrepared writes the record an earlier release committed for a
// standalone-enrollment.v1 approval, before it wrote the agent's files,
// and answers the request identifier and agent key fingerprint it
// bound.
func keyedPrepared(t *testing.T, supervisorDir, agentDir string) (requestID, agentFingerprint string) {
	t.Helper()
	agentKey, agentRequest := keyAndRequest(t)
	fingerprint, err := agent.RequestedKeyFingerprint([]byte(agentRequest))
	require.NoError(t, err)
	supervisorKey, supervisorRequest := keyAndRequest(t)
	record, err := json.Marshal(map[string]string{
		"requestId": "request-1", "secret": "secret", "agentDir": agentDir,
		"supervisorKeyPem": supervisorKey, "supervisorCertificateRequestPem": supervisorRequest,
		"agentKeyPem": agentKey, "agentCertificateRequestPem": agentRequest, "agentKeySha256": fingerprint.String(),
	})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(supervisorDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(supervisorDir, "prepared.json"), record, 0o600))
	return "request-1", fingerprint.String()
}

// TestPrepareHost_CompletesAPreparationInterruptedBeforeTheAgentFiles
// stops a v1 preparation an earlier release wrote where a crash would:
// its record committed, no agent file written. Preparing again writes
// the agent files from that record and answers the request it already
// bound, rather than refusing an agent directory it owns or generating
// other keys.
func TestPrepareHost_CompletesAPreparationInterruptedBeforeTheAgentFiles(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	supervisorDir, agentDir := filepath.Join(base, "supervisor"), filepath.Join(base, "agent")
	requestID, fingerprint := keyedPrepared(t, supervisorDir, agentDir)
	assert.NoFileExists(t, filepath.Join(agentDir, "agent.key"))

	request := prepare(t, supervisorDir, agentDir)
	assert.Equal(t, requestID, request.RequestID)
	assert.Equal(t, fingerprint, request.AgentKeySHA256)
	assert.FileExists(t, filepath.Join(agentDir, "agent.key"))
	raw, err := os.ReadFile(filepath.Join(supervisorDir, "prepared.json"))
	require.NoError(t, err)
	var completed map[string]string
	require.NoError(t, json.Unmarshal(raw, &completed))
	assert.Empty(t, completed["agentKeyPem"], "the agent key leaves the supervisor's record once installed")
}

// TestPrepareHost_PreparesAKeylessApproval holds a new preparation to
// standalone-enrollment.v2 (ADR-0100): the request names the secret's
// hash and no key, and the agent's directory is left empty. A keyed
// preparation, which answers its fingerprints, is the control.
func TestPrepareHost_PreparesAKeylessApproval(t *testing.T) {
	base := t.TempDir()
	agentDir := filepath.Join(base, "agent")
	request := prepare(t, filepath.Join(base, "supervisor"), agentDir)
	assert.NotEmpty(t, request.RequestID)
	assert.Len(t, request.SecretSHA256, 64)
	assert.Empty(t, request.SupervisorKeySHA256)
	assert.Empty(t, request.AgentKeySHA256)
	entries, err := os.ReadDir(agentDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.Equal(t, ".standalone.lock", e.Name(), "nothing but the lock is written for the agent")
	}

	resolved, err := filepath.EvalSymlinks(base)
	require.NoError(t, err)
	keyedSupervisor, keyedAgent := filepath.Join(resolved, "keyed-supervisor"), filepath.Join(resolved, "keyed-agent")
	keyedPrepared(t, keyedSupervisor, keyedAgent)
	request = prepare(t, keyedSupervisor, keyedAgent)
	assert.NotEmpty(t, request.AgentKeySHA256, "the control: a keyed preparation answers its fingerprints")
}
