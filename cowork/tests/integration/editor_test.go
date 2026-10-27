package integration_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/collab/goja"
)

// editor is one client editing a page: its own Yjs document, in its
// own engine as a browser holds its own, synced over a WebSocket as
// y-websocket's provider syncs it. Offline, it keeps editing its
// document and sends nothing; reconnecting syncs both ways.
type editor struct {
	t      *testing.T
	s      *stack
	who    string
	page   string
	engine *goja.Engine
	doc    *goja.Client

	mu     sync.Mutex
	conn   *websocket.Conn
	denied bool
}

func newEditor(t *testing.T, s *stack, who, page string) *editor {
	t.Helper()
	engine, err := goja.New()
	require.NoError(t, err)
	doc, err := engine.NewClient(nil)
	require.NoError(t, err)
	e := &editor{t: t, s: s, who: who, page: page, engine: engine, doc: doc}
	t.Cleanup(e.disconnect)
	return e
}

// dial opens the sync WebSocket as e.who and answers the upgrade's
// status where it was refused.
func (e *editor) dial() (*websocket.Conn, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(e.s.url, "http") + "/orgs/acme/pages/" + e.page + "/sync"
	c, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {"garam_session=" + e.who}},
	})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return nil, status, err
	}
	return c, http.StatusSwitchingProtocols, nil
}

// connect goes online: it sends its state vector, answers the
// server's, and applies what arrives until it disconnects.
func (e *editor) connect() {
	e.t.Helper()
	c, status, err := e.dial()
	require.NoError(e.t, err, "upgrade answered %d", status)
	c.SetReadLimit(8 << 20)
	e.mu.Lock()
	e.conn = c
	e.mu.Unlock()
	sv, err := e.doc.StateVector()
	require.NoError(e.t, err)
	e.send(collab.Message{Kind: collab.MessageKindSync, Sync: collab.SyncKindStep1, Payload: sv})
	go e.read(c)
}

func (e *editor) read(c *websocket.Conn) {
	for {
		_, raw, err := c.Read(context.Background())
		if err != nil {
			return
		}
		if len(raw) > 0 && collab.MessageKind(raw[0]) == collab.MessageKindAuth {
			e.mu.Lock()
			e.denied = true
			e.mu.Unlock()
			continue
		}
		m, err := collab.Decode(raw)
		if err != nil || m.Kind != collab.MessageKindSync {
			continue
		}
		switch m.Sync {
		case collab.SyncKindStep1:
			state, err := e.doc.State()
			if err != nil {
				return
			}
			missing, err := e.engine.Diff(state, m.Payload)
			if err != nil {
				return
			}
			e.send(collab.Message{Kind: collab.MessageKindSync, Sync: collab.SyncKindStep2, Payload: missing})
		default:
			_ = e.doc.Apply(m.Payload)
		}
	}
}

func (e *editor) send(m collab.Message) {
	e.mu.Lock()
	c := e.conn
	e.mu.Unlock()
	if c == nil {
		return
	}
	_ = c.Write(context.Background(), websocket.MessageBinary, m.Encode())
}

// disconnect goes offline.
func (e *editor) disconnect() {
	e.mu.Lock()
	c := e.conn
	e.conn = nil
	e.mu.Unlock()
	if c != nil {
		_ = c.Close(websocket.StatusNormalClosure, "")
	}
}

// edit sends an update an edit made, where online.
func (e *editor) edit(update []byte, err error) {
	e.t.Helper()
	require.NoError(e.t, err)
	e.send(collab.Message{Kind: collab.MessageKindSync, Sync: collab.SyncKindUpdate, Payload: update})
}

func (e *editor) appendBlock(b collab.Block) { e.t.Helper(); e.edit(e.doc.AppendBlock(b)) }

func (e *editor) insertText(block, offset int, text string) {
	e.t.Helper()
	e.edit(e.doc.InsertText(block, offset, text))
}

func (e *editor) blocks() []collab.Block {
	e.t.Helper()
	state, err := e.doc.State()
	require.NoError(e.t, err)
	b, err := e.engine.Blocks(state)
	require.NoError(e.t, err)
	return b
}

func (e *editor) text() string {
	var parts []string
	for _, b := range e.blocks() {
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n")
}

func (e *editor) wasDenied() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.denied
}

// eventually waits for cond, the condition the next step depends on,
// and fails naming what never held.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never held: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
