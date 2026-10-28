package standalone_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScrub_RemovesASecretValueOnAnyLine holds the redaction every
// captured output passes through to the secret's value, wherever it
// appears, rather than to lines that name a key. The control is the
// same text without a secret file read, which keeps the value.
func TestScrub_RemovesASecretValueOnAnyLine(t *testing.T) {
	env := filepath.Join(t.TempDir(), "model.env")
	require.NoError(t, os.WriteFile(env, []byte("MODEL_KEY=s3cr3t-value\n"), 0o600))
	captured := "a line naming nothing\n{\"msg\":\"tool output\",\"out\":\"s3cr3t-value\"}\nend"

	unread := &stack{t: t}
	assert.Contains(t, unread.scrub(captured), "s3cr3t-value", "the control: nothing read, nothing removed")

	s := &stack{t: t}
	s.secretsFrom(env)
	scrubbed := s.scrub(captured)
	assert.NotContains(t, scrubbed, "s3cr3t-value")
	assert.Contains(t, scrubbed, "[redacted]")
	assert.Contains(t, scrubbed, "a line naming nothing", "the rest of the output is kept")
}
