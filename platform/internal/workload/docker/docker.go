// Package docker is the workload Engine over the Docker Engine API, as
// a rootless daemon serves it to the host user the supervisor runs as.
package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/workload"
)

// stopNever is the stop timeout every container is created with: a
// person's own `docker stop` against it waits rather than kills, as
// the supervisor's drain does.
var stopNever = -1

// Engine reaches one Docker daemon.
type Engine struct {
	api *client.Client
}

// New connects to the daemon at host, a unix:// address, negotiating
// the API version the daemon serves.
func New(host string) (*Engine, error) {
	if !strings.HasPrefix(host, "unix://") {
		return nil, errorx.New(errorx.KindInvalidArgument, "docker: the engine is reached on a unix:// socket")
	}
	api, err := client.New(client.WithHost(host), client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, errorx.Wrap(err, "docker: client", errorx.KindInvalidArgument)
	}
	return &Engine{api: api}, nil
}

// Close releases the connection.
func (e *Engine) Close() error {
	return e.api.Close()
}

// kindOf renders an engine error as workload's, so the supervisor
// reads absence and a taken name and treats the rest as the engine
// unavailable.
func kindOf(err error, what string) error {
	switch {
	case err == nil:
		return nil
	case cerrdefs.IsNotFound(err):
		return errorx.Wrap(workload.ErrAbsent, what+": "+err.Error())
	case cerrdefs.IsConflict(err):
		return errorx.Wrap(workload.ErrNameInUse, what+": "+err.Error())
	}
	return errorx.Wrap(err, "docker: "+what, errorx.KindUnavailable)
}

func (e *Engine) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	_, err := e.api.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name, Labels: labels})
	return kindOf(err, "create volume "+name)
}

func (e *Engine) InspectVolume(ctx context.Context, name string) (*workload.VolumeState, error) {
	res, err := e.api.VolumeInspect(ctx, name, client.VolumeInspectOptions{})
	if err != nil {
		return nil, kindOf(err, "inspect volume "+name)
	}
	return &workload.VolumeState{Name: res.Volume.Name, Labels: res.Volume.Labels}, nil
}

// CreateContainer creates spec's container in the arrangement's fixed
// shape: every capability dropped but those named, no new privileges,
// no restart, no published port, the host's namespaces shared with
// nothing, and a stop that never escalates to a kill.
func (e *Engine) CreateContainer(ctx context.Context, spec workload.ContainerSpec) (string, error) {
	mounts := make([]mount.Mount, 0, len(spec.Volumes)+len(spec.Binds))
	for _, v := range spec.Volumes {
		m := mount.Mount{Type: mount.TypeVolume, Source: v.Volume, Target: v.Target, ReadOnly: v.ReadOnly}
		if v.Subpath != "" {
			m.VolumeOptions = &mount.VolumeOptions{Subpath: v.Subpath}
		}
		mounts = append(mounts, m)
	}
	for _, b := range spec.Binds {
		mounts = append(mounts, mount.Mount{Type: mount.TypeBind, Source: b.Source, Target: b.Target})
	}
	host := &container.HostConfig{
		Mounts:        mounts,
		CapDrop:       []string{"ALL"},
		CapAdd:        spec.CapAdd,
		SecurityOpt:   []string{"no-new-privileges"},
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
	}
	switch {
	case spec.NetworkOf != "":
		host.NetworkMode = container.NetworkMode("container:" + spec.NetworkOf)
	case spec.NoNetwork:
		host.NetworkMode = container.NetworkMode("none")
	}
	res, err := e.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: spec.Name,
		Config: &container.Config{
			Image:       spec.Image,
			User:        spec.User,
			Cmd:         spec.Cmd,
			Env:         spec.Env,
			Labels:      spec.Labels,
			StopTimeout: &stopNever,
		},
		HostConfig: host,
	})
	if err != nil {
		return "", kindOf(err, "create "+spec.Name)
	}
	return res.ID, nil
}

func (e *Engine) InspectContainer(ctx context.Context, nameOrID string) (*workload.ContainerState, error) {
	res, err := e.api.ContainerInspect(ctx, nameOrID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, kindOf(err, "inspect "+nameOrID)
	}
	c := res.Container
	state := &workload.ContainerState{
		ID:           c.ID,
		Name:         strings.TrimPrefix(c.Name, "/"),
		RestartCount: c.RestartCount,
		Volumes:      map[string]string{},
		Status:       workload.ContainerRunning,
	}
	if c.Config != nil {
		state.Image, state.Labels = c.Config.Image, c.Config.Labels
	}
	if c.State != nil {
		switch c.State.Status {
		case container.StateCreated:
			state.Status = workload.ContainerCreated
		case container.StateExited, container.StateDead:
			state.Status, state.ExitCode = workload.ContainerExited, c.State.ExitCode
		}
		if started, err := time.Parse(time.RFC3339Nano, c.State.StartedAt); err == nil && !started.IsZero() &&
			state.Status != workload.ContainerCreated {
			state.StartedAt = started
		}
	}
	for _, m := range c.Mounts {
		if m.Type == mount.TypeVolume {
			state.Volumes[m.Destination] = m.Name
		}
	}
	return state, nil
}

