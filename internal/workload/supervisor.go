package workload

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"log/slog"
	"regexp"
	"sync"

	"github.com/garamsh/garam/internal/errorx"
)

// Paths inside the containers. The runtime's and the workspace's are
// what Sherlock's own fixture mounts (deploy/runtime-fixture at
// sherlock v0.2.0).
const (
	memoryTarget    = "/data"
	outboxDir       = "outbox"
	outboxTarget    = "/outbox"
	workspaceTarget = "/var/lib/sherlock-workspace"
	runtimeConfig   = "/etc/sherlock/config.yaml"
	runtimeEgo      = "/etc/sherlock/ego.md"
	runtimeInstr    = "/etc/sherlock/instructions.md"
	adapterFiles    = "/etc/garam"
	socketTarget    = "/run/garam-control"
)

// runtimeUser is the runtime's account, which owns the memory volume
// and every outbox entry; the adapter runs as it too, which is what
// lets it read and remove an entry the runtime wrote 0600.
const (
	runtimeUID  = 65532
	runtimeUser = "65532:65532"
)

// workspaceCaps is what the workspace's executor needs to drop each
// child to its own account and namespace (Sherlock's
// deploy/local/docker-compose.yml at v0.2.0 records why each).
var workspaceCaps = []string{"SETUID", "SETGID", "CHOWN", "SYS_ADMIN", "KILL"}

// immutableImage is a reference that cannot come to mean other
// content: a repository digest, or an image ID, which is the digest of
// the image's configuration.
var immutableImage = regexp.MustCompile(`^([^@\s]+@)?sha256:[0-9a-f]{64}$`)

