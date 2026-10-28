package workload

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/garamsh/garam/internal/errorx"
)

// attachDir holds an attaching supervisor's serving material, in its
// own directory. One root outlasts every intent: the adapter is a host
// process that reads its root file once, when it starts, and it
// outlives the intents of a supervisor its init restarts.
const attachDir = "attach"

// placementTokenFile is the placement token in the adapter's directory.
const placementTokenFile = "placement-token"

// statusTimeout bounds one read of an attached runtime's status.
const statusTimeout = 5 * time.Second

// defaultStatusInterval is how often an attached runtime's status is
// read again, to notice that it reports another generation.
const defaultStatusInterval = 5 * time.Second

// runAttached supervises a runtime the host runs itself (ADR-0111). It
// starts, signals and drains nothing. It resumes the intent the record
// holds while that intent is still what is desired, and opens the next
// one otherwise. It then serves the adapter's control until the
// runtime reports a generation other than the one forwarded, and
// returns ErrGenerationEnded.
func (s *Supervisor) runAttached(ctx context.Context) error {
	in := s.st.Intent
	if in != nil && in.Phase != phaseEnded {
		if !in.Attached {
			return ErrStartedHere
		}
		d := s.cfg.Desired
		if !in.live() || d.Replace || in.Epoch != d.Epoch || in.Revision != d.Revision {
			if err := s.locked(func() { in.Phase = phaseEnded }); err != nil {
				return err
			}
			in = nil
		}
	} else {
		in = nil
	}
	if in == nil {
		var err error
		if in, err = s.newIntent(); err != nil {
			return err
		}
	}
	return s.superviseAttached(ctx, in)
}

// superviseAttached writes what the adapter reads into its directory,
// serves its control on a socket there, and watches the runtime.
func (s *Supervisor) superviseAttached(ctx context.Context, in *intent) error {
	a := s.cfg.Attach
	serving, root, err := servingMaterial(filepath.Join(s.cfg.Dir, attachDir))
	if err != nil {
		return err
	}
	token, err := s.agentToken(ctx, RoleAdapter)
	if err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{
		{controlRootPEM, root},
		{placementTokenFile, []byte(in.PlacementToken)},
		{controlTokenFile, []byte(in.ControlToken)},
		{agentTokenFile, []byte(token)},
	} {
		if err := writeDurably(filepath.Join(a.AdapterDir, f.name), f.data, 0o600); err != nil {
			return err
		}
	}
	ctl, err := s.serveControl(filepath.Join(a.AdapterDir, socketDir), serving, in)
	if err != nil {
		return err
	}
	defer ctl.close()
	slog.Info("runtime attached", slog.String("intent", in.ID))
	return s.watchAttached(ctx, ctl, in)
}

// watchAttached waits for the runtime to report a generation other
// than the one the intent forwarded, or for the control to stop
// serving. A status it cannot read decides nothing: the same
// generation read again resumes, and only another one ends the intent
// (ADR-0111). ctx ending leaves the runtime as it is.
func (s *Supervisor) watchAttached(ctx context.Context, ctl *controlServer, in *intent) error {
	a := s.cfg.Attach
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.refresh(waitCtx, func(token string) error {
		return writeDurably(filepath.Join(a.AdapterDir, agentTokenFile), []byte(token), 0o600)
	})
	interval := a.StatusInterval
	if interval <= 0 {
		interval = defaultStatusInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-ctl.failed:
			slog.Error("control stopped serving; ending the attachment",
				slog.String("intent", in.ID), slog.String("error", err.Error()))
			return s.endAttached(ctx, ctl, in)
		case <-ticker.C:
		}
		s.mu.Lock()
		claimed := in.Generation
		s.mu.Unlock()
		if claimed == "" {
			continue
		}
		status, err := s.readStatus(ctx)
		if err == nil && status.Generation == claimed ||
			err != nil && !errorx.IsKind(err, errorx.KindFailedPrecondition) {
			continue
		}
		slog.Warn("the attached runtime no longer reports the generation forwarded",
			slog.String("intent", in.ID), slog.String("generation", claimed))
		return s.endAttached(ctx, ctl, in)
	}
}

// endAttached ends an intent whose runtime is gone: its control stops
// taking requests, those in flight finish, and the record says it
// ended. The runtime itself is the host's.
func (s *Supervisor) endAttached(ctx context.Context, ctl *controlServer, in *intent) error {
	if err := ctl.srv.Shutdown(ctx); err != nil {
		return errorx.Wrap(err, "workload: control", errorx.KindUnavailable)
	}
	if err := s.locked(func() { in.Phase = phaseEnded }); err != nil {
		return err
	}
	slog.Warn("attached generation ended", slog.String("intent", in.ID))
	return ErrGenerationEnded
}

// attached refuses a runtime that does not report, at the attached
// address, the generation the request names serving under the
// intent's epoch and revision. It is provenance for a runtime the
// supervisor did not start: Sherlock mints a generation only once it
// holds its store's lock, so a fresh one serving holds the store
// (ADR-0111). A status it could not read is KindUnavailable; any other
// mismatch is KindFailedPrecondition.
func (s *Supervisor) attached(ctx context.Context, in *intent, generation string) error {
	status, err := s.readStatus(ctx)
	if errorx.IsKind(err, errorx.KindFailedPrecondition) {
		return errorx.Wrap(err, "workload: the attached runtime", errorx.KindFailedPrecondition)
	}
	if err != nil {
		return errorx.Wrap(err, "workload: the attached runtime", errorx.KindUnavailable)
	}
	if status.State != "serving" || status.Generation != generation ||
		status.Epoch != in.Epoch || status.Revision != in.Revision {
		return errorx.New(errorx.KindFailedPrecondition,
			"workload: the attached runtime does not report this generation serving at this epoch and revision")
	}
	return nil
}

// readStatus makes one bounded read of the attached runtime's status.
func (s *Supervisor) readStatus(ctx context.Context) (*RuntimeStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	return s.cfg.Attach.Status.Status(ctx)
}
