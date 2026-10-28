package authn

import (
	"context"

	"github.com/garamsh/garam/platform/internal/grn"
)

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
