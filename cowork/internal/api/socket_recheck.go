package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/coder/websocket"

	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
)

// SocketRecheck is how often an open WebSocket's caller is asked of
// the platform again (ADR-0139). A refusal closes the connection
// within this and one request's timeout.
const SocketRecheck = 30 * time.Second

// Close codes a client reads in the range RFC 6455 leaves to
// applications, each the HTTP status its upgrade would now be answered
// (cowork-web/src/lib/socket-close.ts).
const (
	closeSignedOut websocket.StatusCode = 4401
	closeNoAccess  websocket.StatusCode = 4403
)

// errWritableChanged ends a sync connection whose cowork:page-write
// answer moved. The client reconnects, and the upgrade decides again.
var errWritableChanged = errorx.New(errorx.KindFailedPrecondition, "api: the connection's write access changed")

// recheckSocket runs recheck every s.recheck until ctx ends, and closes
// conn on a refusal. Anything else decides nothing, so a platform that
// cannot be reached leaves the connection as it stands.
func (s *Server) recheckSocket(ctx context.Context, conn *websocket.Conn, recheck func(context.Context) error) {
	ticker := time.NewTicker(s.recheck)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		err := recheck(ctx)
		switch {
		case err == nil, ctx.Err() != nil:
		case refuses(err):
			// Closing the connection, rather than ending ctx, is what
			// reaches the client with this code: a read whose ctx ends
			// drops the connection with no close frame.
			closeFor(conn, err)
			return
		default:
			slog.WarnContext(ctx, "cowork: socket re-check failed", slog.String("error", err.Error()))
		}
	}
}

// refuses reports whether a re-check's error is an answer that ends
// the connection, rather than a failure to get one.
func refuses(err error) bool {
	switch errorx.KindOf(err) {
	case errorx.KindUnauthenticated, errorx.KindPermissionDenied, errorx.KindNotFound, errorx.KindFailedPrecondition:
		return true
	}
	return false
}

// pageRecheck is one sync connection's re-check, in one request to the
// platform: reading the page, and writing it as the connection does.
func (s *Server) pageRecheck(c platform.Caller, org, id string, writable bool) func(context.Context) error {
	checks := []platform.Check{platform.OnPage(platform.ActionPageRead, id), platform.OnPage(platform.ActionPageWrite, id)}
	return func(ctx context.Context) error {
		allowed, err := s.platform.Allowed(ctx, c, org, checks)
		switch {
		case err != nil:
			return err
		case !allowed[0]:
			return platform.ErrNotAllowed
		case allowed[1] != writable:
			return errWritableChanged
		}
		return nil
	}
}
