package authn

import (
	"context"
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
