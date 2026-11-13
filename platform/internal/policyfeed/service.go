package policyfeed

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Service delivers the feed.
type Service interface {
	// Run delivers until ctx ends, and returns nil when it does. A pass
	// that fails is retried after a delay that doubles to a cap, and
	// no failure ends Run.
	Run(ctx context.Context) error
	// Wake ends the wait between passes, so a pass runs now. It never
	// blocks, and a wake while a pass runs is kept for the next wait.
	Wake()
	// LastPass is when a pass last ended without a failure, and the
	// zero time before the first. A pass that found another replica
	// holding the feed ends without one, so it counts.
	LastPass() time.Time
}

// Config sets the loop's pace.
type Config struct {
	// Poll is how long Run waits after a pass that read fewer marks
	// than a batch holds.
	Poll time.Duration
	// MaxBackoff caps the delay after failed passes.
	MaxBackoff time.Duration
}

type service struct {
	store  Store
	sender Sender
	logger *slog.Logger
	config Config
	wake   chan struct{}
	// lastPass is the Unix nanoseconds of the last pass that did not
	// fail, and zero before the first.
	lastPass atomic.Int64
}

// NewService constructs the delivery loop.
func NewService(store Store, sender Sender, logger *slog.Logger, config Config) Service {
	return &service{store: store, sender: sender, logger: logger, config: config,
		wake: make(chan struct{}, 1)}
}

func (s *service) LastPass() time.Time {
	ns := s.lastPass.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (s *service) Run(ctx context.Context) error {
	var failures int
	for {
		result, err := s.store.Deliver(ctx, BatchLimit, s.sender)
		if err != nil && ctx.Err() != nil {
			// A pass cut off by shutdown is not a failure to report. One
			// that committed is reported below even then.
			return nil
		}
		wait := s.config.Poll
		switch {
		case err != nil:
			// One line when delivery starts failing and one when it
			// stops, so an outage does not log on every retry.
			if failures == 0 {
				s.logger.Error("policy feed delivery failing", slog.String("error", err.Error()))
			}
			failures++
			wait = s.backoff(failures)
		default:
			if failures > 0 {
				s.logger.Info("policy feed delivery recovered", slog.Int("failed_passes", failures))
				failures = 0
			}
			s.lastPass.Store(time.Now().UnixNano())
			for _, r := range result.SetAside {
				// Each pass sets a link aside once, so this line is
				// not repeated by the retries a failure would be.
				s.logger.Error("policy service refused a role link for good; it is set aside",
					slog.String("org", r.Link.Org),
					slog.String("user", r.Link.User),
					slog.String("code", r.Code),
					slog.String("reason", r.Reason))
			}
			for _, r := range result.Refused {
				s.logger.Error("policy service refused an entity; its delete was sent instead",
					slog.String("entity_type", r.Entity.Type),
					slog.String("entity_id", r.Entity.ID),
					slog.String("reason", r.Reason))
			}
			if result.Marks >= BatchLimit {
				wait = 0
			}
		}
		if !s.sleep(ctx, wait) {
			return nil
		}
	}
}

// backoff is Poll doubled once for each failure after the first,
// capped at MaxBackoff.
func (s *service) backoff(failures int) time.Duration {
	wait := s.config.Poll
	for i := 1; i < failures && wait < s.config.MaxBackoff; i++ {
		wait *= 2
	}
	return min(wait, s.config.MaxBackoff)
}

func (s *service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// sleep waits d, or until Wake, and reports whether ctx is still live.
func (s *service) sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-s.wake:
		return true
	case <-timer.C:
		return true
	}
}
