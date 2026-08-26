package delivery

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/garamsh/garam/internal/errorx"
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
}

// Queue is garam's side of the loop: the claim this adapter holds for
// its agent, the renewal that keeps it, and the completion that ends
// it. Every call is for the one agent the adapter runs beside, which is
// why none of them names it.
type Queue interface {
	// Claim waits for a message addressed to the agent and returns the
	// hold garam gave it, or ErrNoMessageReady when the wait ran out.
	Claim(ctx context.Context) (*Claim, error)
	// Renew carries the hold forward and answers with it as it now
	// stands, or ErrClaimLapsed when it is gone.
	Renew(ctx context.Context, id ClaimID) (*Claim, error)
	// Complete reports the claimed message delivered, or answers
	// ErrClaimLapsed when the hold was handed on.
	Complete(ctx context.Context, id ClaimID) error
}

// Gateway is the agent platform's side: the ingress that takes a
// message and the event stream a turn's end arrives on.
type Gateway interface {
	// Post hands one message to the agent and returns the identifier the
	// gateway minted for it. A caller cannot supply one, so this answer
	// is the only thing a turn's event can be paired back to.
	Post(ctx context.Context, body string) (GatewayID, error)
	// Subscribe opens one stream of the agent's events, which lasts
	// until it is closed, it drops, or ctx is done.
	Subscribe(ctx context.Context) (TurnStream, error)
}

// TurnStream is one open subscription, read to its end by one reader.
type TurnStream interface {
	// Next blocks until the agent's next turn ends, and answers an error
	// once the stream carries nothing more.
	Next() (TurnEnd, error)
	// Close ends the subscription.
	Close() error
}

// Service is the delivery loop of docs/architecture/adapter.md: claim a
// message with its lease, post it into the gateway, hold the pairing of
// the gateway's identifier to garam's, watch for a turn ending, and
// complete that message's claim when a turn reports success and names
// it.
type Service interface {
	// Run carries messages until ctx is done, and answers nil then,
	// because a requested stop is not a failure. Nothing else ends it:
	// a garam or a gateway that is not answering is waited on and tried
	// again, since a sidecar that exited would take its agent's only
	// route to the queue with it.
	Run(ctx context.Context) error
}

type service struct {
	queue   Queue
	gateway Gateway
	cfg     Config
}

// NewService constructs the loop over the two ends it carries between,
// or refuses a configuration that would renew nothing.
func NewService(queue Queue, gateway Gateway, cfg Config) (Service, error) {
	cfg = withDefaults(cfg)
	switch {
	case queue == nil || gateway == nil:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: both ends are required")
	case cfg.RetryDelay <= 0:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: retry delay must be positive")
	case cfg.MinRenewInterval <= 0:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: minimum renew interval must be positive")
	case cfg.ClaimTimeout <= 0 || cfg.CallTimeout <= 0:
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: call bounds must be positive")
	}
	return &service{queue: queue, gateway: gateway, cfg: cfg}, nil
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
	return cfg
}

// pairing is one message in flight: what the gateway called it, the
// hold garam gave it, and when that hold is next carried forward.
type pairing struct {
	gateway GatewayID
	claim   *Claim
	renewAt time.Time
}

// Run starts the two readers and holds the pairings between them.
//
// The subscription is opened before the first message is posted, and
// nothing is claimed until it is: a turn that ends before anyone is
// reading completes nothing, and its message would come back only once
// the lease had lapsed.
func (s *service) Run(ctx context.Context) error {
	posted := make(chan pairing)
	ended := make(chan TurnEnd)
	subscribed := make(chan struct{})

	go s.readTurns(ctx, ended, subscribed)
	select {
	case <-subscribed:
	case <-ctx.Done():
		return nil
	}
	go s.claim(ctx, posted)
	s.hold(ctx, posted, ended)
	return nil
}

// hold owns every pairing this adapter is carrying. One goroutine holds
// the map, so what is in flight needs no lock and no second reader.
//
// The map is the whole of the adapter's memory of what it has delivered
// and it reaches no disk: a restart lapses the leases in it rather than
// losing the messages, and reconciling what was in it is garam's
// (ADR-0018).
func (s *service) hold(ctx context.Context, posted <-chan pairing, ended <-chan TurnEnd) {
	held := map[GatewayID]pairing{}
	for {
		due, stop := s.renewalTimer(held)
		select {
		case <-ctx.Done():
			stop()
			return
		case p := <-posted:
			held[p.gateway] = p
		case e := <-ended:
			s.complete(ctx, held, e)
		case <-due:
			s.renew(ctx, held)
		}
		stop()
	}
}

