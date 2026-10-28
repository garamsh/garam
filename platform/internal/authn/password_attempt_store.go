package authn

import (
	"context"
	"time"
)

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
