package workload

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"sync"

	"github.com/garamsh/garam/internal/errorx"
)

// Paths inside the containers. The runtime's and the workspace's are
// what Sherlock's own fixture mounts (deploy/runtime-fixture at
// sherlock v0.2.0).
const (
	memoryTarget    = "/data"
	workspaceTarget = "/var/lib/sherlock-workspace"
	runtimeConfig   = "/etc/sherlock/config.yaml"
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
}

// Supervisor runs one agent's workload on a standalone host.
type Supervisor struct {
	engine Engine
	garam  Activator
	cfg    Config
	issuer *x509.CertPool

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
	issuer := x509.NewCertPool()
	if !issuer.AppendCertsFromPEM(cfg.Adapter.IssuerPEM) {
		return nil, errorx.New(errorx.KindInvalidArgument, "workload: the agent's issuer holds no certificate")
	}
	return &Supervisor{engine: engine, garam: garam, cfg: cfg, issuer: issuer}, nil
}

// Run brings the desired generation up, or adopts the one running, and
// supervises it. It returns nil when ctx ends, leaving the workload
// running; ErrGenerationEnded once the generation exited and the rest
// of it was drained; and any other error with the workload left as it
// stood. It never forces a container to stop.
func (s *Supervisor) Run(ctx context.Context) error {
	st, err := loadState(s.cfg.Dir)
	if err != nil {
		return err
	}
	s.st = st
	if err := s.storage(ctx); err != nil {
		return err
	}
	in, err := s.recover(ctx)
	if err != nil {
		return err
	}
	if in == nil {
		if in, err = s.newIntent(); err != nil {
			return err
		}
	}
	return s.supervise(ctx, in)
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

// storage makes sure both volumes are the ones this host created:
// created where the record says none was yet, and refused where one is
// gone or carries another nonce.
func (s *Supervisor) storage(ctx context.Context) error {
	for _, v := range []struct {
		rec  **volumeRecord
		kind string
	}{{&s.st.Memory, "memory"}, {&s.st.Workspace, "workspace"}} {
		if *v.rec == nil {
			rec := &volumeRecord{Name: s.cfg.Prefix + v.kind + "-" + randomHex(8), Nonce: randomHex(16)}
			if err := s.locked(func() { *v.rec = rec }); err != nil {
				return err
			}
		}
		rec := *v.rec
		_, err := s.engine.InspectVolume(ctx, rec.Name)
		if errors.Is(err, ErrAbsent) && !rec.Confirmed {
			err = s.engine.CreateVolume(ctx, rec.Name, map[string]string{labelNonce: rec.Nonce})
		}
		if err != nil && !errors.Is(err, ErrAbsent) {
			return err
		}
		if err := s.verifyVolume(ctx, rec); err != nil {
			return err
		}
		if !rec.Confirmed {
			if err := s.locked(func() { rec.Confirmed = true }); err != nil {
				return err
			}
		}
	}
	return nil
}

// verifyVolume refuses a volume that is gone or does not carry the
// recorded nonce.
func (s *Supervisor) verifyVolume(ctx context.Context, rec *volumeRecord) error {
	v, err := s.engine.InspectVolume(ctx, rec.Name)
	if errors.Is(err, ErrAbsent) {
		return errorx.Wrap(ErrStorageMissing, rec.Name)
	}
	if err != nil {
		return err
	}
	if v.Labels[labelNonce] != rec.Nonce {
		return errorx.Wrap(ErrStorageReplaced, rec.Name)
	}
	return nil
}

// recover settles the intent the record holds: one still running what
// is desired is answered to be supervised again; one that ended, was
// draining, or runs something else is drained.
func (s *Supervisor) recover(ctx context.Context) (*intent, error) {
	in := s.st.Intent
	if in == nil || in.Phase == phaseEnded {
		return nil, nil
	}
	if in.Phase == phaseStarting {
		over, err := s.over(ctx, in)
		if err != nil {
			return nil, err
		}
		d := s.cfg.Desired
		if !over && !d.Replace && in.Revision == d.Revision && in.Epoch == d.Epoch && in.Images == d.Images {
			return in, nil
		}
	}
	return nil, s.drain(ctx, in)
}

// over reports whether any container of the intent can no longer run:
// one that started and is gone, exited, or started again since.
func (s *Supervisor) over(ctx context.Context, in *intent) (bool, error) {
	for _, r := range roles {
		c, err := s.adopt(ctx, in, r)
		if err != nil {
			return false, err
		}
		rec := in.container(r)
		switch {
		case c == nil:
			if rec.ID != "" {
				return true, nil
			}
		case c.Status == ContainerExited:
			return true, nil
		case c.Status == ContainerRunning && !rec.StartedAt.IsZero() && !c.StartedAt.Equal(rec.StartedAt):
			return true, nil
		}
	}
	return false, nil
}

// adopt answers the intent's container in role, or nil where the
// engine holds none under its name. A container the record has no ID
// for is adopted when its labels confirm the intent, which recovers a
// create whose answer was lost. Anything else under the name is
// ErrForeign.
func (s *Supervisor) adopt(ctx context.Context, in *intent, r Role) (*ContainerState, error) {
	rec := in.container(r)
	c, err := s.engine.InspectContainer(ctx, rec.Name)
	if errors.Is(err, ErrAbsent) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !s.owns(in, r, c) || (rec.ID != "" && rec.ID != c.ID) {
		return nil, errorx.Wrap(ErrForeign, rec.Name)
	}
	if rec.ID == "" {
		if err := s.locked(func() { rec.ID = c.ID }); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// owns reports whether c is what the intent created for role: the
// intent's nonce and identifier, its image, and its volumes.
func (s *Supervisor) owns(in *intent, r Role, c *ContainerState) bool {
	if c.Labels[labelNonce] != in.Nonce || c.Labels[labelIntent] != in.ID || c.Image != imageOf(in, r) {
		return false
	}
	for _, v := range s.volumesOf(r) {
		if c.Volumes[v.Target] != v.Volume {
			return false
		}
	}
	return true
}

func imageOf(in *intent, r Role) string {
	switch r {
	case RoleRuntime:
		return in.Images.Runtime
	case RoleWorkspace:
		return in.Images.Workspace
	}
	return in.Images.Adapter
}

// volumesOf is what each role mounts: the memory volume into the
// runtime and, for the outbox, the adapter; the workspace volume into
// the workspace alone.
func (s *Supervisor) volumesOf(r Role) []VolumeMount {
	switch r {
	case RoleRuntime, RoleAdapter:
		return []VolumeMount{{Volume: s.st.Memory.Name, Target: memoryTarget}}
	}
	return []VolumeMount{{Volume: s.st.Workspace.Name, Target: workspaceTarget}}
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
	if err := s.locked(func() { s.st.Intent = in }); err != nil {
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
	srv, err := s.serveControl(genDir, serving, in)
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()
	for _, r := range roles {
		err := s.ensure(ctx, in, r, genDir, root)
		if errors.Is(err, errOver) {
			return s.end(ctx, srv, in)
		}
		if err != nil {
			return err
		}
	}
	slog.Info("generation running", slog.String("intent", in.ID))
	return s.watch(ctx, srv, in)
}

// ensure brings one container of the intent up: created under its
// name where the engine holds none, its files copied in and started
// where it is created, and adopted where it runs.
func (s *Supervisor) ensure(ctx context.Context, in *intent, r Role, genDir string, root []byte) error {
	rec := in.container(r)
	c, err := s.adopt(ctx, in, r)
	if err != nil {
		return err
	}
	if c == nil {
		if rec.ID != "" {
			return errOver
		}
		if c, err = s.create(ctx, in, r, genDir); err != nil {
			return err
		}
	}
	switch c.Status {
	case ContainerCreated:
		return s.start(ctx, in, r, c.ID, root)
	case ContainerRunning:
		switch {
		case rec.StartedAt.IsZero():
			return s.locked(func() { rec.StartedAt = c.StartedAt })
		case !c.StartedAt.Equal(rec.StartedAt):
			return errOver
		}
		return nil
	}
	return errOver
}

// create creates the container, recording its ID, and checks its
// volumes again afterwards: an engine creates a volume a mount names
// where none exists, and such a volume carries no nonce.
func (s *Supervisor) create(ctx context.Context, in *intent, r Role, genDir string) (*ContainerState, error) {
	for _, v := range s.volumesOf(r) {
		if err := s.verifyVolume(ctx, s.volumeRecord(v.Volume)); err != nil {
			return nil, err
		}
	}
	spec := s.spec(in, r, genDir)
	id, err := s.engine.CreateContainer(ctx, spec)
	if errors.Is(err, ErrNameInUse) {
		c, err := s.adopt(ctx, in, r)
		if err == nil && c == nil {
			err = errorx.New(errorx.KindUnavailable, "workload: "+spec.Name+" was taken and is gone")
		}
		return c, err
	}
	if err != nil {
		return nil, err
	}
	if err := s.locked(func() { in.container(r).ID = id }); err != nil {
		return nil, err
	}
	for _, v := range s.volumesOf(r) {
		if err := s.verifyVolume(ctx, s.volumeRecord(v.Volume)); err != nil {
			return nil, err
		}
	}
	return s.engine.InspectContainer(ctx, id)
}

func (s *Supervisor) volumeRecord(name string) *volumeRecord {
	if s.st.Memory.Name == name {
		return s.st.Memory
	}
	return s.st.Workspace
}

// start copies the container's files in and starts it. A start that
// leaves it not running, or that the engine refuses because the
// runtime it joins has exited, is the generation over.
func (s *Supervisor) start(ctx context.Context, in *intent, r Role, id string, root []byte) error {
	if files := s.files(in, r, root); len(files) > 0 {
		if err := s.engine.CopyFiles(ctx, id, "/etc", files); err != nil {
			return err
		}
	}
	startErr := s.engine.StartContainer(ctx, id)
	c, err := s.engine.InspectContainer(ctx, id)
	if err != nil {
		return err
	}
	if startErr != nil {
		if r != RoleRuntime {
			if runtime, err := s.engine.InspectContainer(ctx, in.Runtime.ID); err == nil && runtime.Status != ContainerRunning {
				return errOver
			}
		}
		return startErr
	}
	if c.Status != ContainerRunning {
		return errOver
	}
	rec := in.container(r)
	return s.locked(func() { rec.StartedAt = c.StartedAt })
}

// spec is the container the intent runs in role.
func (s *Supervisor) spec(in *intent, r Role, genDir string) ContainerSpec {
	d := s.cfg.Desired
	spec := ContainerSpec{
		Name:    in.container(r).Name,
		Image:   imageOf(in, r),
		Labels:  map[string]string{labelNonce: in.Nonce, labelIntent: in.ID},
		Volumes: s.volumesOf(r),
	}
	switch r {
	case RoleRuntime:
		spec.User = runtimeUser
		spec.Env = d.ModelEnv
		spec.Cmd = []string{
			"agent", "--config", runtimeConfig,
			"--agent-id", d.Agent.String(), "--assignment-epoch", in.Epoch,
			"--memory-path", memoryTarget + "/memory.db",
			"--addr", "127.0.0.1:8080", "--workspace-addr", "127.0.0.1:8081",
		}
		if in.RequireStore {
			spec.Cmd = append(spec.Cmd, "--require-store")
		}
	case RoleWorkspace:
		spec.NetworkOf = in.Runtime.ID
		spec.CapAdd = workspaceCaps
		spec.Cmd = []string{"workspace"}
		spec.Env = []string{"SHERLOCK_WORKSPACE=" + workspaceTarget, "SHERLOCK_EXEC_UID=10001"}
	case RoleAdapter:
		spec.NetworkOf = in.Runtime.ID
		spec.User = runtimeUser
		spec.Cmd = []string{"adapter"}
		spec.Binds = []BindMount{{Source: genDir + "/" + socketDir, Target: socketTarget}}
		spec.Env = []string{
			"GARAM_ADAPTER_AGENT=" + d.Agent.String(),
			"GARAM_ADAPTER_MACHINE_URL=" + s.cfg.Adapter.MachineURL,
			"GARAM_ADAPTER_TLS_CERT_FILE=" + adapterFiles + "/agent.crt",
			"GARAM_ADAPTER_TLS_KEY_FILE=" + adapterFiles + "/agent.key",
			"GARAM_ADAPTER_SERVER_ROOT_FILE=" + adapterFiles + "/server-root.pem",
			"GARAM_ADAPTER_CONTROL_URL=https://" + ControlName,
			"GARAM_ADAPTER_CONTROL_ROOT_FILE=" + adapterFiles + "/control-root.pem",
			"GARAM_ADAPTER_PLACEMENT_TOKEN_FILE=" + adapterFiles + "/placement-token",
			"GARAM_ADAPTER_CONTROL_SOCKET=" + socketTarget + "/" + socketName,
			"GARAM_ADAPTER_OUTBOX_DIR=" + memoryTarget + "/outbox",
		}
	}
	return spec
}

// files is what is copied into a container before it starts, below
// /etc: the runtime's configuration, and the adapter's credential,
// roots and placement token. Each is owned by the account the
// container runs as and readable by it alone.
func (s *Supervisor) files(in *intent, r Role, root []byte) []File {
	own := func(path string, data []byte, mode os.FileMode) File {
		return File{Path: path, Data: data, Mode: mode, UID: runtimeUID, GID: runtimeUID}
	}
	dir := func(path string) File {
		return File{Path: path, Mode: 0o700, Dir: true, UID: runtimeUID, GID: runtimeUID}
	}
	a := s.cfg.Adapter
	switch r {
	case RoleRuntime:
		return []File{dir("sherlock"), own("sherlock/config.yaml", s.cfg.Desired.Config, 0o600)}
	case RoleAdapter:
		return []File{
			dir("garam"),
			own("garam/agent.crt", a.CertificatePEM, 0o600),
			own("garam/agent.key", a.KeyPEM, 0o600),
			own("garam/server-root.pem", a.ServerRootPEM, 0o600),
			own("garam/control-root.pem", root, 0o600),
			own("garam/placement-token", []byte(in.PlacementToken), 0o600),
		}
	}
	return nil
}

// watch waits for any container of the generation to exit. ctx ending
// leaves the workload running; an exit drains the rest of it.
func (s *Supervisor) watch(ctx context.Context, srv *http.Server, in *intent) error {
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	exited := make(chan error, len(roles))
	for _, r := range roles {
		go func(id string) { exited <- s.engine.WaitExited(waitCtx, id) }(in.container(r).ID)
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-exited:
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			// The engine stopped answering: nothing is known to have
			// exited, so nothing is drained.
			return err
		}
	}
	return s.end(ctx, srv, in)
}

// end drains a generation one of whose containers exited.
func (s *Supervisor) end(ctx context.Context, srv *http.Server, in *intent) error {
	_ = srv.Close()
	if err := s.drain(ctx, in); err != nil {
		return err
	}
	s.mu.Lock()
	generation := in.Generation
	s.mu.Unlock()
	slog.Warn("generation ended", slog.String("intent", in.ID), slog.String("generation", generation))
	return ErrGenerationEnded
}

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