// claim holds one long poll on garam and posts what it answers with.
// The pairing travels to hold rather than into the map directly, so one
// goroutine owns everything in flight.
//
// A turn that ends before its pairing arrives completes nothing, and
// the message is redelivered. That is the at-least-once behaviour
// ADR-0018 states rather than a case to synchronize away: the platform
// runs the turn after it accepts the message, so the pairing is a
// channel send ahead of an event a turn's length behind it.
func (s *service) claim(ctx context.Context, posted chan<- pairing) {
	for ctx.Err() == nil {
		claimCtx, cancel := context.WithTimeout(ctx, s.cfg.ClaimTimeout)
		claim, err := s.queue.Claim(claimCtx)
		cancel()
		if errors.Is(err, ErrNoMessageReady) {
			// The poll waited its time at garam, so asking again at once
			// is the loop rather than a spin.
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "message not claimed", slog.String("error", err.Error()))
			s.wait(ctx)
			continue
		}
		postCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
		id, err := s.gateway.Post(postCtx, claim.Body)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			// Nothing was accepted, so there is nothing to pair: the
			// lease lapses and garam hands the message out again.
			slog.WarnContext(ctx, "message not posted",
				slog.String("claim", string(claim.ID)),
				slog.String("error", err.Error()))
			s.wait(ctx)
			continue
		}
		select {
		case posted <- pairing{gateway: id, claim: claim, renewAt: s.renewAt(claim)}:
		case <-ctx.Done():
			return
		}
	}
}

// readTurns holds one subscription to the platform's events and reports
// each turn that ends on it, reopening a stream that drops. It closes
// subscribed once the first one is open.
//
// The subscription is this loop's own. Consuming, completing and
// forwarding each take one, because a reader that falls behind loses
// events silently, and the one that decides what is completed must not
// be degraded by another (ADR-0013, ADR-0018).
//
// Nothing is recovered across a reopen: the stream carries no cursor,
// so the turns that ended while it was down complete nothing and their
// messages arrive again.
func (s *service) readTurns(ctx context.Context, ended chan<- TurnEnd, subscribed chan<- struct{}) {
	opened := false
	for ctx.Err() == nil {
		stream, err := s.gateway.Subscribe(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "turn events not subscribed", slog.String("error", err.Error()))
			s.wait(ctx)
			continue
		}
		if !opened {
			close(subscribed)
			opened = true
		}
		err = s.readStream(ctx, stream, ended)
		if ctx.Err() != nil {
			return
		}
		slog.WarnContext(ctx, "turn events resubscribing", slog.String("error", err.Error()))
		s.wait(ctx)
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
func (s *service) complete(ctx context.Context, held map[GatewayID]pairing, turn TurnEnd) {
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
				slog.String("claim", string(p.claim.ID)),
				slog.String("gateway_message", string(id)))
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
		err := s.queue.Complete(callCtx, p.claim.ID)
		cancel()
		if err != nil {
			// A completion that does not arrive is a redelivery, which
			// is what every other lost completion here means too.
			slog.WarnContext(ctx, "claim not completed",
				slog.String("claim", string(p.claim.ID)),
				slog.String("error", err.Error()))
		}
	}
}

// renew carries every claim that is due forward. A refusal is garam
// saying the hold is gone — the lease lapsed, or the ceiling it was
// held at ran out — so the pairing is dropped and the message arrives
// again. The ceiling is garam's to enforce and this loop has none of
// its own: it renews while a turn is in flight and stops when it is
// refused.
func (s *service) renew(ctx context.Context, held map[GatewayID]pairing) {
	now := time.Now()
	for id, p := range held {
		if p.renewAt.After(now) {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, s.cfg.CallTimeout)
		renewed, err := s.queue.Renew(callCtx, p.claim.ID)
		cancel()
		if errors.Is(err, ErrClaimLapsed) {
			slog.WarnContext(ctx, "claim lapsed",
				slog.String("claim", string(p.claim.ID)),
				slog.String("gateway_message", string(id)))
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
func (s *service) renewalTimer(held map[GatewayID]pairing) (<-chan time.Time, func()) {
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

// wait holds for the retry delay, or returns at once when ctx is done.
func (s *service) wait(ctx context.Context) {
	timer := time.NewTimer(s.cfg.RetryDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}
