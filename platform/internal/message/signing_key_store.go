package message

import (
	"context"
	"time"
)

// SigningKeyStore holds the keys garam signs messages with (ADR-0101).
// Nothing is deleted from it, so the newest key that has begun signing
// is always the last one created to sign, withdrawn or not.
type SigningKeyStore interface {
	// SigningKeys answers every key, in the order they take over
	// signing, and the database's clock when they were read.
	SigningKeys(ctx context.Context) ([]*SigningKey, time.Time, error)
	// RotateSigningKeys holds a lock every replica's rotation takes,
	// reads the keys and the clock under it, and stores the key prepare
	// answers. prepare answers nil where no key is due.
	RotateSigningKeys(ctx context.Context, prepare func(keys []*SigningKey, now time.Time) (*NewSigningKey, error)) error
	// WithdrawSigningKey stops a key signing and removes it from the
	// published set at once, or answers ErrNoSuchSigningKey.
	WithdrawSigningKey(ctx context.Context, id KeyID) error
}
