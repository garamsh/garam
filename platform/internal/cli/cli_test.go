package cli_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/cli"
)

// runRoot executes the command tree with args and returns what cobra
// wrote and what Execute handed back. Both streams are captured
// together because what this file is about is the whole of what a
// terminal sees.
func runRoot(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runRootContext(context.Background(), t, args...)
}

// runRootContext is runRoot under ctx, which a serve command runs until
// it is done.
func runRootContext(ctx context.Context, t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	// Execute first: a return statement evaluates its operands left to
	// right, so reading the buffer beside the call reads it empty.
	err := root.ExecuteContext(ctx)
	return out.String(), err
}

// TestNewRootCommand_AFailureWhileRunningPrintsNoUsage covers the
// command that fails after its arguments were accepted. `migrate`
// refuses an empty GARAM_DATABASE_URL before it opens anything, so the
// failure is the cheapest one in the tree that reaches RunE.
//
// The error is asserted twice over, once for each way it could be
// wrong: Execute hands it back, so cmd/garam has something to print,
// and cobra wrote nothing, so what cmd/garam prints is the only copy.
// A test asserting one and not the other passes on a silence.
func TestNewRootCommand_AFailureWhileRunningPrintsNoUsage(t *testing.T) {
	t.Setenv("GARAM_DATABASE_URL", "")

	out, err := runRoot(t, "migrate")

	require.Error(t, err, "Execute hands the error back, so the caller has one to print")
	assert.NotContains(t, out, "Usage:", "a failure while running is not a question about invocation")
	assert.Empty(t, out, "cobra printed nothing, so the caller's line is the only copy")
}

// TestNewRootCommand_AUsageErrorStillPrintsUsage covers the other side.
// Each row fails before RunE, which is what the usage block answers,
// and each fails at a different check: cobra parses flags, validates
// arguments, and validates required flags in that order, with its
// PersistentPreRunE and PreRunE hooks in between. The required-flag row
// is why the silencing is inside RunE rather than in a hook — a hook
// runs before that check and would take this block away.
func TestNewRootCommand_AUsageErrorStillPrintsUsage(t *testing.T) {
	tests := map[string][]string{
		"a flag that does not exist":      {"migrate", "--bogus"},
		"an argument that is missing":     {"retire-message-signing-key"},
		"a required flag that is not set": {"copy-memory", "--from", "/data"},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			out, err := runRoot(t, args...)

			require.Error(t, err)
			assert.Contains(t, out, "Usage:", "a bad invocation is what the block answers")
			assert.NotContains(t, out, "Error:",
				"cobra still writes no error, so the caller's line stays the only copy")
		})
	}
}
