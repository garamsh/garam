package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/watch"
)

// watchCloseTimeout bounds dropping a watch's lease once the stream it
// belonged to is over. The lease lapses on its own, so this waits for
// the tidy answer rather than on it.
const watchCloseTimeout = 5 * time.Second

// keepAliveComment is what a live stream with nothing on it carries. A
// server-sent comment reaches no listener in the browser, so it says the
// connection is there without saying the agent spoke.
const keepAliveComment = ": keepalive\n\n"

// WatchAgent streams what the agent is saying to the caller, for as long
// as the caller holds the response open (ADR-0022). Requires
// `agent:watch`, which is granted separately from reading an agent:
// reading reports that it exists, and this exposes the content of its
// work.
//
// The stream starts here and replays nothing, because nothing was kept.
// It is best effort: an event dropped on the way is invisible by
// construction, so nothing in it may be read as a record of what the
// agent did.
func (s *Server) WatchAgent(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, agentSlug apigen.AgentSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := agentGRN(orgSlug, agentSlug)
	if err := s.authorizeOn(r, orgGRN(orgSlug), g, u, org.ActionAgentWatch); err != nil {
		writeError(w, err)
		return
	}
	// An agent no operator has reported does not answer here. A stream on
	// a GRN nothing holds would be silence, and silence on this stream is
	// what an agent that has said nothing looks like.
	if _, err := s.agents.Get(r.Context(), g); err != nil {
		writeError(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, errorx.New(errorx.KindInternal, "response cannot stream"))
		return
	}
	sub, err := s.watches.Watch(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	defer closeWatch(r, sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// The response reaches the caller before the agent says anything, so
	// a reader knows it is connected rather than waiting to find out.
	flusher.Flush()

	keepAlive := time.NewTicker(sub.KeepAlive())
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-sub.Events():
			if !open {
				return
			}
			if err := writeEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			// One tick proves both halves: the lease that has this stream
			// counted, and the connection that still takes bytes. A lease
			// that cannot be carried forward ends the stream — one nothing
			// forwards to shows a working agent as a quiet one.
			if err := sub.Refresh(r.Context()); err != nil {
				slog.WarnContext(r.Context(), "watch not refreshed",
					slog.String("agent", g.String()),
					slog.String("error", err.Error()))
				return
			}
			if _, err := io.WriteString(w, keepAliveComment); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// closeWatch ends the watch and drops its lease. The request's context
// is done by the time a stream is over, so the deletion gets one of its
// own; a lease this leaves behind lapses on its own, which is why a
// failure is reported rather than retried.
func closeWatch(r *http.Request, sub *watch.Subscription) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), watchCloseTimeout)
	defer cancel()
	if err := sub.Close(ctx); err != nil {
		slog.WarnContext(ctx, "watch not closed", slog.String("error", err.Error()))
	}
}

// writeEvent frames one event as a server-sent event: the platform's
// name for it, then the event itself.
//
// A body spanning lines is written as one data line per line, because
// the framing is line based — a newline written into one would end the
// event where the agent did not, and the rest of what it said would
// arrive as a field name nothing reads.
func writeEvent(w io.Writer, e watch.Event) error {
	var framed strings.Builder
	framed.WriteString("event: " + e.Kind() + "\n")
	for _, line := range strings.Split(lineEndings.Replace(e.Body()), "\n") {
		framed.WriteString("data: " + line + "\n")
	}
	framed.WriteString("\n")
	_, err := io.WriteString(w, framed.String())
	return err
}

// lineEndings normalizes what a platform may end a line with to the one
// the framing splits on.
var lineEndings = strings.NewReplacer("\r\n", "\n", "\r", "\n")
