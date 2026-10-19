package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrepareHost_PreparesAKeylessApproval holds a preparation to
// standalone-enrollment.v2 (ADR-0100, ADR-0118): what it prints for the
// administrator is the request identifier and the secret's hash and
// nothing else, so no key fingerprint is offered for an approval to
// bind, and the agent's directory is left empty. Run again, it prints
// the same request.
func TestPrepareHost_PreparesAKeylessApproval(t *testing.T) {
	base := t.TempDir()
	supervisorDir, agentDir := filepath.Join(base, "supervisor"), filepath.Join(base, "agent")
	out, err := runRoot(t, "standalone", "prepare", "--dir", supervisorDir, "--agent-dir", agentDir)
	require.NoError(t, err, out)
	var request map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &request))
	assert.Len(t, request, 2, "the request identifier and the secret's hash alone: %s", out)
	assert.NotEmpty(t, request["requestId"])
	assert.Len(t, request["secretSha256"], 64)
	entries, err := os.ReadDir(agentDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.Equal(t, ".standalone.lock", e.Name(), "nothing but the lock is written for the agent")
	}

	again, err := runRoot(t, "standalone", "prepare", "--dir", supervisorDir, "--agent-dir", agentDir)
	require.NoError(t, err, again)
	assert.JSONEq(t, out, again, "the same request, never a second approval")
}
