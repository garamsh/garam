package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/coder/websocket"

	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
)

// maxMessage bounds one message a client sends: an update the size of
// a large paste, and no more.
const maxMessage = 8 << 20

// SyncPath is where a page's content is edited, as y-websocket's
// provider reaches it: its URL, then the room name, which is the
// page's org and ID.
const SyncPath = "GET /orgs/{org}/pages/{page}/sync"

// wsConn adapts a WebSocket to collab.Conn: one binary message each.
type wsConn struct{ c *websocket.Conn }

func (w wsConn) Read(ctx context.Context) ([]byte, error) {
	kind, b, err := w.c.Read(ctx)
	if err != nil {
		return nil, err
	}
	if kind != websocket.MessageBinary {
		return nil, collab.ErrMalformedMessage
	}
	return b, nil
}

func (w wsConn) Write(ctx context.Context, msg []byte) error {
	return w.c.Write(ctx, websocket.MessageBinary, msg)
}

// SyncPage upgrades to a WebSocket for one page's content. Everything
// it can refuse, it refuses before the upgrade, as getPage would:
// cowork:page-read on the page, which also proves it exists. Whether
// the connection's edits are kept is cowork:page-write on the page.
// Both are asked again while it is open (ADR-0139): a changed answer
// to the second closes it to reconnect, and the upgrade decides again.
func (s *Server) SyncPage(w http.ResponseWriter, r *http.Request) {
	org, id := r.PathValue("org"), r.PathValue("page")
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if _, err := s.pages.Get(r.Context(), c, org, id); err != nil {
		writeError(w, r, err)
		return
	}
	allowed, err := s.platform.Allowed(r.Context(), c, org, []platform.Check{platform.OnPage(platform.ActionPageWrite, id)})
	if err != nil {
		writeError(w, r, err)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.origins})
	if err != nil {
		// Accept has answered the request itself.
		return
	}
	conn.SetReadLimit(maxMessage)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go s.recheckSocket(ctx, conn, s.pageRecheck(c, org, id, allowed[0]))
	err = s.collab.Serve(ctx, collab.Room{Org: org, Page: id, Writable: allowed[0]}, wsConn{conn})
	closeFor(conn, err)
}

// closeFor ends the connection with the close code that says why.
func closeFor(conn *websocket.Conn, err error) {
	var closeErr websocket.CloseError
	switch {
	case err == nil, errors.As(err, &closeErr), errors.Is(err, context.Canceled):
		_ = conn.Close(websocket.StatusNormalClosure, "")
	case errorx.IsKind(err, errorx.KindInvalidArgument):
		_ = conn.Close(websocket.StatusProtocolError, "malformed message")
	case errorx.IsKind(err, errorx.KindUnauthenticated):
		_ = conn.Close(closeSignedOut, "signed out")
	case errorx.IsKind(err, errorx.KindPermissionDenied):
		_ = conn.Close(closeNoAccess, "no longer allowed")
	case errors.Is(err, errWritableChanged):
		_ = conn.Close(websocket.StatusTryAgainLater, "write access changed")
	case errorx.IsKind(err, errorx.KindNotFound):
		_ = conn.Close(websocket.StatusPolicyViolation, "it no longer exists")
	case errorx.IsKind(err, errorx.KindResourceExhausted):
		_ = conn.Close(websocket.StatusTryAgainLater, "too far behind")
	default:
		_ = conn.Close(websocket.StatusInternalError, "internal error")
	}
}
