package delivery

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

const (
	// DefaultRetryDelay is how long the loop waits before going back to
	// a call that failed or a subscription that dropped. It is not a
	// backoff: the two ends are a queue this adapter is the only reader
	// of and a gateway one hop away, so a failure here is a restart or
	// an outage rather than contention.
	DefaultRetryDelay = time.Second
	// DefaultMinRenewInterval is the shortest gap between two renewals
	// of one claim. A renewal held at the ceiling answers with the same
	// expiry it was given, so without a floor the interval below would
	// shrink toward zero as a claim approaches it.
	DefaultMinRenewInterval = time.Second
	// DefaultClaimTimeout bounds one claim. A claim is a long poll that
	// answers 204 when nothing arrived, so the bound has to clear
	// garam's wait — thirty seconds (message.DefaultWait) — with room
	// for the answer to travel, or every empty poll becomes an error.
	DefaultClaimTimeout = 60 * time.Second
	// DefaultCallTimeout bounds the calls that are not long polls: the
	// post to the gateway, and the renewal and completion hold makes
	// inline. It sits under one renewal interval — a lease divided by
	// renewDivisor, and garam's default lease is thirty seconds — so a
	// peer that stops answering costs less than the gap to the next
	// renewal rather than stopping every other claim behind it.
	DefaultCallTimeout = 5 * time.Second
	// DefaultStatusInterval is how often the runtime's status and the
	// mounted credentials are read. It bounds how long a new generation
	// or a renewed certificate waits to be acted on, and nothing else
	// waits on it.
	DefaultStatusInterval = 5 * time.Second
	// DefaultOutboxInterval is how often the outbox is read for replies
	// the runtime recorded.
	DefaultOutboxInterval = 2 * time.Second
	// DefaultMaxRetryDelay caps the backoff an undecided answer from
	// garam or the lifecycle authority doubles up to, so an outage is
	// retried at most this often and never in a hot loop.
	DefaultMaxRetryDelay = time.Minute
	// renewDivisor is how many renewals fall inside one lease. Three
	// means two may be lost — to a paused process or a slow call —
	// before a claim whose turn is still running lapses.
	renewDivisor = 3
)

// Config tunes what the loop does with the lease garam hands it. A zero
// field takes the default above; the constructor refuses a value that
// would renew nothing or spin.
type Config struct {
	// RetryDelay is how long to wait before retrying a failed call or
	// reopening a dropped subscription.
	RetryDelay time.Duration
	// MinRenewInterval bounds how often one claim is renewed. A lease
	// shorter than renewDivisor times this cannot be held at all —
	// garam's default lease is thirty seconds.
	MinRenewInterval time.Duration
	// ClaimTimeout bounds one claim, which is a long poll and needs a
	// bound above garam's wait.
	ClaimTimeout time.Duration
	// CallTimeout bounds every call that is not a long poll.
	CallTimeout time.Duration
	// StatusInterval is how often the runtime's status and the mounted
	// credentials are read.
	StatusInterval time.Duration
	// OutboxInterval is how often the outbox is read.
	OutboxInterval time.Duration
	// MaxRetryDelay caps the backoff of an undecided answer.
	MaxRetryDelay time.Duration
	// ForwardSignature sends garam's signature beside the content it
	// covers (ADR-0101). It is off unless whoever runs the adapter
	// turns it on, because nothing the adapter can read says whether
	// the runtime accepts the member or trusts garam's issuer, and a
	// runtime that does neither refuses every signed message.
	ForwardSignature bool
}

// Ends is the agent the loop runs beside, what it carries between and
// what it presents. Authority is what activates the execution, and
// Outbox where the runtime records its replies.
type Ends struct {
	Agent       grn.GRN
	Queue       Queue
	Gateway     Gateway
	Execution   *Execution
	Credentials Credentials
	Authority   Authority
	Outbox      *Outbox
}

// Service is the delivery loop of docs/architecture/adapter.md: hold
// an activation of the runtime's generation, claim a message with its lease, post it into the gateway under its
// own identifier, watch for a turn ending, complete that message's
// claim when the runtime says a turn that consumed it committed, and
// forward the replies the runtime records in its outbox.
type Service interface {
	// Run carries messages until ctx is done, and answers nil then,
	// because a requested stop is not a failure. Nothing else ends it:
	// a garam, a lifecycle authority or a gateway that is not answering
	// is waited on and tried again, and a refusal pauses protected work
	// rather than exiting, since a sidecar that exited would take its
	// agent's only route to the queue with it.
	Run(ctx context.Context) error
}

type service struct {
	ends Ends
	cfg  Config
}

