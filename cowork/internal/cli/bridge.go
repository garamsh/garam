package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/cowork/internal/mcp"
	"github.com/garamsh/garam/lib/errorx"
)

// maxBridged bounds one message the bridge relays, either way.
const maxBridged = 16 << 20

func newMCPCommand(flags *clientFlags) *cobra.Command {
	return clientCommand(&cobra.Command{
		Use:   "mcp",
		Short: "Serve cowork's tools to a local MCP client over stdio",
		Long: "Relay MCP's stdio transport to cowork's /mcp endpoint, as the signed-in session. " +
			"Give it to a local client, such as Claude Desktop or Claude Code, as the command to run.",
		Args: cobra.NoArgs,
	}, flags, func(cmd *cobra.Command, c *client, _ []string) error {
		if _, err := c.session.Bearer(cmd.Context()); err != nil {
			return err
		}
		return c.bridge(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
	})
}

// bridge relays each line of in, one JSON-RPC message, to the MCP
// endpoint, and writes what the endpoint answers to out, a line per
// message. Requests are relayed concurrently, as a client may send
// several before the first is answered; out is written one whole
// message at a time.
func (c *client) bridge(ctx context.Context, in io.Reader, out io.Writer) error {
	var mu sync.Mutex
	write := func(msg []byte) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = out.Write(append(bytes.TrimSpace(msg), '\n'))
	}
	httpClient := &http.Client{Transport: bearer{c.session, http.DefaultTransport}}
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
			if err := c.relay(ctx, httpClient, msg, write); err != nil {
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
func (c *client) relay(ctx context.Context, h *http.Client, msg []byte, write func([]byte)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+mcp.Path, bytes.NewReader(msg))
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
