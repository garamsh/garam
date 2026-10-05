package workload

import (
	"context"
	"io/fs"
	"time"
)

// Engine is the container engine the supervisor alone holds. It has no
// stop with a timeout and no forced kill: a drain is a SIGTERM and a
// wait for as long as it takes (ADR-0085).
//
// An Engine answers ErrAbsent for a container or volume it does not
// hold and ErrNameInUse for a create whose name is taken. Every other
// error is the engine unavailable, or refusing in a way the supervisor
// does not act on, and leaves what it asked for pending.
type Engine interface {
	// CreateVolume creates a volume under name with labels. A volume
	// already under that name is left as it is and answered as created,
	// so the caller reads its labels back to know which it got.
	CreateVolume(ctx context.Context, name string, labels map[string]string) error
	// InspectVolume answers the labels of the volume under name.
	InspectVolume(ctx context.Context, name string) (*VolumeState, error)
	// CreateContainer creates spec's container, not started, and
	// answers its ID.
	CreateContainer(ctx context.Context, spec ContainerSpec) (string, error)
	// InspectContainer answers the container under a name or an ID.
	InspectContainer(ctx context.Context, nameOrID string) (*ContainerState, error)
	// StatPath answers what is at path in a container, created or
	// running, without following a link there.
	StatPath(ctx context.Context, id, path string) (*PathState, error)
	// CopyFiles writes files below dir in a container, created or
	// running, each with the owner and mode it names. It writes into a
	// volume the container mounts there too.
	CopyFiles(ctx context.Context, id, dir string, files []File) error
	// StartContainer starts a created container.
	StartContainer(ctx context.Context, id string) error
	// SignalContainer sends signal to a container's first process. A
	// container that is not running is answered as signalled.
	SignalContainer(ctx context.Context, id, signal string) error
	// WaitExited returns once the container is not running, however
	// long that takes, or with ctx's error.
	WaitExited(ctx context.Context, id string) error
	// RemoveContainer removes a container that is not running.
	RemoveContainer(ctx context.Context, id string) error
}

// VolumeState is what the engine says of a volume.
type VolumeState struct {
	Name   string
	Labels map[string]string
}

// ContainerSpec is one container of a generation. Whatever it does not
// name is the arrangement's fixed shape, which the Engine applies to
// every container: every capability dropped but those named, no new
// privileges, no restart, no published port, and no namespace shared
// with the host.
type ContainerSpec struct {
	Name   string
	Image  string
	Labels map[string]string
	// User is uid:gid, empty for the image's own.
	User string
	Cmd  []string
	Env  []string
	// NetworkOf is the container whose network namespace this one
	// joins, empty for one of its own.
	NetworkOf string
	Volumes   []VolumeMount
	Binds     []BindMount
	CapAdd    []string
}

// VolumeMount mounts a named volume, or the directory Subpath names
// inside it. An engine creates a volume a mount names where none
// exists, which is why the supervisor checks a volume again after
// each create.
type VolumeMount struct {
	Volume  string
	Target  string
	Subpath string
}

// PathState is what is at a path: its mode, and a link's target.
type PathState struct {
	Mode       fs.FileMode
	LinkTarget string
}

// BindMount mounts a host directory.
type BindMount struct {
	Source string
	Target string
}

// ContainerStatus is a container's state as the engine reports it.
type ContainerStatus string

const (
	ContainerCreated ContainerStatus = "created"
	ContainerRunning ContainerStatus = "running"
	ContainerExited  ContainerStatus = "exited"
)

// ContainerState is what the engine says of a container.
type ContainerState struct {
	ID     string
	Name   string
	Image  string
	Labels map[string]string
	// Status is ContainerCreated, ContainerRunning or ContainerExited;
	// anything the engine reports between them reads as running, so a
	// paused or restarting container is never taken for one that
	// exited.
	Status       ContainerStatus
	StartedAt    time.Time
	RestartCount int
	// Volumes is each named volume mounted, by target.
	Volumes map[string]string
}

// File is one entry CopyFiles writes, its path relative to the
// destination. A directory carries no data.
type File struct {
	Path string
	Data []byte
	Mode fs.FileMode
	Dir  bool
	UID  int
	GID  int
}
