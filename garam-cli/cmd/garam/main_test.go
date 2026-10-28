package main_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGaram_TheBuiltClientStartsAndExitsByKind builds the client binary
// and runs it twice: `--help` lists the user commands, and `whoami`
// with no credentials file ends with the exit code for "not signed in"
// (ADR-0122), which only main's wiring of ExitCode gives the process.
func TestGaram_TheBuiltClientStartsAndExitsByKind(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "garam")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, "go build: %s", out)

	help, err := exec.Command(bin, "--help").Output()
	require.NoError(t, err)
	for _, name := range []string{"login", "logout", "whoami", "orgs", "agents", "operators", "cowork", "mcp"} {
		assert.Contains(t, string(help), "  "+name+" ", "--help lists %s", name)
	}
	assert.NotContains(t, string(help), "  serve ", "the server commands are not the client's")

	whoami := exec.Command(bin, "whoami")
	whoami.Env = []string{"XDG_CONFIG_HOME=" + t.TempDir(), "GARAM_ENDPOINT=http://127.0.0.1:1"}
	stderr, err := whoami.CombinedOutput()
	var exit *exec.ExitError
	require.True(t, errors.As(err, &exit), "whoami signed out fails: %v", err)
	assert.Equal(t, 3, exit.ExitCode(), "not signed in exits 3: %s", stderr)
}
