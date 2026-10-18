package workload

import (
	"context"
	"log/slog"
	"time"
)

// The token mode's files in the adapter's directory: the agent token
// garam accepts, and the generation's control token (ADR-0100).
const (
	agentTokenFile   = "agent-token"
	controlTokenFile = "control-token"
)

// defaultTokenCheckInterval is how often a running adapter's agent
// token is compared with the one the supervisor holds. It is a small
// part of the quarter of a token's life a refresh leaves for it to
// reach the adapter: five minutes at the shortest lifetime garam
// allows.
const defaultTokenCheckInterval = 30 * time.Second

// agentToken answers the agent token an adapter starts with, in the
// token mode, and nothing for any other role or mode.
func (s *Supervisor) agentToken(ctx context.Context, r Role) (string, error) {
	if r != RoleAdapter || s.cfg.Tokens == nil {
		return "", nil
	}
	token, err := s.cfg.Tokens.Current(ctx)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.handed = token
	s.mu.Unlock()
	return token, nil
}

// refresh hands the running adapter the agent token, through hand,
// whenever the one the supervisor holds is not the one it handed, until
// ctx ends. A failure is retried on the next look, and refusing to
// refresh never stops the generation; garam refusing the token the
// adapter holds is what does.
func (s *Supervisor) refresh(ctx context.Context, hand func(token string) error) {
	if s.cfg.Tokens == nil {
		return
	}
	interval := s.cfg.TokenCheckInterval
	if interval <= 0 {
		interval = defaultTokenCheckInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		token, err := s.cfg.Tokens.Current(ctx)
		if err != nil {
			slog.Warn("agent token not refreshed", slog.String("error", err.Error()))
			continue
		}
		s.mu.Lock()
		handed := s.handed
		s.mu.Unlock()
		if token == handed {
			continue
		}
		if err := hand(token); err != nil {
			slog.Warn("agent token not handed to the adapter", slog.String("error", err.Error()))
			continue
		}
		s.mu.Lock()
		s.handed = token
		s.mu.Unlock()
		slog.Info("agent token handed to the adapter")
	}
}
