package machineapi

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// WithFeed makes a route that creates an agent answer once the policy
// service has applied the entity it marked (ADR-0165 item 6). The gate
// belongs to a process that runs no delivery loop: it reads the outbox
// and waits for the loop of `serve api` to empty it. Without it
// nothing is waited for, and the server answers as it did before.
func (s *Server) WithFeed(gate policyfeed.Gate) *Server {
	s.feed = gate
	return s
}

// awaitMarks holds the answer of a request that has committed a change
// until the policy service has applied the entities it marked, which
// every later request is decided from. The change is stored whatever
// it returns. At the bound the answer is the 503 every route here gives
// for what it cannot yet read as current, and its caller repeats the
// request under the same `requestId`: a replay answers what the first
// stored and waits again. This surface lists no reason for it
// (ADR-0086), so the body carries the kind alone.
func (s *Server) awaitMarks(ctx context.Context, marks ...policyfeed.Entity) error {
	if s.feed == nil || len(marks) == 0 {
		return nil
	}
	return s.feed.AwaitMarks(ctx, marks)
}

// agentMark is the mark of an agent's entity, and supervisorMark the
// mark of a supervisor's (ADR-0164).
func agentMark(g grn.GRN) policyfeed.Entity {
	return policyfeed.Entity{Type: policyfeed.AgentType, ID: g.String()}
}

func supervisorMark(g grn.GRN) policyfeed.Entity {
	return policyfeed.Entity{Type: policyfeed.SupervisorType, ID: g.String()}
}
