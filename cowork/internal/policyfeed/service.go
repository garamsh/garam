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
	// failures counts passes that failed whole, orgFailures passes in
	// which an organization's run failed.
	var failures, orgFailures int
	failing := map[string]bool{}
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
			s.reportOrgs(result, failing)
			for _, r := range result.Refused {
				s.logger.Error("policy service refused an entity; its delete was sent instead",
					slog.String("entity_type", r.Entity.Type),
					slog.String("org", r.Entity.Org),
					slog.String("entity_id", r.Entity.ID),
					slog.String("reason", r.Reason))
			}
			switch {
			case len(result.Failed) > 0:
				// The others' runs went in this pass; the failing
				// ones are retried at a pace that spares the service.
				orgFailures++
				wait = s.backoff(orgFailures)
			case result.Marks >= BatchLimit:
				orgFailures = 0
				wait = 0
			default:
				orgFailures = 0
			}
		}
		if !sleep(ctx, wait) {
			return nil
		}
	}
}

// reportOrgs logs an organization once as it starts failing and once
// as its run is delivered again, so an organization the platform no
// longer knows does not log on every retry.
func (s *service) reportOrgs(result Result, failing map[string]bool) {
	for _, f := range result.Failed {
		if !failing[f.Org] {
			failing[f.Org] = true
			s.logger.Error("policy feed delivery failing for an organization",
				slog.String("org", f.Org), slog.String("error", f.Err.Error()))
		}
	}
	for _, org := range result.Delivered {
		if failing[org] {
			delete(failing, org)
			s.logger.Info("policy feed delivery recovered for an organization", slog.String("org", org))
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
