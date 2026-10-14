package workload

import "context"

// AgentTokens is the agent's garam token, which a token-mode supervisor
// holds for its adapter (ADR-0100): it exchanges its own credential for
// a token of its own, mints the agent's with it, and mints the next
// inside the current one's refresh window. Garam's audience is the only
// one it mints, since it is the assignee and the lifecycle authority at
// once.
type AgentTokens interface {
	// Current answers the agent token to hand the adapter now. An error
	// of KindUnavailable is garam not answering, which a later call
	// retries; every other error is garam refusing the supervisor.
	Current(ctx context.Context) (string, error)
}
