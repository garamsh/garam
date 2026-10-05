package workload

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/garamsh/garam/internal/errorx"
)

// drain stops the intent's generation with no deadline: SIGTERM to the
// runtime and the workspace, a wait until both have exited, then the
// same for the adapter, and only then are the three removed. A
// runtime still finishing a turn keeps the drain pending for as long
// as that takes; nothing here escalates.
func (s *Supervisor) drain(ctx context.Context, in *intent) error {
	if in.Phase != phaseDraining {
		if err := s.locked(func() { in.Phase = phaseDraining }); err != nil {
			return err
		}
	}
	ids := map[Role]string{}
	for _, r := range roles {
		c, err := s.adopt(ctx, in, r)
		if errors.Is(err, ErrForeign) {
			slog.Warn("a container under this generation's name is not its own; left untouched",
				slog.String("name", in.container(r).Name))
			continue
		}
		if err != nil {
			return err
		}
		if c != nil {
			ids[r] = c.ID
		}
	}
	slog.Info("draining generation", slog.String("intent", in.ID))
	for _, stage := range [][]Role{{RoleRuntime, RoleWorkspace}, {RoleAdapter}} {
		for _, r := range stage {
			if id := ids[r]; id != "" {
				if err := s.engine.SignalContainer(ctx, id, "SIGTERM"); err != nil {
					return err
				}
			}
		}
		for _, r := range stage {
			if id := ids[r]; id != "" {
				if err := s.engine.WaitExited(ctx, id); err != nil && !errors.Is(err, ErrAbsent) {
					return err
				}
			}
		}
	}
	for _, r := range roles {
		if id := ids[r]; id != "" {
			if err := s.engine.RemoveContainer(ctx, id); err != nil && !errors.Is(err, ErrAbsent) {
				return err
			}
		}
	}
	if err := os.RemoveAll(generationDir(s.cfg.Dir, in.ID)); err != nil {
		return errorx.Wrap(err, "workload: generation directory", errorx.KindUnavailable)
	}
	return s.locked(func() { in.Phase = phaseEnded })
}
