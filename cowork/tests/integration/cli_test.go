package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The codes garam's client commands exit with (ADR-0122).
const (
	exitUsage            = 2
	exitUnauthenticated  = 3
	exitPermissionDenied = 4
	exitNotFound         = 5
	exitInvalid          = 7
)

// The client binary cowork's client commands are in (ADR-0133), built
// once for the run.
var (
	garamOnce sync.Once
	garamBin  string
	garamErr  error
)

// garamCLI builds garam-cli's garam from the module beside this one,
// once per run, and answers its path.
func garamCLI(t *testing.T) string {
	t.Helper()
	garamOnce.Do(func() {
		dir, err := os.MkdirTemp("", "garam-cli-")
		if err != nil {
			garamErr = err
			return
		}
		garamBin = filepath.Join(dir, "garam")
		build := exec.Command("go", "build", "-o", garamBin, "./cmd/garam")
		build.Dir = filepath.Join("..", "..", "..", "garam-cli")
		if out, err := build.CombinedOutput(); err != nil {
			garamErr = fmt.Errorf("go build: %w: %s", err, out)
		}
	})
	require.NoError(t, garamErr)
	return garamBin
}

// signIn keeps who's CLI session for the stack's platform in a fresh
// credentials file, as `garam login` leaves one, with acme saved as the
// workspace, and points the CLI at the stack. The file is written in
// the format the CLI has kept since ADR-0120, which is the contract
// between the two.
func (s *stack) signIn(t *testing.T, who string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("COWORK_ENDPOINT", s.url)
	t.Setenv("GARAM_ENDPOINT", s.platformURL)
	if who == "" {
		return
	}
	later := time.Now().Add(time.Hour)
	file, err := json.Marshal(map[string]any{"endpoints": map[string]any{s.platformURL: map[string]any{
		"accessToken": who, "accessExpiresAt": later, "refreshToken": "unused", "refreshExpiresAt": later, "org": "acme",
	}}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "garam"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "garam", "credentials"), file, 0o600))
}

// garam runs the built client with args and stdin, and answers what it
// printed and the code it exits with.
func garam(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(garamCLI(t), args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, failure bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &failure
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out.String(), 0
	case errors.As(err, &exit):
		return out.String() + failure.String(), exit.ExitCode()
	}
	require.NoError(t, err)
	return "", -1
}

// TestCLI_WorksInAWorkspaceAsItsSession has garam, as ada, create a
// page with its content, read it as Markdown, find it, and post about
// it, each as cowork answers the caller.
func TestCLI_WorksInAWorkspaceAsItsSession(t *testing.T) {
	s := newStack(t)
	s.signIn(t, "ada")
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)

	out, code := garam(t, "", "whoami", "-o", "json")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, `"grn": "grn:root:default:user:ada"`)

	out, code = garam(t, "", "orgs", "list")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "acme")

	out, code = garam(t, "", "cowork", "pages", "create", "--title", "Retro", "--paragraph", "What went well", "-o", "json")
	require.Equal(t, 0, code, out)
	var created struct {
		Page struct{ ID string } `json:"page"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &created))

	out, code = garam(t, "", "cowork", "pages", "read", created.Page.ID)
	require.Equal(t, 0, code, out)
	assert.Equal(t, "What went well\n", out)

	out, code = garam(t, "", "cowork", "pages", "list")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Retro")

	out, code = garam(t, "", "cowork", "channels", "messages", "post", general.ID, "--body", "retro notes are up")
	require.Equal(t, 0, code, out)
	out, code = garam(t, "", "cowork", "channels", "messages", "list", general.ID)
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "retro notes are up")

	out, code = garam(t, "", "cowork", "channels", "list")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "general")

	out, code = garam(t, "", "cowork", "search", "retro")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "page")
	assert.Contains(t, out, "message")
}

// TestCLI_ExitsWithTheCodeOfWhatFailed holds cowork's commands to
// ADR-0122's codes: 3 not signed in, 4 refused, 5 not found, 2 used
// wrongly, through a derived command and one calling a tool.
func TestCLI_ExitsWithTheCodeOfWhatFailed(t *testing.T) {
	s := newStack(t)
	id := s.createPage(t, "secret")

	s.signIn(t, "")
	_, code := garam(t, "", "cowork", "pages", "list", "--org", "acme")
	assert.Equal(t, exitUnauthenticated, code)

	s.signIn(t, "bob")
	s.platform.denyCheck("bob cowork:page-read@" + id)
	for _, read := range []string{"get", "read"} {
		_, code = garam(t, "", "cowork", "pages", read, id)
		assert.Equal(t, exitPermissionDenied, code, read)
		_, code = garam(t, "", "cowork", "pages", read, "01JAAAAAAAAAAAAAAAAAAAAAAA")
		assert.Equal(t, exitNotFound, code, read)
		_, code = garam(t, "", "cowork", "pages", read)
		assert.Equal(t, exitUsage, code, read)
	}
}

// TestCLI_BridgesStdioToTheTools has a local MCP client, speaking the
// stdio transport, list the tools and call one through
// `garam mcp cowork`.
func TestCLI_BridgesStdioToTheTools(t *testing.T) {
	s := newStack(t)
	s.signIn(t, "ada")
	stdin := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"local","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_workspaces","arguments":{}}}`,
	}, "\n") + "\n"
	out, code := garam(t, stdin, "mcp", "cowork")
	require.Equal(t, 0, code, out)

	replies := map[float64]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m), line)
		id, ok := m["id"].(float64)
		require.True(t, ok, "only requests are answered: %s", line)
		replies[id] = m
	}
	require.Len(t, replies, 3, out)
	assert.Contains(t, replies[1]["result"], "serverInfo")
	tools, _ := json.Marshal(replies[2]["result"])
	for _, name := range []string{"search", "read_page", "create_page", "update_page", "list_channels", "read_messages", "post_message"} {
		assert.Contains(t, string(tools), `"name":"`+name+`"`)
	}
	call, _ := json.Marshal(replies[3]["result"])
	assert.Contains(t, string(call), "acme")
}

// TestCLI_WorksTasks creates a task for bob, has bob move it and
// comment on it, and lists it by status and assignee.
func TestCLI_WorksTasks(t *testing.T) {
	s := newStack(t)
	s.signIn(t, "ada")
	out, code := garam(t, "", "cowork", "tasks", "create", "--title", "Review the plan", "--assignee", userGRN("bob"), "--due", "2026-11-02", "-o", "json")
	require.Equal(t, 0, code, out)
	var created struct{ ID string }
	require.NoError(t, json.Unmarshal([]byte(out), &created))

	s.signIn(t, "bob")
	out, code = garam(t, "", "cowork", "tasks", "update", created.ID, "--status", "doing")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "doing")
	out, code = garam(t, "", "cowork", "tasks", "comments", "post", created.ID, "--body", "reading it")
	require.Equal(t, 0, code, out)
	out, code = garam(t, "", "cowork", "tasks", "list", "--status", "doing", "--assignee", userGRN("bob"))
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Review the plan")
	out, code = garam(t, "", "cowork", "tasks", "comments", "list", created.ID)
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "reading it")

	_, code = garam(t, "", "cowork", "tasks", "update", created.ID, "--status", "blocked")
	assert.Equal(t, exitInvalid, code)
}