// NewService constructs the loop over the ends it carries between, or
// refuses a configuration that would renew nothing or an execution
// with nothing to activate it or forward its replies.
func NewService(ends Ends, cfg Config) (Service, error) {
	cfg = withDefaults(cfg)
	switch {
	case ends.Agent.Validate() != nil:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: the agent's GRN is required")
	case ends.Queue == nil || ends.Gateway == nil || ends.Execution == nil || ends.Credentials == nil:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: the queue, the gateway, the execution and the credentials are required")
	case ends.Authority == nil || ends.Outbox == nil:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: an execution needs a lifecycle authority and an outbox")
	case cfg.RetryDelay <= 0 || cfg.MaxRetryDelay < cfg.RetryDelay:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: retry delay must be positive and under its cap")
	case cfg.MinRenewInterval <= 0:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: minimum renew interval must be positive")
	case cfg.ClaimTimeout <= 0 || cfg.CallTimeout <= 0:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: call bounds must be positive")
	case cfg.StatusInterval <= 0 || cfg.OutboxInterval <= 0:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: status and outbox intervals must be positive")
	}
	return &service{ends: ends, cfg: cfg}, nil
}

// withDefaults fills what the caller left zero.
func withDefaults(cfg Config) Config {
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = DefaultRetryDelay
	}
	if cfg.MinRenewInterval == 0 {
		cfg.MinRenewInterval = DefaultMinRenewInterval
	}
	if cfg.ClaimTimeout == 0 {
		cfg.ClaimTimeout = DefaultClaimTimeout
	}
	if cfg.CallTimeout == 0 {
		cfg.CallTimeout = DefaultCallTimeout
	}
	if cfg.StatusInterval == 0 {
		cfg.StatusInterval = DefaultStatusInterval
	}
	if cfg.OutboxInterval == 0 {
		cfg.OutboxInterval = DefaultOutboxInterval
	}
	if cfg.MaxRetryDelay == 0 {
		cfg.MaxRetryDelay = max(DefaultMaxRetryDelay, cfg.RetryDelay)
	}
	return cfg
}

// pairing is one message in flight: the hold garam gave it and when
// that hold is next carried forward. done is a message the runtime had
// already completed when it was posted, whose claim completes at once.
type pairing struct {
	claim   *Claim
	renewAt time.Time
	done    bool
}

// Run starts the readers and holds the pairings between them.
//
// The subscription is opened before the first message is posted, and
// nothing is claimed until it is: a turn that ends before anyone is
// reading completes nothing, and its message would come back only once
// the lease had lapsed.
func (s *service) Run(ctx context.Context) error {
	posted := make(chan pairing)
	ended := make(chan TurnEnd)
	subscribed := make(chan struct{})
	reopened := make(chan struct{}, 1)

	go s.supervise(ctx)
	go s.readTurns(ctx, ended, subscribed, reopened)
	select {
	case <-subscribed:
	case <-ctx.Done():
		return nil
	}
	go s.claim(ctx, posted)
	if s.ends.Outbox != nil {
		go s.forward(ctx)
	}
	s.hold(ctx, posted, ended, reopened)
	return nil
}

// hold owns every pairing this adapter is carrying, keyed by garam's
// message identifier. One goroutine holds the map, so what is in flight
// needs no lock and no second reader.
//
// The map is the whole of the adapter's memory of what it has delivered
// and it reaches no disk: a restart lapses the leases in it rather than
// losing the messages, the runtime recognises a redelivered message by
// its identifier, and reconciling what was in it is garam's
// (ADR-0018, ADR-0079).
func (s *service) hold(ctx context.Context, posted <-chan pairing, ended <-chan TurnEnd, reopened <-chan struct{}) {
	held := map[MessageID]pairing{}
	for {
		due, stop := s.renewalTimer(held)
		select {
		case <-ctx.Done():
			stop()
			return
		case p := <-posted:
			if p.done {
				delete(held, p.claim.Message)
				s.completeClaim(ctx, p.claim)
			} else {
				held[p.claim.Message] = p
			}
		case e := <-ended:
			s.complete(ctx, held, e)
		case <-reopened:
			s.reconcile(ctx, held)
		case <-due:
			s.renew(ctx, held)
		}
		stop()
	}
}

