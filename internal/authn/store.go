package authn

import (
	"context"
	"time"
)

// SessionStore persists sessions by the hash of their bearer token.
// Expired sessions behave as absent.
type SessionStore interface {
	// Create persists a new session for the raw token.
	Create(ctx context.Context, token string, s *Session) error
	// Get returns the live session for the raw token, or
	// ErrSessionNotFound.
	Get(ctx context.Context, token string) (*Session, error)
	// Delete revokes the session for the raw token.
	Delete(ctx context.Context, token string) error
}

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
