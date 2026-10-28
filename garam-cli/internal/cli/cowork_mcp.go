package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/garam-cli/internal/session"
	"github.com/garamsh/garam/lib/errorx"
)

// What cowork's MCP endpoint publishes besides its tools
// (cowork/docs/cowork.md §MCP): where it is served, and the key of a
// tool error's _meta naming its errorx kind.
const (
	coworkMCPPath    = "/mcp"
	coworkMCPKindKey = "garam.sh/kind"
)

// maxBridged bounds one message the bridge relays, either way.
const maxBridged = 16 << 20

// bearer sends every request with the session's bearer, refreshed
// where it lapses.
type bearer struct {
	session *session.Session
	next    http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	token, err := b.session.Bearer(r.Context())
	if err != nil {
		return nil, err
	}
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	return b.next.RoundTrip(r)
}

// callTool calls one of the tools at the cowork base names and decodes
// what it answers into out. A tool's error comes back with the kind
// the server named, so the command exits with its code.
func (c *userCommand) callTool(ctx context.Context, base, tool string, args map[string]any, out any) error {
	// The session is checked first, so a person not signed in is told
	// so rather than that the connection failed.
	if _, err := c.session.Bearer(ctx); err != nil {
		return err
	}
	cl := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "garam-cli", Version: "1"}, nil)
	conn, err := cl.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:   base + coworkMCPPath,
		HTTPClient: &http.Client{Timeout: userRequestTimeout, Transport: bearer{c.session, http.DefaultTransport}},
	}, nil)
	if err != nil {
		return errorx.Wrap(err, "connecting to "+base, errorx.KindUnavailable)
	}
	defer func() { _ = conn.Close() }()
	res, err := conn.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return errorx.Wrap(err, tool, errorx.KindUnavailable)
	}
	if res.IsError {
		kind, _ := res.Meta[coworkMCPKindKey].(string)
		return errorx.New(kindNamed(kind), textOf(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return errorx.Wrap(err, tool)
	}
	return json.Unmarshal(raw, out)
}

// kindNamed is the errorx kind name names, unknown for none.
func kindNamed(name string) errorx.Kind {
	for k := errorx.KindUnknown; k <= errorx.KindUnauthenticated; k++ {
		if k.String() == name {
			return k
		}
	}
	return errorx.KindUnknown
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

// newMCPCommand groups the stdio bridges to the services' MCP
// endpoints (ADR-0133): one per service, named for it.
func newMCPCommand() *cobra.Command {
	mcp := &cobra.Command{Use: "mcp", Short: "Serve a service's MCP tools to a local client over stdio"}
	flags := &userFlags{}
	flags.bind(mcp)
	server := coworkServer(coworkFlag(mcp))
	mcp.AddCommand(&cobra.Command{
		Use:   "cowork",
		Short: "Serve cowork's tools to a local MCP client over stdio",
		Long: "Relay MCP's stdio transport to cowork's /mcp endpoint, as the signed-in session. " +
			"Give it to a local client, such as Claude Desktop or Claude Code, as the command to run.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := newUserCommand(cmd, flags)
			if err != nil {
				return err
			}
			base, err := server(c)
			if err != nil {
				return err
			}
			if _, err := c.session.Bearer(cmd.Context()); err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()
			return c.bridge(ctx, base+coworkMCPPath, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	})
	return mcp
}

// bridge relays each line of in, one JSON-RPC message, to the MCP
// endpoint, and writes what the endpoint answers to out, a line per
// message. Requests are relayed concurrently, as a client may send
// several before the first is answered; out is written one whole
// message at a time.
func (c *userCommand) bridge(ctx context.Context, endpoint string, in io.Reader, out io.Writer) error {
	var mu sync.Mutex
	write := func(msg []byte) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = out.Write(append(bytes.TrimSpace(msg), '\n'))
	}
	h := &http.Client{Transport: bearer{c.session, http.DefaultTransport}}
	var wg sync.WaitGroup
	defer wg.Wait()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64<<10), maxBridged)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		msg := append([]byte(nil), line...)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := relay(ctx, h, endpoint, msg, write); err != nil {
				if reply := errorReply(msg, err); reply != nil {
					write(reply)
				}
			}
		}()
	}
	if err := scanner.Err(); err != nil {
		return errorx.Wrap(err, "reading stdin")
	}
	return nil
}

// relay posts one message and writes each the endpoint answers: none
// for a notification, one as JSON, or each event of a stream.
func relay(ctx context.Context, h *http.Client, endpoint string, msg []byte, write func([]byte)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(msg))
	if err != nil {
		return errorx.Wrap(err, "mcp")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := h.Do(req)
	if err != nil {
		return errorx.Wrap(err, "mcp", errorx.KindUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	body := io.LimitReader(resp.Body, maxBridged)
	switch {
	case resp.StatusCode == http.StatusAccepted:
		return nil
	case resp.StatusCode != http.StatusOK:
		raw, _ := io.ReadAll(body)
		return errorx.Wrap(statusError(resp.StatusCode, raw), "mcp")
	case strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream"):
		return events(body, write)
	default:
		raw, err := io.ReadAll(body)
		if err != nil {
			return errorx.Wrap(err, "mcp", errorx.KindUnavailable)
		}
		write(raw)
		return nil
	}
}

func statusError(status int, body []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	msg := http.StatusText(status)
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		msg = e.Message
	}
	return errorx.New(errorx.KindUnavailable, msg)
}

// events writes the data of each server-sent event.
func events(r io.Reader, write func([]byte)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64<<10), maxBridged)
	var data bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "":
			if data.Len() > 0 {
				write(data.Bytes())
				data.Reset()
			}
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if data.Len() > 0 {
		write(data.Bytes())
	}
	return scanner.Err()
}

// errorReply answers a request the bridge could not relay, so the
// client is told rather than left waiting. A notification is answered
// nothing.
func errorReply(msg []byte, err error) []byte {
	var req struct {
		ID json.RawMessage `json:"id"`
	}
	if json.Unmarshal(msg, &req) != nil || len(req.ID) == 0 || string(req.ID) == "null" {
		return nil
	}
	reply, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": req.ID,
		"error": map[string]any{"code": -32603, "message": err.Error()},
	})
	return reply
}