// claim holds one long poll on garam and posts what it answers with,
// while the execution lets protected calls through. The pairing
// travels to hold rather than into the map directly, so one goroutine
// owns everything in flight. A turn that ends before its pairing
// arrives completes nothing and the message is redelivered, which the
// runtime then answers as completed.
func (s *service) claim(ctx context.Context, posted chan<- pairing) {
	// refused is the backoff while garam refuses the agent token, which
	// the assignee replaces in its file; each attempt reads it again.
	var refused time.Duration
	for ctx.Err() == nil {
		if err := s.ends.Execution.Await(ctx); err != nil {
			return
		}
		claimCtx, cancel := context.WithTimeout(ctx, s.cfg.ClaimTimeout)
		claim, err := s.ends.Queue.Claim(claimCtx)
		cancel()
		if errorx.IsKind(err, errorx.KindNotFound) {
			// The poll waited its time at garam, so asking again at once
			// is the loop rather than a spin. Only the empty answer is
			// not-found (stack-go.md §4).
			continue
		}
		if errors.Is(err, ErrTokenRefused) {
			refused = s.backoff(refused)
			slog.WarnContext(ctx, "agent token refused; reading it again after a backoff",
				slog.Duration("backoff", refused), slog.String("error", err.Error()))
			s.wait(ctx, refused)
			continue
		}
		refused = 0
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			s.claimRefused(ctx, err)
			continue
		}
		p, err := s.post(ctx, claim)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Nothing is known to be accepted, so there is nothing to
			// pair: the lease lapses and garam hands the message out
			// again, under the same identifier.
			slog.WarnContext(ctx, "message not posted",
				slog.String("message", string(claim.Message)),
				slog.String("claim", string(claim.ID)),
				slog.String("error", err.Error()))
			s.wait(ctx, s.cfg.RetryDelay)
			continue
		}
		select {
		case posted <- p:
		case <-ctx.Done():
			return
		}
	}
}

// claimRefused logs a claim garam did not answer with a message. A
// refused token or credential has closed the execution already, so the
// next Await is what waits; anything else waits the retry delay.
func (s *service) claimRefused(ctx context.Context, err error) {
	if errorx.IsKind(err, errorx.KindPermissionDenied) {
		// The only permission refusal a claim answers is the fence.
		slog.ErrorContext(ctx, "protected work stopped", slog.String("error", err.Error()))
		return
	}
	slog.WarnContext(ctx, "message not claimed", slog.String("error", err.Error()))
	s.wait(ctx, s.cfg.RetryDelay)
}

// post hands a claimed message to the runtime as garam-message.v1
// under garam's identifier for it, with garam's signature where the
// adapter forwards one. A signature goes only beside the content garam
// signed, never beside one the adapter encoded.
func (s *service) post(ctx context.Context, claim *Claim) (pairing, error) {
	content, err := claim.Content()
	if err != nil {
		return pairing{}, err
	}
	signature := ""
	if s.cfg.ForwardSignature && claim.Encoded != "" {
		signature = claim.Signature
	}
	postCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	progress, err := s.ends.Gateway.Post(postCtx, claim.Message, content, signature)
	cancel()
	if err != nil {
		return pairing{}, err
	}
	return pairing{claim: claim, renewAt: s.renewAt(claim), done: progress == ProgressCompleted}, nil
}

// readTurns holds one subscription to the platform's events and reports
// each turn that ends on it, reopening a stream that drops, closes
// subscribed once the first one is open, and signals reopened on every
// one after it. The subscription is this loop's own, because a reader
// that falls behind loses events silently (ADR-0013, ADR-0018). The
// stream carries no cursor, so the turns that ended while it was down
// are recovered by asking the runtime, not by the stream.
func (s *service) readTurns(ctx context.Context, ended chan<- TurnEnd, subscribed, reopened chan<- struct{}) {
	opened := false
	for ctx.Err() == nil {
		stream, err := s.ends.Gateway.Subscribe(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "turn events not subscribed", slog.String("error", err.Error()))
			s.wait(ctx, s.cfg.RetryDelay)
			continue
		}
		if !opened {
			close(subscribed)
			opened = true
		} else {
			select {
			case reopened <- struct{}{}:
			default:
			}
		}
		err = s.readStream(ctx, stream, ended)
		if ctx.Err() != nil {
			return
		}
		slog.WarnContext(ctx, "turn events resubscribing", slog.String("error", err.Error()))
		s.wait(ctx, s.cfg.RetryDelay)
	}
}

