package authn

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/grn"
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

// PasswordCredentialStore persists the verifier that authenticates a
// user's password, one row per user that has one. The password itself
// never reaches it — only the Argon2id verifier derived from it
// (ADR-0041).
type PasswordCredentialStore interface {
	// Create records the first verifier for a user.
	Create(ctx context.Context, userGRN grn.GRN, v Verifier) error
	// Get returns the user's verifier, or
	// ErrPasswordCredentialNotFound.
	Get(ctx context.Context, userGRN grn.GRN) (Verifier, error)
	// Replace writes a verifier over the one held, which is how a
	// sign-in under parameters other than the current policy re-hashes
	// without asking anyone to reset.
	Replace(ctx context.Context, userGRN grn.GRN, v Verifier) error
}

// PasswordAttemptStore bounds how many password verifications may be
// live at once, so an unauthenticated caller cannot drive Argon2id
// computations at the rate requests arrive. An attempt is recorded
// before any verifier is computed and released when one verifies.
type PasswordAttemptStore interface {
	// Record marks an attempt live for ttl, or returns
	// ErrPasswordAttemptRateLimited when MaxLivePasswordAttempts are
	// already live. A refused record still prunes what lapsed, so
	// shedding load cannot wedge the table.
	Record(ctx context.Context, attempt string, ttl time.Duration) error
	// Release frees a recorded attempt's slot. Releasing an unknown
	// attempt is not an error.
	Release(ctx context.Context, attempt string) error
}
