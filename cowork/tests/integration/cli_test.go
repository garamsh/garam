package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/cli"
	"github.com/garamsh/garam/platform/sdk/clisession"
)

// signIn keeps who's CLI session for the stack's platform in a fresh
// credentials file, as `garam login` or `cowork login` leaves one, with
// acme saved as the workspace.
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
	require.NoError(t, clisession.Store{Path: filepath.Join(dir, "garam", "credentials")}.Update(func(f *clisession.File) (bool, error) {
		f.Endpoints[s.platformURL] = &clisession.Entry{
			AccessToken: who, AccessExpiresAt: later, RefreshToken: "unused", RefreshExpiresAt: later, Org: "acme",
		}
		return true, nil
	}))
}

// cowork runs the binary's command tree with args and stdin, and
// answers what it printed and the code it exits with.
func cowork(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	root := cli.NewRootCommand()
	root.SetArgs(args)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetIn(strings.NewReader(stdin))
	cmd, err := root.ExecuteC()
	return out.String(), cli.ExitCode(cmd, err)
}

// TestCLI_WorksInAWorkspaceAsItsSession has the CLI, as ada, create a
// page, read it as Markdown, find it, and post about it, each as the
// tool of its name would.
func TestCLI_WorksInAWorkspaceAsItsSession(t *testing.T) {
	s := newStack(t)
	s.signIn(t, "ada")
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)

	out, code := cowork(t, "", "whoami", "-o", "json")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, `"grn": "grn:root:default:user:ada"`)

	out, code = cowork(t, "", "workspaces")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "acme")

	out, code = cowork(t, "", "pages", "create", "--title", "Retro", "--paragraph", "What went well", "-o", "json")
	require.Equal(t, 0, code, out)
	var created struct {
		Page struct{ ID string } `json:"page"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &created))

	out, code = cowork(t, "", "pages", "read", created.Page.ID)
	require.Equal(t, 0, code, out)
	assert.Equal(t, "What went well\n", out)

	out, code = cowork(t, "", "pages", "list")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Retro")

	out, code = cowork(t, "", "messages", "post", general.ID, "retro notes are up")
	require.Equal(t, 0, code, out)
	out, code = cowork(t, "", "messages", "list", general.ID)
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "retro notes are up")

	out, code = cowork(t, "", "channels")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "general")

	out, code = cowork(t, "", "search", "retro")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "page")
	assert.Contains(t, out, "message")
}

// TestCLI_ExitsWithTheCodeOfWhatFailed holds the client commands to
// ADR-0122's codes: 3 not signed in, 4 refused, 5 not found, 2 used
// wrongly.
func TestCLI_ExitsWithTheCodeOfWhatFailed(t *testing.T) {
	s := newStack(t)
	id := s.createPage(t, "secret")

	s.signIn(t, "")
	_, code := cowork(t, "", "pages", "list", "--org", "acme")
	assert.Equal(t, cli.ExitUnauthenticated, code)

	s.signIn(t, "bob")
	s.platform.denyCheck("bob cowork:page-read@" + id)
	_, code = cowork(t, "", "pages", "read", id)
	assert.Equal(t, cli.ExitPermissionDenied, code)
	_, code = cowork(t, "", "pages", "read", "01JAAAAAAAAAAAAAAAAAAAAAAA")
	assert.Equal(t, cli.ExitNotFound, code)
	_, code = cowork(t, "", "pages", "read")
	assert.Equal(t, cli.ExitUsage, code)
}

// TestCLI_BridgesStdioToTheTools has a local MCP client, speaking the
// stdio transport, list the tools and call one through `cowork mcp`.
func TestCLI_BridgesStdioToTheTools(t *testing.T) {
	s := newStack(t)
	s.signIn(t, "ada")
	stdin := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"local","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_workspaces","arguments":{}}}`,
	}, "\n") + "\n"
	out, code := cowork(t, stdin, "mcp")
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

// TestCLI_WorksTasks creates a task for bob, has bob move it with a
// comment, and lists it by status.
func TestCLI_WorksTasks(t *testing.T) {
	s := newStack(t)
	s.signIn(t, "ada")
	out, code := cowork(t, "", "tasks", "create", "--title", "Review the plan", "--assignee", userGRN("bob"), "--due", "2026-11-02", "-o", "json")
	require.Equal(t, 0, code, out)
	var created struct{ ID string }
	require.NoError(t, json.Unmarshal([]byte(out), &created))

	s.signIn(t, "bob")
	out, code = cowork(t, "", "tasks", "update", created.ID, "--status", "doing", "--comment", "reading it")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "doing")
	out, code = cowork(t, "", "tasks", "list", "--status", "doing", "--assignee", "me")
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "Review the plan")
	out, code = cowork(t, "", "tasks", "show", created.ID)
	require.Equal(t, 0, code, out)
	assert.Contains(t, out, "reading it")

	_, code = cowork(t, "", "tasks", "update", created.ID, "--status", "blocked")
	assert.Equal(t, cli.ExitInvalid, code)
}