// readStream reports every turn one stream ends, and returns what ended
// it. A stream ends by dropping, by the agent stopping, or by ctx; the
// caller tells those apart and this does not.
func (s *service) readStream(ctx context.Context, stream TurnStream, ended chan<- TurnEnd) error {
	defer func() { _ = stream.Close() }()
	for {
		turn, err := stream.Next()
		if err != nil {
			return err
		}
		select {
		case ended <- turn:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// complete reports the messages a turn finished with, and forgets every
// message it named either way.
//
// A turn that failed is an attempt rather than a completion: its claim
// is left to lapse and garam hands the message out again, up to the
// attempt count it bounds a message by. The adapter never has to tell a
// turn that failed on the message from one that failed on a model
// provider it could not reach, because success completes and everything
// else retries.
func (s *service) complete(ctx context.Context, held map[MessageID]pairing, turn TurnEnd) {
	for _, id := range turn.Messages {
		p, ok := held[id]
		if !ok {
			// A message this adapter is not holding: one a previous
			// process posted before it restarted, or one whose lease
			// has already lapsed.
			continue
		}
		delete(held, id)
		if !turn.Completes() {
			slog.InfoContext(ctx, "turn failed",
				slog.String("message", string(id)),
				slog.String("claim", string(p.claim.ID)))
			continue
		}
		s.completeClaim(ctx, p.claim)
	}
}

// reconcile asks the runtime about every message held once the stream
// has been reopened, and completes those whose turn committed while no
// event could reach this loop.
func (s *service) reconcile(ctx context.Context, held map[MessageID]pairing) {
	for id, p := range held {
		callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
		progress, err := s.ends.Gateway.Lookup(callCtx, id)
		cancel()
		if err != nil {
			slog.WarnContext(ctx, "message not looked up",
				slog.String("message", string(id)),
				slog.String("error", err.Error()))
			continue
		}
		if progress == ProgressCompleted {
			delete(held, id)
			s.completeClaim(ctx, p.claim)
		}
	}
}

// completeClaim reports one claim's message delivered. A completion
// that does not arrive is a redelivery, which the runtime answers as
// completed, so a failure here is logged and nothing more.
func (s *service) completeClaim(ctx context.Context, claim *Claim) {
	callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
	err := s.ends.Queue.Complete(callCtx, claim.ID)
	cancel()
	if err != nil {
		slog.WarnContext(ctx, "claim not completed",
			slog.String("message", string(claim.Message)),
			slog.String("claim", string(claim.ID)),
			slog.String("error", err.Error()))
	}
}

// renew carries every claim that is due forward. A refusal is garam
// saying the hold is gone — the lease lapsed, or the ceiling it was
// held at ran out — or that what this adapter presents is no longer
// admitted; either way the pairing is dropped and the message arrives
// again. The ceiling is garam's to enforce and this loop has none of
// its own: it renews while a turn is in flight and stops when it is
// refused.
func (s *service) renew(ctx context.Context, held map[MessageID]pairing) {
	now := time.Now()
	for id, p := range held {
		if p.renewAt.After(now) {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
		renewed, err := s.ends.Queue.Renew(callCtx, p.claim.ID)
		cancel()
		// The hold being gone and the fence are the only failed
		// precondition and permission refusal garam reports here, and
		// the kind is what the chain carries (stack-go.md §4).
		if errorx.IsKind(err, errorx.KindFailedPrecondition) || errorx.IsKind(err, errorx.KindPermissionDenied) {
			slog.WarnContext(ctx, "claim lapsed",
				slog.String("message", string(id)),
				slog.String("claim", string(p.claim.ID)),
				slog.String("error", err.Error()))
			delete(held, id)
			continue
		}
		if err != nil {
			// The hold may well still stand, so it is kept and the call
			// is made again rather than the message given up on.
			slog.WarnContext(ctx, "claim not renewed",
				slog.String("claim", string(p.claim.ID)),
				slog.String("error", err.Error()))
			p.renewAt = now.Add(s.cfg.RetryDelay)
			held[id] = p
			continue
		}
		p.claim, p.renewAt = renewed, s.renewAt(renewed)
		held[id] = p
	}
}

// renewalTimer fires when the earliest claim held is due, and never
// when none is held. The stop it returns is safe to call either way.
func (s *service) renewalTimer(held map[MessageID]pairing) (<-chan time.Time, func()) {
	var earliest time.Time
	for _, p := range held {
		if earliest.IsZero() || p.renewAt.Before(earliest) {
			earliest = p.renewAt
		}
	}
	if earliest.IsZero() {
		return nil, func() {}
	}
	timer := time.NewTimer(max(time.Until(earliest), 0))
	return timer.C, func() { timer.Stop() }
}

// renewAt is when a claim is next carried forward: a third of what is
// left on it, and never sooner than the configured floor.
func (s *service) renewAt(c *Claim) time.Time {
	return time.Now().Add(max(time.Until(c.ExpiresAt)/renewDivisor, s.cfg.MinRenewInterval))
}

// wait holds for d, or returns at once when ctx is done.
func (s *service) wait(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// backoff doubles a retry delay, from the configured one up to its cap.
func (s *service) backoff(d time.Duration) time.Duration {
	return min(max(2*d, s.cfg.RetryDelay), s.cfg.MaxRetryDelay)
}
