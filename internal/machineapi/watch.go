package machineapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/watch"
)

// ForwardTurnEvent hands one event from the agent's monitoring stream to
// whoever is watching it, and to nobody when no one is. A caller
// forwards for the agent it authenticates as and is refused every other
// one, which `internal/watch` decides as `internal/message` decides
// whose queue a caller may act on.
//
// The answer says the event was announced and never that a person saw
// it: nothing is stored, so there is nothing that could report a
// delivery (ADR-0022).
func (s *Server) ForwardTurnEvent(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) {
	caller, agent, ok := parties(w, r, agentGRN)
	if !ok {
		return
	}
	var forwarded machinegen.TurnEvent
	if err := json.NewDecoder(r.Body).Decode(&forwarded); err != nil {
		writeError(w, errorx.Wrap(err, "turn event", errorx.KindInvalidArgument))
		return
	}
	event, err := watch.NewEvent(forwarded.Kind, forwarded.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.watches.Publish(r.Context(), caller, agent, event); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// watchers is what the poll response tells the adapter: how many streams
// garam is holding for the agent, so an agent nobody watches is answered
// with zero rather than with a different shape (ADR-0022).
//
// A count that cannot be read answers zero and is logged rather than
// returned. Forwarding is best effort and the queue is not: failing the
// poll would spend one of the message's delivery attempts (ADR-0018) on
// the live view, which is the direction ADR-0022 refuses — a watcher
// must not degrade what another decision rests on.
func (s *Server) watchers(ctx context.Context, caller, agent grn.GRN) int {
	count, err := s.watches.Watchers(ctx, caller, agent)
	if err != nil {
		slog.WarnContext(ctx, "watcher count unread",
			slog.String("agent", agent.String()),
			slog.String("error", err.Error()))
		return 0
	}
	return count
}
