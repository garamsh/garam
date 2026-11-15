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

// awaitAgent holds the answer of a request that has committed the
// creation of agent until the policy service has applied its entity,
// which every later request of the agent is decided from. The agent is
// stored whatever it returns. At the bound the answer is the 503 every
// route here gives for what it cannot yet read as current, and its
// caller repeats the request under the same `requestId`: a replay
// answers what the first stored and waits again. This surface lists no
// reason for it (ADR-0086), so the body carries the kind alone.
func (s *Server) awaitAgent(ctx context.Context, agent grn.GRN) error {
	if s.feed == nil {
		return nil
	}
	return s.feed.AwaitMarks(ctx, []policyfeed.Entity{{Type: policyfeed.AgentType, ID: agent.String()}})
}