// CopyFiles writes files as one archive the daemon extracts keeping
// each entry's owner, which works on a created container where a bind
// mount of a host file would show the host user's ownership instead.
func (e *Engine) CopyFiles(ctx context.Context, id, dir string, files []workload.File) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range files {
		header := &tar.Header{Name: f.Path, Mode: int64(f.Mode.Perm()), Uid: f.UID, Gid: f.GID, ModTime: time.Now()}
		if f.Dir {
			header.Typeflag, header.Name = tar.TypeDir, f.Path+"/"
		} else {
			header.Typeflag, header.Size = tar.TypeReg, int64(len(f.Data))
		}
		if err := tw.WriteHeader(header); err != nil {
			return errorx.Wrap(err, "docker: archive", errorx.KindInternal)
		}
		if !f.Dir {
			if _, err := tw.Write(f.Data); err != nil {
				return errorx.Wrap(err, "docker: archive", errorx.KindInternal)
			}
		}
	}
	if err := tw.Close(); err != nil {
		return errorx.Wrap(err, "docker: archive", errorx.KindInternal)
	}
	_, err := e.api.CopyToContainer(ctx, id, client.CopyToContainerOptions{
		DestinationPath: dir, Content: &buf, CopyUIDGID: true,
	})
	return kindOf(err, "copy into "+id)
}

// StatPath reads what is at path without following a link, as the
// engine's archive stat reports it.
func (e *Engine) StatPath(ctx context.Context, id, path string) (*workload.PathState, error) {
	res, err := e.api.ContainerStatPath(ctx, id, client.ContainerStatPathOptions{Path: path})
	if err != nil {
		return nil, kindOf(err, "stat "+path+" in "+id)
	}
	return &workload.PathState{Mode: res.Stat.Mode, LinkTarget: res.Stat.LinkTarget}, nil
}

func (e *Engine) StartContainer(ctx context.Context, id string) error {
	_, err := e.api.ContainerStart(ctx, id, client.ContainerStartOptions{})
	return kindOf(err, "start "+id)
}

// SignalContainer sends signal and nothing after it. A container that
// is not running answers a conflict, which is the signal's purpose
// already met.
func (e *Engine) SignalContainer(ctx context.Context, id, signal string) error {
	_, err := e.api.ContainerKill(ctx, id, client.ContainerKillOptions{Signal: signal})
	if cerrdefs.IsConflict(err) {
		return nil
	}
	return kindOf(err, "signal "+id)
}

func (e *Engine) WaitExited(ctx context.Context, id string) error {
	wait := e.api.ContainerWait(ctx, id, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case <-wait.Result:
		return nil
	case err := <-wait.Error:
		return kindOf(err, "wait for "+id)
	}
}

func (e *Engine) RemoveContainer(ctx context.Context, id string) error {
	_, err := e.api.ContainerRemove(ctx, id, client.ContainerRemoveOptions{})
	return kindOf(err, "remove "+id)
}

// ArchiveFrom reads path out of a container as the engine's archive,
// which a created or exited container answers too.
func (e *Engine) ArchiveFrom(ctx context.Context, id, path string) (io.ReadCloser, error) {
	res, err := e.api.CopyFromContainer(ctx, id, client.CopyFromContainerOptions{SourcePath: path})
	if err != nil {
		return nil, kindOf(err, "copy "+path+" out of "+id)
	}
	return res.Content, nil
}

// logLines bounds what Logs answers: the reason a one-shot container
// gave is in its last lines.
const logLines = "20"

func (e *Engine) Logs(ctx context.Context, id string) (string, error) {
	res, err := e.api.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: logLines})
	if err != nil {
		return "", kindOf(err, "logs of "+id)
	}
	defer func() { _ = res.Close() }()
	var out bytes.Buffer
	if _, err := stdcopy.StdCopy(&out, &out, res); err != nil {
		return "", errorx.Wrap(err, "docker: logs of "+id, errorx.KindUnavailable)
	}
	return strings.TrimSpace(out.String()), nil
}