// volumeName is a name the engine accepts for a volume.
var volumeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]+$`)

// errOver is a generation that can no longer run: a container of it
// exited, was removed, or restarted. It is drained, never resumed.
var errOver = errorx.New(errorx.KindAborted, "workload: the generation is over")

// Config is what one supervisor runs.
type Config struct {
	// Dir is the supervisor's own directory, which holds the record
	// and each generation's serving material.
	Dir string
	// Prefix begins every container and volume name the supervisor
	// derives.
	Prefix  string
	Desired Desired
	Adapter AdapterFiles
	// MessageIssuer is garam's message-signing issuer, which the runtime
	// trusts by garam's key set (ADR-0101). The caller holds it to
	// garam's one spelling. Empty renders no issuers entry: the runtime
	// is handed the host's configuration as it stands.
	MessageIssuer string
}

// Supervisor runs one agent's workload on a standalone host.
type Supervisor struct {
	engine Engine
	garam  Activator
	cfg    Config
	issuer *x509.CertPool
	// renderedConfig is the host's runtime configuration with garam's
	// issuers entry added, which is what the runtime is handed.
	renderedConfig []byte

	// mu guards st and its writes, which the control handlers share
	// with the run.
	mu sync.Mutex
	st *state
	// activating holds one activation attempt at a time (ADR-0084).
	activating sync.Mutex
}

// NewSupervisor constructs a supervisor over an engine and garam.
func NewSupervisor(engine Engine, garam Activator, cfg Config) (*Supervisor, error) {
	if engine == nil || garam == nil {
		return nil, errorx.New(errorx.KindInvalidArgument, "workload: an engine and garam are required")
	}
	if cfg.Dir == "" || cfg.Prefix == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "workload: a directory and a name prefix are required")
	}
	d := cfg.Desired
	if err := d.Agent.Validate(); err != nil {
		return nil, errorx.Wrap(err, "workload: agent", errorx.KindInvalidArgument)
	}
	if d.Epoch == "" || d.Revision == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "workload: an epoch and a non-empty revision are required")
	}
	for _, image := range []string{d.Images.Runtime, d.Images.Workspace, d.Images.Adapter} {
		if !immutableImage.MatchString(image) {
			return nil, errorx.New(errorx.KindInvalidArgument, "workload: every image is named by its digest or its ID: "+image)
		}
	}
	if d.MoveMemoryTo != "" && !volumeName.MatchString(d.MoveMemoryTo) {
		return nil, errorx.New(errorx.KindInvalidArgument, "workload: not a volume name: "+d.MoveMemoryTo)
	}
	issuer := x509.NewCertPool()
	if !issuer.AppendCertsFromPEM(cfg.Adapter.IssuerPEM) {
		return nil, errorx.New(errorx.KindInvalidArgument, "workload: the agent's issuer holds no certificate")
	}
	renderedConfig := d.Config
	if cfg.MessageIssuer != "" {
		if cfg.Adapter.MachineURL == "" {
			return nil, errorx.New(errorx.KindInvalidArgument, "workload: garam's message issuer needs the adapter's address for garam")
		}
		var err error
		renderedConfig, err = withIssuer(d.Config, cfg.MessageIssuer, cfg.Adapter.MachineURL+keySetPath, runtimeServerRoot)
		if err != nil {
			return nil, err
		}
	}
	return &Supervisor{engine: engine, garam: garam, cfg: cfg, issuer: issuer, renderedConfig: renderedConfig}, nil
}

// Run brings the desired generation up, or adopts the one running, and
// supervises it. It returns nil when ctx ends, leaving the workload
// running; ErrGenerationEnded once the generation exited and the rest
// of it was drained; and any other error with the workload left as it
// stood. It never forces a container to stop.
func (s *Supervisor) Run(ctx context.Context) error {
	err := s.run(ctx)
	if ctx.Err() != nil {
		// A requested stop is not a failure, wherever it lands: the
		// record is written before every step, so the next run takes
		// up what this one left.
		return nil
	}
	return err
}

func (s *Supervisor) run(ctx context.Context) error {
	st, err := loadState(s.cfg.Dir)
	if err != nil {
		return err
	}
	s.st = st
	if err := s.storage(ctx); err != nil {
		return err
	}
	owed, err := s.moveOwed()
	if err != nil {
		return err
	}
	in, err := s.recover(ctx, owed)
	if err != nil {
		return err
	}
	if in == nil {
		if err := s.moveEnded(ctx); err != nil {
			return err
		}
		if owed {
			if err := s.move(ctx); err != nil {
				return err
			}
		}
		if in, err = s.newIntent(); err != nil {
			return err
		}
	}
	err = s.supervise(ctx, in)
	if errors.Is(err, ErrGenerationEnded) {
		if refused := s.moveEnded(ctx); refused != nil {
			return refused
		}
	}
	return err
}

// save writes the record. The caller holds mu.
func (s *Supervisor) save() error {
	return s.st.save(s.cfg.Dir)
}

// locked runs f holding mu and saves the record after it.
func (s *Supervisor) locked(f func()) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	f()
	return s.save()
}

// newIntent records the next generation before anything of it exists.
func (s *Supervisor) newIntent() (*intent, error) {
	token := make([]byte, 32)
	_, _ = rand.Read(token)
	d := s.cfg.Desired
	id := randomHex(8)
	in := &intent{
		ID: id, Nonce: randomHex(16), Phase: phaseStarting,
		Epoch: d.Epoch, Revision: d.Revision, Images: d.Images,
		RequireStore:   s.st.Memory.Served,
		PlacementToken: base64.RawURLEncoding.EncodeToString(token),
	}
	for _, r := range roles {
		in.container(r).Name = s.cfg.Prefix + string(r) + "-" + id
	}
	if err := s.locked(func() {
		s.st.Intent = in
		if mv := s.st.Move; mv.awaiting() && mv.Intent == "" {
			mv.Intent = in.ID
		}
	}); err != nil {
		return nil, err
	}
	slog.Info("generation intended", slog.String("intent", in.ID), slog.Bool("require_store", in.RequireStore))
	return in, nil
}

// supervise brings the intent up, serves its adapter, and waits for it
// to exit.
func (s *Supervisor) supervise(ctx context.Context, in *intent) error {
	genDir := generationDir(s.cfg.Dir, in.ID)
	serving, root, err := servingMaterial(genDir)
	if err != nil {
		return err
	}
	ctl, err := s.serveControl(genDir, serving, in)
	if err != nil {
		return err
	}
	defer func() { _ = ctl.srv.Close() }()
	for _, r := range roles {
		err := s.ensure(ctx, in, r, genDir, root)
		if errors.Is(err, errOver) {
			return s.end(ctx, ctl, in)
		}
		if err != nil {
			return err
		}
	}
	slog.Info("generation running", slog.String("intent", in.ID))
	return s.watch(ctx, ctl, in)
}

// watch waits for any container of the generation to exit, or for its
// control to stop serving, which leaves the adapter no way to activate.
// ctx ending leaves the workload running; either of the others drains
// it.
func (s *Supervisor) watch(ctx context.Context, ctl *controlServer, in *intent) error {
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	exited := make(chan error, len(roles))
	for _, r := range roles {
		go func(id string) { exited <- s.engine.WaitExited(waitCtx, id) }(in.container(r).ID)
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-ctl.failed:
		slog.Error("control stopped serving; ending the generation",
			slog.String("intent", in.ID), slog.String("error", err.Error()))
	case err := <-exited:
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && !errors.Is(err, ErrAbsent) {
			// The engine stopped answering: nothing is known to have
			// exited, so nothing is drained.
			return err
		}
	}
	return s.end(ctx, ctl, in)
}

// end drains a generation that can no longer run. Its control stops
// taking requests first, and those already in flight finish.
func (s *Supervisor) end(ctx context.Context, ctl *controlServer, in *intent) error {
	if err := ctl.srv.Shutdown(ctx); err != nil {
		return errorx.Wrap(err, "workload: control", errorx.KindUnavailable)
	}
	if err := s.drain(ctx, in); err != nil {
		return err
	}
	s.mu.Lock()
	generation := in.Generation
	s.mu.Unlock()
	slog.Warn("generation ended", slog.String("intent", in.ID), slog.String("generation", generation))
	return ErrGenerationEnded
}
