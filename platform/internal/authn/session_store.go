package authn

import (
	"context"

	"github.com/garamsh/garam/platform/internal/grn"
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
	// Refresh spends a live CLI session's refresh token for next, both
	// tokens replaced in one write, and answers the session, or
	// ErrRefreshTokenNotLive (ADR-0120).
	Refresh(ctx context.Context, refresh string, next CLITokens) (*Session, error)
	// List answers the user's sessions anything is still live in,
	// newest first.
	List(ctx context.Context, user grn.GRN) ([]*Session, error)
	// Revoke deletes the user's session id, or answers
	// ErrSessionNotFound where the user holds none by that id.
	Revoke(ctx context.Context, user grn.GRN, id string) error
}
