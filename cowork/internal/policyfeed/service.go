package policyfeed

import (
	"context"
	"log/slog"
	"time"
)

// Service delivers the feed.
type Service interface {
	// Run delivers until ctx ends, and returns nil when it does. A pass
	// that fails is retried after a delay that doubles to a cap, and
	// no failure ends Run.
	Run(ctx context.Context) error
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
}

// NewService constructs the delivery loop.
func NewService(store Store, sender Sender, logger *slog.Logger, config Config) Service {
	return &service{store: store, sender: sender, logger: logger, config: config}
}

func (s *service) Run(ctx context.Context) error {
	var failures int
	for {
		result, err := s.store.Deliver(ctx, BatchLimit, s.sender)
		if err != nil && ctx.Err() != nil {
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
			for _, r := range result.Refused {
				s.logger.Error("policy service refused an entity; its delete was sent instead",
					slog.String("entity_type", r.Entity.Type),
					slog.String("org", r.Entity.Org),
					slog.String("entity_id", r.Entity.ID),
					slog.String("reason", r.Reason))
			}
			if result.Marks >= BatchLimit {
				wait = 0
			}
		}
		if !sleep(ctx, wait) {
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

// sleep waits d and reports whether ctx is still live.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
