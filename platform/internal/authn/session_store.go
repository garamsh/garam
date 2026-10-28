package authn

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/grn"
)

// SessionStore persists sessions by the hash of their bearer token.
// Expired sessions behave as absent.
type SessionStore interface {
	// Create persists a new session for the raw token.
	Create(ctx context.Context, token string, s *Session) error
	// Get returns the live session for the raw token, or
	// ErrSessionNotFound.
	Get(ctx context.Context, token string) (*Session, error)
	// Delete ends the sign-in the raw token's session belongs to: that
	// session, the sign-in it was handed off from, and every session
	// handed off from that sign-in (ADR-0131).
	Delete(ctx context.Context, token string) error
	// IssueHandoff records code as a one-time handoff of the sign-in
	// the raw token's live browser session belongs to, redeemable with
	// state until ttl runs out, or answers ErrSessionNotFound where the
	// token names no live browser session (ADR-0131).
	IssueHandoff(ctx context.Context, token, code, state string, ttl time.Duration) error
	// RedeemHandoff spends a live handoff issued for code and state and
	// stores a session for the raw token next, of the same user, ending
	// with the sign-in it was handed off from; or answers
	// ErrHandoffNotLive.
	RedeemHandoff(ctx context.Context, code, state, next string) (*Session, error)
	// Refresh spends a live CLI session's refresh token for next, both
	// tokens replaced in one write, and answers the session, or
	// ErrRefreshTokenNotLive (ADR-0120).
	Refresh(ctx context.Context, refresh string, next CLITokens) (*Session, error)
	// List answers the user's sessions anything is still live in,
	// newest first.
	List(ctx context.Context, user grn.GRN) ([]*Session, error)
	// Revoke ends the sign-in the user's session id belongs to, as
	// Delete does, or answers
	// ErrSessionNotFound where the user holds none by that id.
	Revoke(ctx context.Context, user grn.GRN, id string) error
}
