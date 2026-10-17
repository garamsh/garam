package message

import (
	"context"
	"strconv"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Delivery parameters. They default narrow, and the reason is dated: a
// repeated turn is unmeasured on the agent platform, so a redelivery
// costs more than a late one until that changes (ADR-0018).
const (
	// DefaultLease is what a claim holds a message for when [Config]
	// says nothing. A turn takes seconds, and a holder that is still
	// working renews.
	DefaultLease = 30 * time.Second
	// DefaultLeaseCeiling is how far renewals may carry one claim. It
	// is a backstop for a completion that never arrives rather than a
	// prediction of how long a turn takes, so it only has to exceed a
	// normal one.
	DefaultLeaseCeiling = 5 * time.Minute
	// DefaultAttempts is how many times a message is handed out before
	// it stops being redelivered.
	DefaultAttempts = 3
	// MinAttempts is the fewest that delivers a message at all.
	MinAttempts = 1
	// MaxAttempts is the most this package will hand one message out.
	// Past it the bound is switched off rather than configured, and
	// that is a decision to record rather than a value to set.
	MaxAttempts = 100
	// DefaultWait is how long a claim waits for a message before
	// answering that there is none. The caller asks again.
	DefaultWait = 30 * time.Second
	// DefaultPollInterval is how often a waiting claim re-reads the
	// queue, which is the delivery latency ADR-0013 traded a push for.
	DefaultPollInterval = 250 * time.Millisecond
)

// Config holds the queue's delivery parameters. A zero field takes the
// default above; the constructor refuses a value that would deliver
// nothing or renew past its own backstop.
type Config struct {
	// Lease is how long a claim holds a message before it is claimable
	// again.
	Lease time.Duration
	// LeaseCeiling is the furthest renewals may carry one claim,
	// measured from when it was taken. It is never shorter than Lease.
	LeaseCeiling time.Duration
	// Attempts is how many times one message is handed out in total.
	Attempts int
	// Wait is how long [Service.Claim] waits before answering that
	// nothing is ready.
	Wait time.Duration
	// PollInterval is how often a waiting claim re-reads the queue.
	PollInterval time.Duration
}

// Service is what a machine identity does to its own queue: it posts a
// message to a recipient, claims one addressed to itself, holds the
// claim while it works, reports the message delivered, and reads how
// far a message it sent has got.
//
// Every method between Post and Status takes the calling identity and
// the recipient whose queue it names, and answers only where the two
// are the same identity — the whole of the authorization here. Status
// is bounded the same way from the other side: it reads under the
// caller as the sender (ADR-0055).
//
// An agent's call carries the execution fence its request presents
// (ADR-0080), and the store runs it with the write. A sender that is
// not an agent carries none.
type Service interface {
	// Post stores a message from sender to recipient, written by
	// author, and answers with what is stored. Posting the same
	// idempotency key twice stores one message.
	Post(ctx context.Context, sender, recipient grn.GRN, idempotencyKey, body string, author Author, fence *Fence) (*Message, error)
	// PostWithinBudget is Post for a durable submission created at
	// created: it stores a message only inside SubmissionBudget of that
	// instant, and answers what the key already stores at any time
	// (ADR-0083).
	PostWithinBudget(ctx context.Context, sender, recipient grn.GRN, idempotencyKey, body string, author Author, created time.Time, fence *Fence) (*Message, error)
	// Claim hands the caller the oldest message addressed to it, waiting
	// until one is ready rather than answering an empty queue at once,
	// and returns ErrNoMessageReady when none arrives in that time. The
	// claim is signed by signer before it is taken for good, so a claim
	// that cannot be signed spends none of the message's attempts
	// (ADR-0104).
	Claim(ctx context.Context, caller, recipient grn.GRN, fence *Fence, signer Signer) (*Claim, Signed, error)
	// Renew extends the caller's hold on a message it is still working
	// on, no further than the ceiling.
	Renew(ctx context.Context, caller, recipient grn.GRN, id ClaimID, fence *Fence) (*Claim, error)
	// Complete reports the claimed message delivered.
	Complete(ctx context.Context, caller, recipient grn.GRN, id ClaimID, fence *Fence) error
	// Status answers how far a message the caller sent has got, or
	// ErrNoSuchMessage when the caller sent none under the identifier
	// (ADR-0055). It is the one method a sender rather than a recipient
	// calls, and the sender it reads under is the caller itself.
	Status(ctx context.Context, caller grn.GRN, id ID, fence *Fence) (*Status, error)
}

type service struct {
	store Store
	cfg   Config
}

// NewService constructs the queue's service over the store it reads,
// or refuses a configuration that would deliver nothing.
func NewService(store Store, cfg Config) (Service, error) {
	cfg = withDefaults(cfg)
	switch {
	case cfg.Lease <= 0:
		return nil, errorx.New(errorx.KindInvalidArgument, "message: lease must be positive")
	case cfg.LeaseCeiling < cfg.Lease:
		return nil, errorx.New(errorx.KindInvalidArgument, "message: lease ceiling must not be shorter than the lease")
	case cfg.Attempts < MinAttempts || cfg.Attempts > MaxAttempts:
		return nil, errorx.New(errorx.KindInvalidArgument,
			"message: attempts must be between "+strconv.Itoa(MinAttempts)+" and "+strconv.Itoa(MaxAttempts))
	case cfg.Wait < 0:
		return nil, errorx.New(errorx.KindInvalidArgument, "message: wait must not be negative")
	case cfg.PollInterval <= 0:
		return nil, errorx.New(errorx.KindInvalidArgument, "message: poll interval must be positive")
	}
	return &service{store: store, cfg: cfg}, nil
}

// withDefaults fills what the caller left zero.
func withDefaults(cfg Config) Config {
	if cfg.Lease == 0 {
		cfg.Lease = DefaultLease
	}
	if cfg.LeaseCeiling == 0 {
		cfg.LeaseCeiling = DefaultLeaseCeiling
	}
	if cfg.Attempts == 0 {
		cfg.Attempts = DefaultAttempts
	}
	if cfg.Wait == 0 {
		cfg.Wait = DefaultWait
	}
	if cfg.PollInterval == 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	return cfg
}

// Post stores the message. The recipient is not checked against the
// agents garam knows: garam answers a sender once the message is
// stored, and reports a routing failure back as a message of its own
// rather than as a refusal (ADR-0013).
//
// fence is the requesting agent's, which is not always sender: a
// refusal garam stores in reply is sent under the intended recipient's
// identity, and it is still the requester's write that is fenced. Its
// author is garam (ADR-0101).
func (s *service) Post(ctx context.Context, sender, recipient grn.GRN, idempotencyKey, body string, author Author, fence *Fence) (*Message, error) {
	m, err := newAuthored(recipient, sender, idempotencyKey, body, author)
	if err != nil {
		return nil, err
	}
	return s.store.Enqueue(ctx, m, fence)
}

// PostWithinBudget is Post, with the budget judged by the store at its
// insert rather than here: a request delayed on its way there is judged
// when it writes.
func (s *service) PostWithinBudget(ctx context.Context, sender, recipient grn.GRN, idempotencyKey, body string, author Author, created time.Time, fence *Fence) (*Message, error) {
	m, err := newAuthored(recipient, sender, idempotencyKey, body, author)
	if err != nil {
		return nil, err
	}
	return s.store.EnqueueWithinBudget(ctx, m, created, fence)
}

func (s *service) Claim(ctx context.Context, caller, recipient grn.GRN, fence *Fence, signer Signer) (*Claim, Signed, error) {
	if err := addressesFenced(caller, recipient, fence); err != nil {
		return nil, Signed{}, err
	}
	var signed Signed
	sign := func(c *Claim) error {
		var err error
		signed, err = signer.Sign(ctx, c)
		return err
	}
	// A message becomes ready either because a sender stored one or
	// because a lease ran out, and the second is a moment passing rather
	// than a write — nothing announces it, and a wait listening for an
	// announcement would sleep through every redelivery. So the wait
	// re-reads the queue, and the interval is the delivery latency
	// ADR-0013 accepted in place of a push.
	deadline := time.Now().Add(s.cfg.Wait)
	for {
		claim, err := s.store.Claim(ctx, recipient, s.cfg.Lease, s.cfg.Attempts, fence, sign)
		// The store has one not-found to answer this with, so the kind
		// names it and no sentinel needs telling apart.
		if !errorx.IsKind(err, errorx.KindNotFound) {
			if err != nil {
				return nil, Signed{}, err
			}
			return claim, signed, nil
		}
		if !time.Now().Before(deadline) {
			return nil, Signed{}, ErrNoMessageReady
		}
		select {
		case <-ctx.Done():
			// The caller stopped waiting, so there is nobody to hand a
			// message to and nothing to report.
			return nil, Signed{}, ErrNoMessageReady
		case <-time.After(s.cfg.PollInterval):
		}
	}
}

func (s *service) Renew(ctx context.Context, caller, recipient grn.GRN, id ClaimID, fence *Fence) (*Claim, error) {
	if err := addressesFenced(caller, recipient, fence); err != nil {
		return nil, err
	}
	return s.store.Renew(ctx, recipient, id, s.cfg.Lease, s.cfg.LeaseCeiling, fence)
}

func (s *service) Complete(ctx context.Context, caller, recipient grn.GRN, id ClaimID, fence *Fence) error {
	if err := addressesFenced(caller, recipient, fence); err != nil {
		return err
	}
	return s.store.Complete(ctx, recipient, id, fence)
}

// Status reads under the caller as the sender, so there is no second
// identity to compare and nothing to refuse: a message the caller did
// not send matches no row. The attempt bound travels with the read, so
// the state a sender is told is the one the claim predicate would act
// on rather than a second reading of the same columns.
func (s *service) Status(ctx context.Context, caller grn.GRN, id ID, fence *Fence) (*Status, error) {
	return s.store.Status(ctx, caller, id, s.cfg.Attempts, fence)
}

// addresses refuses a caller acting on a queue that is not its own. The
// caller is the GRN its certificate names and a GRN carries its
// organization, so this one comparison is the authorization and the
// tenancy check at once — there is no path to a message that does not
// pass it.
func addresses(caller, recipient grn.GRN) error {
	if caller != recipient {
		return ErrNotRecipient
	}
	return nil
}

// addressesFenced is addresses for a call that writes the recipient's
// queue. The transport decides which callers present a fence; one
// presented for another identity is its defect, not a refusal of the
// caller.
func addressesFenced(caller, recipient grn.GRN, fence *Fence) error {
	if err := addresses(caller, recipient); err != nil {
		return err
	}
	if fence != nil && fence.Agent != caller {
		return errorx.New(errorx.KindInternal, "message: the fence does not match the caller")
	}
	return nil
}
