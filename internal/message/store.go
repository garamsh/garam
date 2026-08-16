package message

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Store is the queue itself: every message lives in it, and a reader
// reaches one only by claiming it (ADR-0013).
type Store interface {
	// Enqueue stores the message and returns what is stored. A sender
	// key already used returns the message stored under it, so a sender
	// that retries stores one message and not two.
	Enqueue(ctx context.Context, m *Message) (*Message, error)
	// Claim hands the recipient's oldest ready message to exactly one
	// caller and holds it for lease, or returns ErrNoMessageReady. A
	// claim that is never completed lapses when the lease runs out and
	// the message is handed to the next caller.
	Claim(ctx context.Context, recipient grn.GRN, lease time.Duration) (*Claim, error)
	// Complete reports the claimed message delivered, or returns
	// ErrStaleClaim when the claim has since been handed on. Completing
	// twice with the same claim reports the same delivery.
	Complete(ctx context.Context, c *Claim) error
}
