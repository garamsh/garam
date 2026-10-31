package integration_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/collab"
)

// The close codes cowork-web reads (ADR-0139).
const (
	closeSignedOut websocket.StatusCode = 4401
	closeNoAccess  websocket.StatusCode = 4403
)

// socket is one open WebSocket as who, read until it closes.
type socket struct {
	closed chan websocket.StatusCode
}

func (s *stack) open(t *testing.T, who, path string) *socket {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	url := "ws" + strings.TrimPrefix(s.url, "http") + path
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {"garam_session=" + who}}})
	require.NoError(t, err)
	c.SetReadLimit(8 << 20)
	out := &socket{closed: make(chan websocket.StatusCode, 1)}
	go func() {
		for {
			if _, _, err := c.Read(ctx); err != nil {
				out.closed <- websocket.CloseStatus(err)
				return
			}
		}
	}()
	return out
}

// closeCode waits for the socket's close and answers its code.
func (s *socket) closeCode(t *testing.T) websocket.StatusCode {
	t.Helper()
	select {
	case code := <-s.closed:
		return code
	case <-time.After(5 * time.Second):
		t.Fatal("the connection stayed open")
	}
	return 0
}

// openPage and openChannel are the two sockets a person holds.
func (s *stack) openPage(t *testing.T, who, page string) *socket {
	t.Helper()
	return s.open(t, who, "/orgs/acme/pages/"+page+"/sync")
}

func (s *stack) openChannel(t *testing.T, who, channel string) *socket {
	t.Helper()
	return s.open(t, who, "/orgs/acme/channels/"+channel+"/live")
}

// TestSocket_ASignedOutSessionClosesBothSockets signs bob out with both
// sockets open: each closes with the code a client reads as signed out.
// Ada's two connections are the control: they outlive bob's, and carry
// what is sent after it.
func TestSocket_ASignedOutSessionClosesBothSockets(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "notes")
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	adaEditor := newEditor(t, s, "ada", pageID)
	adaEditor.connect()
	adaLive := s.listen(t, "ada", general.ID)
	bobPage, bobChannel := s.openPage(t, "bob", pageID), s.openChannel(t, "bob", general.ID)

	s.platform.signOut("bob")

	assert.Equal(t, closeSignedOut, bobPage.closeCode(t))
	assert.Equal(t, closeSignedOut, bobChannel.closeCode(t))
	carol := newEditor(t, s, "carol", pageID)
	carol.connect()
	carol.appendBlock(collab.Block{Type: "paragraph", Text: "after"})
	eventually(t, "ada's sync outlives bob's", func() bool { return adaEditor.text() == "after" })
	posted := s.post(t, "carol", general.ID, `{"body":"after"}`)
	assert.Equal(t, posted.ID, next(t, adaLive).ID, "ada's live outlives bob's")
}

// TestSocket_RemovalFromTheOrganizationClosesBothSockets removes bob
// from acme: the session stands, so the code says refused rather than
// signed out. Ada, a member still, is the control.
func TestSocket_RemovalFromTheOrganizationClosesBothSockets(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "notes")
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	adaLive := s.listen(t, "ada", general.ID)
	bobPage, bobChannel := s.openPage(t, "bob", pageID), s.openChannel(t, "bob", general.ID)

	s.platform.remove("bob")

	assert.Equal(t, closeNoAccess, bobPage.closeCode(t))
	assert.Equal(t, closeNoAccess, bobChannel.closeCode(t))
	posted := s.post(t, "ada", general.ID, `{"body":"after"}`)
	assert.Equal(t, posted.ID, next(t, adaLive).ID)
}

// TestSocket_LosingReadClosesBothSockets denies bob reading the page
// and the channel, and bob alone: ada's connection to each is the
// control.
func TestSocket_LosingReadClosesBothSockets(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "notes")
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	adaLive := s.listen(t, "ada", general.ID)
	bobPage, bobChannel := s.openPage(t, "bob", pageID), s.openChannel(t, "bob", general.ID)

	s.platform.denyCheck("bob cowork:page-read@" + pageID)
	s.platform.denyCheck("bob cowork:channel-read@" + general.ID)

	assert.Equal(t, closeNoAccess, bobPage.closeCode(t))
	assert.Equal(t, closeNoAccess, bobChannel.closeCode(t))
	posted := s.post(t, "ada", general.ID, `{"body":"after"}`)
	assert.Equal(t, posted.ID, next(t, adaLive).ID)
}

// TestSocket_WriteAccessFollowsTheRecheck denies bob writing a page bob
// has open: the connection closes to reconnect, and the upgrade then
// makes it read-only, so bob's next edit is refused. Giving it back
// closes the read-only one the same way, and the edit is kept. Ada's
// connection, whose access never moves, is the control.
func TestSocket_WriteAccessFollowsTheRecheck(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "notes")
	ada := newEditor(t, s, "ada", pageID)
	ada.connect()
	writable := s.openPage(t, "bob", pageID)

	s.platform.denyCheck("bob cowork:page-write@" + pageID)
	require.Equal(t, websocket.StatusTryAgainLater, writable.closeCode(t))
	bob := newEditor(t, s, "bob", pageID)
	bob.connect()
	bob.appendBlock(collab.Block{Type: "paragraph", Text: "refused"})
	eventually(t, "bob's reconnected socket is read-only", bob.wasDenied)
	assert.Empty(t, ada.text(), "the refused edit was not relayed")

	readOnly := s.openPage(t, "bob", pageID)
	s.platform.allowCheck("bob cowork:page-write@" + pageID)
	require.Equal(t, websocket.StatusTryAgainLater, readOnly.closeCode(t))
	bob.disconnect()
	again := newEditor(t, s, "bob", pageID)
	again.connect()
	again.appendBlock(collab.Block{Type: "paragraph", Text: "kept"})
	eventually(t, "ada's connection reads bob's edit", func() bool { return ada.text() == "kept" })
}

// TestSocket_AnUnreachablePlatformClosesNothing takes the platform down
// under two open sockets and waits for re-checks that went unanswered.
// Neither socket closes; and once the platform answers again, a
// sign-out closes both, so the re-check was still running.
func TestSocket_AnUnreachablePlatformClosesNothing(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "notes")
	general := s.createChannel(t, "ada", `{"kind":"public","name":"general"}`)
	page, channel := s.openPage(t, "bob", pageID), s.openChannel(t, "bob", general.ID)

	s.platform.setDown(true)
	eventually(t, "re-checks reach the platform that is down", func() bool {
		return s.platform.callsWhileDown() >= 6
	})
	assert.Empty(t, page.closed, "the sync socket stays open")
	assert.Empty(t, channel.closed, "the live socket stays open")

	s.platform.setDown(false)
	s.platform.signOut("bob")
	assert.Equal(t, closeSignedOut, page.closeCode(t))
	assert.Equal(t, closeSignedOut, channel.closeCode(t))
}
