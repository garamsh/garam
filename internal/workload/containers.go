package workload

import (
	"context"
	"errors"
	"os"

	"github.com/garamsh/garam/internal/errorx"
)

// recover settles the intent the record holds: one still running what
// is desired is answered to be supervised again; one that ended, was
// draining, runs something else, or is to be replaced is drained.
func (s *Supervisor) recover(ctx context.Context, replace bool) (*intent, error) {
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
		if !over && !replace && !d.Replace && in.Revision == d.Revision && in.Epoch == d.Epoch && in.Images == d.Images {
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
// engine holds none. A container whose ID the record holds is found by
// that ID alone, so a rename neither hides it nor puts another in its
// place. One the record has no ID for is found by its name and adopted
// when its labels confirm the intent, which recovers a create whose
// answer was lost; anything else under the name is ErrForeign.
func (s *Supervisor) adopt(ctx context.Context, in *intent, r Role) (*ContainerState, error) {
	rec := in.container(r)
	if rec.ID != "" {
		c, err := s.engine.InspectContainer(ctx, rec.ID)
		if errors.Is(err, ErrAbsent) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if !s.owns(in, r, c) {
			return nil, errorx.Wrap(ErrForeign, rec.ID)
		}
		return c, nil
	}
	c, err := s.engine.InspectContainer(ctx, rec.Name)
	if errors.Is(err, ErrAbsent) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !s.owns(in, r, c) {
		return nil, errorx.Wrap(ErrForeign, rec.Name)
	}
	if err := s.locked(func() { rec.ID = c.ID }); err != nil {
		return nil, err
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
	if r == RoleAdapter {
		if err := s.outbox(ctx, in.Runtime.ID, false); err != nil {
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

// start copies the container's files in and starts it. A start that
// leaves it not running, or that the engine refuses because the
// runtime it joins has exited, is the generation over.
func (s *Supervisor) start(ctx context.Context, in *intent, r Role, id string, root []byte) error {
	if r == RoleRuntime {
		if err := s.outbox(ctx, id, true); err != nil {
			return err
		}
	}
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
		if d.Ego != nil {
			spec.Cmd = append(spec.Cmd, "--ego-file", runtimeEgo)
		}
		if d.Instructions != nil {
			spec.Cmd = append(spec.Cmd, "--instructions-file", runtimeInstr)
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
			"GARAM_ADAPTER_OUTBOX_DIR=" + outboxTarget,
		}
	}
	return spec
}

// files is what is copied into a container before it starts, below
// /etc: the runtime's configuration and the garam server root it
// verifies garam's key set by, and the adapter's credential, roots and
// placement token. Each is owned by the account the
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
		files := []File{dir("sherlock"), own("sherlock/config.yaml", s.renderedConfig, 0o600)}
		if s.cfg.MessageIssuer != "" {
			files = append(files, own("sherlock/garam-server-root.pem", a.ServerRootPEM, 0o600))
		}
		if ego := s.cfg.Desired.Ego; ego != nil {
			files = append(files, own("sherlock/ego.md", ego, 0o600))
		}
		if instructions := s.cfg.Desired.Instructions; instructions != nil {
			files = append(files, own("sherlock/instructions.md", instructions, 0o600))
		}
		return files
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

// outbox makes sure the memory volume's outbox is a directory of its
// own before the adapter mounts it alone, read through the runtime
// container that mounts the volume. Where it is absent and create is
// set, it is made, owned by the runtime's account. Anything but a
// directory there, a link above all, refuses: a link inside the volume
// would hand the adapter the whole of it, and the path is never
// repaired, since replacing it could drop what it holds. No turn runs
// before the adapter exists, so nothing changes the path between this
// check and the adapter's mount, and a link made afterwards does not
// move a mount already made (ADR-0085).
func (s *Supervisor) outbox(ctx context.Context, runtimeID string, create bool) error {
	path := memoryTarget + "/" + outboxDir
	st, err := s.engine.StatPath(ctx, runtimeID, path)
	if errors.Is(err, ErrAbsent) && create {
		return s.engine.CopyFiles(ctx, runtimeID, memoryTarget, []File{
			{Path: outboxDir, Mode: 0o755, Dir: true, UID: runtimeUID, GID: runtimeUID},
		})
	}
	if errors.Is(err, ErrAbsent) {
		return errorx.Wrap(ErrOutboxReplaced, path+" is gone")
	}
	if err != nil {
		return err
	}
	if !st.Mode.IsDir() || st.LinkTarget != "" {
		return errorx.Wrap(ErrOutboxReplaced, path+" is "+st.Mode.Type().String())
	}
	return nil
}
