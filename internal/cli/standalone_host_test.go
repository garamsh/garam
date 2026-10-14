package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
)

// keyedPrepared is the record an earlier release committed for a
// standalone-enrollment.v1 approval, before it wrote the agent's files.
func keyedPrepared(t *testing.T, dirs hostDirs) *prepared {
	t.Helper()
	agentKey, agentRequest, err := newKeyAndRequest()
	require.NoError(t, err)
	fingerprint, err := agent.RequestedKeyFingerprint(agentRequest)
	require.NoError(t, err)
	supervisorKey, supervisorRequest, err := newKeyAndRequest()
	require.NoError(t, err)
	p := &prepared{
		RequestID: "request-1", Secret: "secret", AgentDir: dirs.agent,
		SupervisorKeyPEM: string(supervisorKey), SupervisorRequestPEM: string(supervisorRequest),
		AgentKeyPEM: string(agentKey), AgentRequestPEM: string(agentRequest), AgentKeyFingerprintHex: fingerprint.String(),
	}
	require.NoError(t, p.write(dirs.supervisor))
	return p
}

// TestPrepareHost_CompletesAPreparationInterruptedBeforeTheAgentFiles
// stops a v1 preparation an earlier release wrote where a crash would:
// its record committed, no agent file written. Preparing again writes
// the agent files from that record and answers the request it already
// bound, rather than refusing an agent directory it owns or generating
// other keys.
func TestPrepareHost_CompletesAPreparationInterruptedBeforeTheAgentFiles(t *testing.T) {
	base := t.TempDir()
	dirs := hostDirs{supervisor: filepath.Join(base, "supervisor"), agent: filepath.Join(base, "agent")}
	resolved, err := resolvedDirs(dirs)
	require.NoError(t, err)
	interrupted := keyedPrepared(t, resolved)
	assert.NoFileExists(t, filepath.Join(resolved.agent, agentKeyFile))

	request, err := prepareHost(dirs)
	require.NoError(t, err)
	assert.Equal(t, interrupted.RequestID, request.RequestID)
	assert.Equal(t, interrupted.AgentKeyFingerprintHex, request.AgentKeySHA256)
	assert.FileExists(t, filepath.Join(resolved.agent, agentKeyFile))
	completed, err := readPrepared(resolved.supervisor)
	require.NoError(t, err)
	assert.Empty(t, completed.AgentKeyPEM, "the agent key leaves the supervisor's record once installed")
}

// TestPrepareHost_PreparesAKeylessApproval holds a new preparation to
// standalone-enrollment.v2 (ADR-0100): the request names the secret's
// hash and no key, and the agent's directory is left empty. A keyed
// preparation, which answers its fingerprints, is the control.
func TestPrepareHost_PreparesAKeylessApproval(t *testing.T) {
	base := t.TempDir()
	dirs := hostDirs{supervisor: filepath.Join(base, "supervisor"), agent: filepath.Join(base, "agent")}
	request, err := prepareHost(dirs)
	require.NoError(t, err)
	assert.NotEmpty(t, request.RequestID)
	assert.Len(t, request.SecretSHA256, 64)
	assert.Empty(t, request.SupervisorKeySHA256)
	assert.Empty(t, request.AgentKeySHA256)
	entries, err := os.ReadDir(dirs.agent)
	require.NoError(t, err)
	for _, e := range entries {
		assert.Equal(t, hostLockFile, e.Name(), "nothing but the lock is written for the agent")
	}

	keyed := hostDirs{supervisor: filepath.Join(base, "keyed-supervisor"), agent: filepath.Join(base, "keyed-agent")}
	resolved, err := resolvedDirs(keyed)
	require.NoError(t, err)
	keyedPrepared(t, resolved)
	request, err = prepareHost(keyed)
	require.NoError(t, err)
	assert.NotEmpty(t, request.AgentKeySHA256, "the control: a keyed preparation answers its fingerprints")
}
