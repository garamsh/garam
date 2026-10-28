package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/cli"
	"github.com/garamsh/garam/platform/internal/grn"
)

// runCLI runs one garam command in process and answers its stdout,
// its stderr and what it returned.
func runCLI(t *testing.T, stderr io.Writer, args ...string) (string, error) {
	t.Helper()
	root := cli.NewRootCommand()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return stdout.String(), err
}

// lockedBuffer is a buffer one goroutine writes while another reads.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var userCodeLine = regexp.MustCompile(`Your code is ([B-Z]{4}-[B-Z]{4})\.`)

// TestCLI_LogsInByTheDeviceGrantAndReadsTheOrgsObjects runs the user
// commands against the browser API as serve api composes it, behind
// the /api prefix the console rewrites: login waits until ada approves
// the code it printed, the session it saves reads her organization's
// objects, and logout revokes it.
func TestCLI_LogsInByTheDeviceGrantAndReadsTheOrgsObjects(t *testing.T) {
	s := newDeviceStack(t)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("GARAM_ENDPOINT", s.url)
	operatorGRN := registerOperator(t, s.pool, grn.New("acme", grn.DefaultProject, "operator", "hosted"))
	agentGRN := seedAgent(t, agentpg.New(s.pool), s.acme)

	_, err := runCLI(t, io.Discard, "whoami")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not signed in to "+s.url, "before login there is no session")

	prompt := &lockedBuffer{}
	done := make(chan error, 1)
	go func() {
		_, err := runCLI(t, prompt, "login", "--no-browser")
		done <- err
	}()
	var userCode string
	require.Eventually(t, func() bool {
		m := userCodeLine.FindStringSubmatch(prompt.String())
		if m != nil {
			userCode = m[1]
		}
		return m != nil
	}, 10*time.Second, 50*time.Millisecond, "login printed its code")
	assert.Contains(t, prompt.String(), s.url+"/device?code="+userCode, "the page is joined to the endpoint")
	status, body := s.decide(t, s.ada, userCode, true)
	require.Equal(t, 204, status, body)
	select {
	case err := <-done:
		require.NoError(t, err, prompt.String())
	case <-time.After(30 * time.Second):
		t.Fatal("login did not return once its code was approved")
	}
	assert.Contains(t, prompt.String(), "Signed in to "+s.url+" as ada")

	path := filepath.Join(config, "garam", "credentials")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dir, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm())

	out, err := runCLI(t, io.Discard, "whoami", "-o", "json")
	require.NoError(t, err)
	var who apigen.User
	require.NoError(t, json.Unmarshal([]byte(out), &who))
	assert.Equal(t, s.adaGRN.String(), who.Grn)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, out, tokenIn(t, raw), "no command prints the session's token")

	_, err = runCLI(t, io.Discard, "agents", "list")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no organization", "with no --org and nothing saved, no org is guessed")

	out, err = runCLI(t, io.Discard, "orgs", "list")
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^grn:root:default:org:acme\s+acme\s`, out)
	_, err = runCLI(t, io.Discard, "orgs", "use", "nonesuch")
	assert.Error(t, err, "an organization the session cannot read is not saved")
	_, err = runCLI(t, io.Discard, "orgs", "use", "acme")
	require.NoError(t, err)

	out, err = runCLI(t, io.Discard, "agents", "list")
	require.NoError(t, err)
	assert.Contains(t, out, agentGRN.Identifier, "the saved org is the one listed")
	out, err = runCLI(t, io.Discard, "agents", "get", agentGRN.Identifier, "-o", "json")
	require.NoError(t, err)
	var got apigen.Agent
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, agentGRN.String(), got.Grn)
	out, err = runCLI(t, io.Discard, "agents", "rename", agentGRN.Identifier, "Sherlock")
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^`+agentGRN.Identifier+`\s+Sherlock\s`, out)
	out, err = runCLI(t, io.Discard, "agents", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "Sherlock", "the list shows the name the rename set")
	out, err = runCLI(t, io.Discard, "agents", "rename", agentGRN.Identifier, "--clear")
	require.NoError(t, err)
	assert.NotContains(t, out, "Sherlock", "and --clear clears it")
	out, err = runCLI(t, io.Discard, "operators", "list")
	require.NoError(t, err)
	assert.Contains(t, out, operatorGRN.Identifier)

	// An expired access token is refreshed before the call: the next
	// command still answers, under a session holding a new pair.
	before := countRows(t, s.pool, "SELECT count(*) FROM sessions WHERE kind = 'cli'")
	_, err = s.pool.Exec(context.Background(), "UPDATE sessions SET expires_at = now() - interval '1 second' WHERE kind = 'cli'")
	require.NoError(t, err)
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	var file struct {
		Endpoints map[string]map[string]any `json:"endpoints"`
	}
	require.NoError(t, json.Unmarshal(raw, &file))
	file.Endpoints[s.url]["accessExpiresAt"] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
	rewritten, err := json.Marshal(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, rewritten, 0o600))
	_, err = runCLI(t, io.Discard, "whoami")
	require.NoError(t, err, "a lapsed access token is refreshed rather than refused")
	refreshed, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotEqual(t, tokenIn(t, raw), tokenIn(t, refreshed), "and the file holds the new pair")
	assert.Equal(t, before, countRows(t, s.pool, "SELECT count(*) FROM sessions WHERE kind = 'cli'"))

	_, err = runCLI(t, io.Discard, "logout")
	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, s.pool, "SELECT count(*) FROM sessions WHERE kind = 'cli'"),
		"logout revokes the CLI's session")
	_, err = runCLI(t, io.Discard, "whoami")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not signed in")
	out, err = runCLI(t, io.Discard, "agents", "list", "--org", "acme")
	require.Error(t, err, "nor does anything else run: %s", out)
	left, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(left), `"org": "acme"`, "the saved org outlives the session")
}

// tokenIn answers the access token a credentials file holds.
func tokenIn(t *testing.T, raw []byte) string {
	t.Helper()
	var file struct {
		Endpoints map[string]struct {
			AccessToken string `json:"accessToken"`
		} `json:"endpoints"`
	}
	require.NoError(t, json.Unmarshal(raw, &file))
	for _, entry := range file.Endpoints {
		if entry.AccessToken != "" {
			return entry.AccessToken
		}
	}
	t.Fatal("the credentials file holds no access token")
	return ""
}
