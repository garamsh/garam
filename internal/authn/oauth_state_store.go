package authn

import (
	"context"
	"time"
)

// OAuthStateStore persists the states a login issues, by the hash of
// the state. A state is live until it is consumed or its lifetime runs
// out, whichever comes first, so the callback that consumes one is the
// only callback that state completes.
type OAuthStateStore interface {
	// Issue records state as live for ttl, or returns
	// ErrOAuthStateRateLimited when MaxLiveOAuthStates are already
	// live. A refused issue still prunes the states that lapsed, so
	// shedding load cannot wedge the table.
	Issue(ctx context.Context, state string, ttl time.Duration) error
	// Consume spends a live state, or returns ErrOAuthStateNotLive.
	Consume(ctx context.Context, state string) error
}
