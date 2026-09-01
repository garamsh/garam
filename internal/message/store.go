package message

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Store is the queue itself: every message lives in it, and a reader
// reaches one only by claiming it (ADR-0013).
//
// Every method past Enqueue takes the GRN whose messages it acts on —
// the recipient for the queue's own operations, the sender for the read
// of one message's state — so what a caller may reach is decided by one
// GRN rather than by holding an identifier nobody else guessed.
type Store interface {
	// Enqueue stores the message and returns what is stored. A sender
	// key already used returns the message stored under it, so a sender
	// that retries stores one message and not two.
	Enqueue(ctx context.Context, m *Message) (*Message, error)
	// Claim hands the recipient's oldest ready message to exactly one
	// caller and holds it for lease, or returns ErrNoMessageReady. A
	// claim that is never completed lapses when the lease runs out and
	// the message is handed to the next caller, up to maxAttempts hands
	// in total: a message that spends them stops being redelivered
	// (ADR-0018).
	Claim(ctx context.Context, recipient grn.GRN, lease time.Duration, maxAttempts int) (*Claim, error)
	// Renew extends a live claim's lease by lease, no further than
	// ceiling past the moment the claim was taken, and returns the hold
	// as it now stands. A claim that lapsed, was handed on, or was
	// completed returns ErrStaleClaim.
	Renew(ctx context.Context, recipient grn.GRN, id ClaimID, lease, ceiling time.Duration) (*Claim, error)
	// Complete reports the claimed message delivered, or returns
	// ErrStaleClaim when the claim has since been handed on. Completing
	// twice with the same claim reports the same delivery.
	Complete(ctx context.Context, recipient grn.GRN, id ClaimID) error
	// Status returns how far the sender's message has got, or
	// ErrNoSuchMessage when that sender stored none under the
	// identifier. maxAttempts is the bound Claim hands the message out
	// under, so the state answered here and the queue's own decision to
	// hand it out again read the same bound.
	Status(ctx context.Context, sender grn.GRN, id ID, maxAttempts int) (*Status, error)
}
