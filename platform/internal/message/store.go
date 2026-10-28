package message

import (
	"context"
	"time"

	"github.com/garamsh/garam/platform/internal/grn"
)

// Store is the queue itself: every message lives in it, and a reader
// reaches one only by claiming it (ADR-0013).
//
// Every method past Enqueue takes the GRN whose messages it acts on —
// the recipient for the queue's own operations, the sender for the read
// of one message's state — so what a caller may reach is decided by one
// GRN rather than by holding an identifier nobody else guessed.
//
// Each method also takes the agent execution fence its caller presents
// (ADR-0080) and runs it first, in the transaction of its own write, so
// a superseded generation's write is refused rather than committed. A
// nil fence is a caller that is not an agent.
type Store interface {
	// Enqueue stores the message and returns what is stored. A sender
	// key already used returns the message stored under it, so a sender
	// that retries stores one message and not two. Each call that
	// answers writes the send's audit event with what it stores
	// (ADR-0106).
	Enqueue(ctx context.Context, m *Message, fence *Fence) (*Message, error)
	// EnqueueWithinBudget stores the message of a durable submission
	// created at created, or returns what the sender's key already
	// stores, after the budget too (ADR-0083). A key storing a message
	// to another recipient, or with another body while it holds one,
	// returns ErrKeyHoldsAnotherMessage. Where the key stores nothing,
	// the database's clock decides when the insert runs: created after
	// it returns ErrCreatedLater, and created SubmissionBudget or more
	// before it ErrSubmissionExpired. It writes the send's audit event
	// as Enqueue does, and none where it returns an error.
	EnqueueWithinBudget(ctx context.Context, m *Message, created time.Time, fence *Fence) (*Message, error)
	// Refuse writes the audit event of a send from sender to recipient
	// that a policy denied, and stores no message (ADR-0106).
	Refuse(ctx context.Context, sender, recipient grn.GRN, fence *Fence) error
	// Claim hands the recipient's oldest ready message to exactly one
	// caller and holds it for lease, or returns ErrNoMessageReady. A
	// claim that is never completed lapses when the lease runs out and
	// the message is handed to the next caller, up to maxAttempts hands
	// in total: a message that spends them stops being redelivered
	// (ADR-0018). handOut, where not nil, runs on the claim before it is
	// committed, and an error from it leaves the message as it was, its
	// attempt unspent (ADR-0104).
	Claim(ctx context.Context, recipient grn.GRN, lease time.Duration, maxAttempts int, fence *Fence, handOut func(*Claim) error) (*Claim, error)
	// Renew extends a live claim's lease by lease, no further than
	// ceiling past the moment the claim was taken, and returns the hold
	// as it now stands. A claim that lapsed, was handed on, or was
	// completed returns ErrStaleClaim.
	Renew(ctx context.Context, recipient grn.GRN, id ClaimID, lease, ceiling time.Duration, fence *Fence) (*Claim, error)
	// Complete reports the claimed message delivered, or returns
	// ErrStaleClaim when the claim has since been handed on. Completing
	// twice with the same claim reports the same delivery.
	Complete(ctx context.Context, recipient grn.GRN, id ClaimID, fence *Fence) error
	// Status returns how far the sender's message has got, or
	// ErrNoSuchMessage when that sender stored none under the
	// identifier. maxAttempts is the bound Claim hands the message out
	// under, so the state answered here and the queue's own decision to
	// hand it out again read the same bound.
	Status(ctx context.Context, sender grn.GRN, id ID, maxAttempts int, fence *Fence) (*Status, error)
}
