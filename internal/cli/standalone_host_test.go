package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrepareHost_CompletesAPreparationInterruptedBeforeTheAgentFiles
// stops a preparation where a crash would: its record committed, no
// agent file written. Preparing again writes the agent files from that
// record and answers the request it already bound, rather than
// refusing an agent directory it owns or generating other keys.
func TestPrepareHost_CompletesAPreparationInterruptedBeforeTheAgentFiles(t *testing.T) {
	base := t.TempDir()
	dirs := hostDirs{supervisor: filepath.Join(base, "supervisor"), agent: filepath.Join(base, "agent")}
	resolved, err := resolvedDirs(dirs)
	require.NoError(t, err)
	interrupted, err := newPrepared(resolved)
	require.NoError(t, err)
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
