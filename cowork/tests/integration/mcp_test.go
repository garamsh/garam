package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/collab"
)

// bearer sends every request with who's bearer, as a client holding
// that person's CLI session sends it.
type bearer struct{ who string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.who)
	return http.DefaultTransport.RoundTrip(r)
}

// agent is an MCP client acting as who, as an AI client does.
type agent struct {
	t       *testing.T
	session *sdkmcp.ClientSession
}

func (s *stack) agent(t *testing.T, who string) *agent {
	t.Helper()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-agent", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &sdkmcp.StreamableClientTransport{
		Endpoint:   s.url + "/mcp",
		HTTPClient: &http.Client{Transport: bearer{who}},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return &agent{t: t, session: session}
}

// call calls a tool and answers its result, an error included.
func (a *agent) call(name string, args any) *sdkmcp.CallToolResult {
	a.t.Helper()
	res, err := a.session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(a.t, err)
	return res
}

// ok calls a tool that must succeed and decodes what it answers.
func (a *agent) ok(name string, args any, into any) {
	a.t.Helper()
	res := a.call(name, args)
	require.False(a.t, res.IsError, "%s: %s", name, textOf(res))
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(a.t, err)
	require.NoError(a.t, json.Unmarshal(raw, into))
}

func textOf(res *sdkmcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(*sdkmcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

type mcpPage struct {
	Page struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"page"`
	Markdown string         `json:"markdown"`
	Blocks   []collab.Block `json:"blocks"`
}

// TestMCP_TakesABearerAndNoCookie refuses the tools to a request
// carrying only the session cookie, so a page on another site cannot
// call them as the browser's visitor.
func TestMCP_TakesABearerAndNoCookie(t *testing.T) {
	s := newStack(t)
	status, body := s.call(t, "ada", http.MethodPost, "/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	assert.Equal(t, http.StatusUnauthorized, status, body)

	_, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-agent", Version: "1"}, nil).Connect(context.Background(),
		&sdkmcp.StreamableClientTransport{Endpoint: s.url + "/mcp", HTTPClient: &http.Client{Transport: bearer{"mallory"}}}, nil)
	assert.Error(t, err, "a bearer the platform does not accept")
}

// TestMCP_WritesAndReadsPagesAsTheCaller drives the page tools as ada:
// her workspaces, a page created with its content, read back as
// Markdown, edited by block, and found by search.
func TestMCP_WritesAndReadsPagesAsTheCaller(t *testing.T) {
	s := newStack(t)
	ada := s.agent(t, "ada")

	var ws struct {
		Workspaces []struct{ ID, Name string } `json:"workspaces"`
	}
	ada.ok("list_workspaces", map[string]any{}, &ws)
	require.Len(t, ws.Workspaces, 1)
	assert.Equal(t, "acme", ws.Workspaces[0].ID)

	var created mcpPage
	ada.ok("create_page", map[string]any{
		"org": "acme", "title": "Launch plan",
		"blocks": []map[string]any{
			{"type": "heading", "attrs": map[string]any{"level": 1}, "text": "Launch"},
			{"type": "paragraph", "text": "We ship on Friday."},
			{"type": "taskList", "items": []map[string]any{{"text": "write notes", "checked": true}, {"text": "tell support"}}},
		},
	}, &created)
	assert.Equal(t, "Launch plan", created.Page.Title)
	assert.Equal(t, "# Launch\n\nWe ship on Friday.\n\n- [x] write notes\n- [ ] tell support\n", created.Markdown)

	var updated mcpPage
	ada.ok("update_page", map[string]any{
		"org": "acme", "page": created.Page.ID, "title": "Launch plan v2",
		"edits": []map[string]any{
			{"kind": "replace", "index": 1, "block": map[string]any{"type": "paragraph", "text": "We ship on Monday."}},
			{"kind": "insert", "index": 1, "block": map[string]any{"type": "blockquote", "text": "Moved after review."}},
			{"kind": "delete", "index": 3},
		},
	}, &updated)
	assert.Equal(t, "Launch plan v2", updated.Page.Title)
	assert.Equal(t, "# Launch\n\n> Moved after review.\n\nWe ship on Monday.\n", updated.Markdown)

	var read mcpPage
	ada.ok("read_page", map[string]any{"org": "acme", "page": created.Page.ID}, &read)
	assert.Equal(t, updated.Markdown, read.Markdown, "the edits were kept")

	var found struct {
		Pages []struct{ ID string } `json:"pages"`
	}
	ada.ok("search", map[string]any{"org": "acme", "query": "launch"}, &found)
	require.Len(t, found.Pages, 1)
	assert.Equal(t, created.Page.ID, found.Pages[0].ID)

	var listed struct {
		Pages []struct{ ID string } `json:"pages"`
	}
	ada.ok("list_pages", map[string]any{"org": "acme"}, &listed)
	assert.Len(t, listed.Pages, 1)
}

// TestMCP_AnEditReachesAnOpenEditorLive shows bob, editing a page in
// his own editor, receiving ada's MCP edit as it is made, and ada's
// next read holding what bob typed.
func TestMCP_AnEditReachesAnOpenEditorLive(t *testing.T) {
	s := newStack(t)
	id := s.createPage(t, "notes")
	bob := newEditor(t, s, "bob", id)
	bob.connect()
	bob.appendBlock(collab.Block{Type: "paragraph", Text: "bob was here"})

	ada := s.agent(t, "ada")
	eventually(t, "bob's block reaches the server", func() bool {
		var p mcpPage
		ada.ok("read_page", map[string]any{"org": "acme", "page": id}, &p)
		return p.Markdown == "bob was here\n"
	})
	ada.ok("update_page", map[string]any{"org": "acme", "page": id, "edits": []map[string]any{
		{"kind": "append", "block": map[string]any{"type": "paragraph", "text": "ada's agent replied"}},
	}}, &mcpPage{})
	eventually(t, "ada's edit reaches bob's open editor", func() bool {
		return bob.text() == "bob was here\nada's agent replied"
	})
}

// TestMCP_ARefusalIsAToolErrorAndNoData shows each tool asking the
// platform as the caller: what it refuses comes back as the tool's
// error, holding none of the page, and leaves the page as it was.
func TestMCP_ARefusalIsAToolErrorAndNoData(t *testing.T) {
	s := newStack(t)
	id := s.createPage(t, "secret")
	writer := newEditor(t, s, "ada", id)
	writer.connect()
	writer.appendBlock(collab.Block{Type: "paragraph", Text: "the launch date"})
	eventually(t, "the content is kept", func() bool {
		var p mcpPage
		s.agent(t, "ada").ok("read_page", map[string]any{"org": "acme", "page": id}, &p)
		return p.Markdown != ""
	})

	s.platform.denyCheck("bob cowork:page-read@" + id)
	s.platform.denyCheck("carol cowork:page-write@" + id)

	bob := s.agent(t, "bob")
	res := bob.call("read_page", map[string]any{"org": "acme", "page": id})
	assert.True(t, res.IsError)
	assert.NotContains(t, textOf(res), "launch date")
	assert.Nil(t, res.StructuredContent)

	carol := s.agent(t, "carol")
	res = carol.call("update_page", map[string]any{"org": "acme", "page": id, "edits": []map[string]any{
		{"kind": "append", "block": map[string]any{"type": "paragraph", "text": "carol's"}},
	}})
	assert.True(t, res.IsError, "carol may read the page and not write it")
	var p mcpPage
	carol.ok("read_page", map[string]any{"org": "acme", "page": id}, &p)
	assert.Equal(t, "the launch date\n", p.Markdown, "the refused edit changed nothing")

	eve := s.agent(t, "eve")
	var ws struct {
		Workspaces []struct{ ID string } `json:"workspaces"`
	}
	eve.ok("list_workspaces", map[string]any{}, &ws)
	assert.Empty(t, ws.Workspaces)
	res = eve.call("read_page", map[string]any{"org": "acme", "page": id})
	assert.True(t, res.IsError, "eve is in no workspace")
}

// TestMCP_ReadsAndPostsMessages has ada's agent find a channel, post
// to it, and bob reading the post, live and in the channel's history.
func TestMCP_ReadsAndPostsMessages(t *testing.T) {
	s := newStack(t)
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	plans := s.createChannel(t, "bob", `{"kind":"private","name":"plans"}`)
	live := s.listen(t, "bob", general.ID)
	ada := s.agent(t, "ada")

	var cs struct {
		Channels []struct{ ID, Name string } `json:"channels"`
	}
	ada.ok("list_channels", map[string]any{"org": "acme"}, &cs)
	require.Len(t, cs.Channels, 1, "plans is bob's alone")
	assert.Equal(t, "general", cs.Channels[0].Name)

	var posted struct{ ID, Author, Body string }
	ada.ok("post_message", map[string]any{"org": "acme", "channel": general.ID, "body": "standup moved to 10"}, &posted)
	assert.Equal(t, userGRN("ada"), posted.Author)
	assert.Equal(t, "standup moved to 10", (<-live).Body, "bob's open channel receives it")

	var history struct {
		Messages []struct{ ID, Body string } `json:"messages"`
	}
	s.agent(t, "bob").ok("read_messages", map[string]any{"org": "acme", "channel": general.ID}, &history)
	require.Len(t, history.Messages, 1)
	assert.Equal(t, posted.ID, history.Messages[0].ID)

	var found struct {
		Messages []struct{ ID string } `json:"messages"`
	}
	ada.ok("search", map[string]any{"org": "acme", "query": "standup"}, &found)
	require.Len(t, found.Messages, 1)

	res := ada.call("post_message", map[string]any{"org": "acme", "channel": plans.ID, "body": "let me in"})
	assert.True(t, res.IsError, "ada is not among plans' participants")
}
