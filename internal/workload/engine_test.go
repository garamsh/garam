package workload_test

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/workload"
)

// stubEngine is an engine held in memory. A container exits on SIGTERM
// unless it was made to ignore it, and on exit() otherwise; nothing
// else ends one. Every signal it is sent is kept, so a test can say
// none but SIGTERM was.
type stubEngine struct {
	mu      sync.Mutex
	volumes map[string]map[string]string
	// paths is what each volume holds, by path inside it, and data a
	// regular file's content.
	paths      map[string]map[string]workload.PathState
	data       map[string]map[string][]byte
	containers map[string]*stubContainer
	byName     map[string]string
	seq        int
	signals    []string
	// down answers every call unavailable.
	down bool
	// loseCreate creates the container and then answers unavailable,
	// as a create whose answer was lost does.
	loseCreate func(spec workload.ContainerSpec) bool
	// beforeCreate runs as a create begins, and afterStart once a
	// container started, each under the engine's lock.
	beforeCreate func(spec workload.ContainerSpec)
	afterStart   func(c *stubContainer)
	// changed is closed and replaced on every change, for a test
	// waiting on a condition.
	changed chan struct{}
}

type stubContainer struct {
	spec      workload.ContainerSpec
	id        string
	status    workload.ContainerStatus
	startedAt time.Time
	restarts  int
	files     map[string]workload.File
	// ignoresTerm keeps it running through SIGTERM.
	ignoresTerm bool
	exited      chan struct{}
	// exitCode is what it exits with, and logs what it wrote.
	exitCode int
	logs     string
}

func newStubEngine() *stubEngine {
	return &stubEngine{
		volumes: map[string]map[string]string{}, containers: map[string]*stubContainer{},
		paths:  map[string]map[string]workload.PathState{}, data: map[string]map[string][]byte{},
		byName: map[string]string{}, changed: make(chan struct{}),
	}
}

var errDown = errorx.New(errorx.KindUnavailable, "stub engine: unavailable")

// refuse answers why a call is not served: the engine down, or the
// caller's context done, which a real engine refuses too. The caller
// holds mu.
func (e *stubEngine) refuse(ctx context.Context) error {
	if e.down {
		return errDown
	}
	return ctx.Err()
}

func (e *stubEngine) notify() {
	close(e.changed)
	e.changed = make(chan struct{})
}

// await blocks until cond holds, read under the engine's lock after
// each change, or fails after a bound no correct run comes near.
func (e *stubEngine) await(cond func() bool) error {
	deadline := time.After(10 * time.Second)
	for {
		e.mu.Lock()
		ok, changed := cond(), e.changed
		e.mu.Unlock()
		if ok {
			return nil
		}
		select {
		case <-changed:
		case <-deadline:
			return fmt.Errorf("stub engine: condition not met")
		}
	}
}

// named answers every container of role the engine holds, in the
// order they were created. The caller holds mu.
func (e *stubEngine) named(role workload.Role) []*stubContainer {
	var out []*stubContainer
	for i := 1; i <= e.seq; i++ {
		c, ok := e.containers[fmt.Sprintf("c%d", i)]
		if ok && strings.Contains(c.spec.Name, string(role)+"-") {
			out = append(out, c)
		}
	}
	return out
}

func (e *stubEngine) CreateVolume(ctx context.Context, name string, labels map[string]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return err
	}
	if _, ok := e.volumes[name]; !ok {
		e.volumes[name] = labels
	}
	e.notify()
	return nil
}

func (e *stubEngine) InspectVolume(ctx context.Context, name string) (*workload.VolumeState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return nil, err
	}
	labels, ok := e.volumes[name]
	if !ok {
		return nil, workload.ErrAbsent
	}
	return &workload.VolumeState{Name: name, Labels: labels}, nil
}

func (e *stubEngine) CreateContainer(ctx context.Context, spec workload.ContainerSpec) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return "", err
	}
	if _, ok := e.byName[spec.Name]; ok {
		return "", workload.ErrNameInUse
	}
	if e.beforeCreate != nil {
		e.beforeCreate(spec)
	}
	for _, v := range spec.Volumes {
		if _, ok := e.volumes[v.Volume]; !ok {
			// As Docker does: a mount names a volume into existence.
			e.volumes[v.Volume] = map[string]string{}
		}
	}
	e.seq++
	c := &stubContainer{spec: spec, id: fmt.Sprintf("c%d", e.seq), status: workload.ContainerCreated,
		files: map[string]workload.File{}, exited: make(chan struct{})}
	e.containers[c.id], e.byName[spec.Name] = c, c.id
	e.notify()
	if e.loseCreate != nil && e.loseCreate(spec) {
		return "", errDown
	}
	return c.id, nil
}

func (e *stubEngine) lookup(nameOrID string) (*stubContainer, bool) {
	if c, ok := e.containers[nameOrID]; ok {
		return c, true
	}
	c, ok := e.containers[e.byName[nameOrID]]
	return c, ok
}

func (e *stubEngine) InspectContainer(ctx context.Context, nameOrID string) (*workload.ContainerState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return nil, err
	}
	c, ok := e.lookup(nameOrID)
	if !ok {
		return nil, workload.ErrAbsent
	}
	volumes := map[string]string{}
	for _, v := range c.spec.Volumes {
		volumes[v.Target] = v.Volume
	}
	return &workload.ContainerState{
		ID: c.id, Name: c.spec.Name, Image: c.spec.Image, Labels: c.spec.Labels, Status: c.status,
		StartedAt: c.startedAt, RestartCount: c.restarts, Volumes: volumes, ExitCode: c.exitCode,
	}, nil
}

func (e *stubEngine) CopyFiles(ctx context.Context, id, dir string, files []workload.File) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return err
	}
	c, ok := e.lookup(id)
	if !ok {
		return workload.ErrAbsent
	}
	for _, f := range files {
		if volume, rel, ok := c.volumeAt(dir + "/" + f.Path); ok {
			mode := f.Mode
			if f.Dir {
				mode |= fs.ModeDir
			}
			e.put(volume, rel, workload.PathState{Mode: mode}, f.Data)
			continue
		}
		c.files[dir+"/"+f.Path] = f
	}
	return nil
}

// volumeAt answers the volume a container mounts at path's directory,
// and path inside it.
func (c *stubContainer) volumeAt(path string) (string, string, bool) {
	for _, v := range c.spec.Volumes {
		if rel, ok := strings.CutPrefix(path, v.Target+"/"); ok && v.Subpath == "" {
			return v.Volume, rel, true
		}
	}
	return "", "", false
}

func (e *stubEngine) StatPath(ctx context.Context, id, path string) (*workload.PathState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return nil, err
	}
	c, ok := e.lookup(id)
	if !ok {
		return nil, workload.ErrAbsent
	}
	volume, rel, ok := c.volumeAt(path)
	if !ok {
		return nil, workload.ErrAbsent
	}
	st, ok := e.paths[volume][rel]
	if !ok {
		return nil, workload.ErrAbsent
	}
	return &st, nil
}

func (e *stubEngine) StartContainer(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return err
	}
	c, ok := e.lookup(id)
	if !ok {
		return workload.ErrAbsent
	}
	if c.spec.NetworkOf != "" {
		if owner, ok := e.lookup(c.spec.NetworkOf); !ok || owner.status != workload.ContainerRunning {
			return errorx.New(errorx.KindFailedPrecondition, "stub engine: cannot join a network namespace that is not running")
		}
	}
	c.status, c.startedAt = workload.ContainerRunning, time.Now().UTC().Truncate(time.Microsecond)
	if len(c.spec.Cmd) > 0 && c.spec.Cmd[0] == workload.CopyCommand {
		e.copyLocked(c)
	}
	if e.afterStart != nil {
		e.afterStart(c)
	}
	e.notify()
	return nil
}

func (e *stubEngine) SignalContainer(ctx context.Context, id, signal string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return err
	}
	c, ok := e.lookup(id)
	if !ok {
		return workload.ErrAbsent
	}
	e.signals = append(e.signals, c.spec.Name+":"+signal)
	if c.status != workload.ContainerExited && (signal != "SIGTERM" || !c.ignoresTerm) {
		e.exitLocked(c)
	}
	e.notify()
	return nil
}

func (e *stubEngine) exitLocked(c *stubContainer) {
	if c.status == workload.ContainerExited {
		return
	}
	c.status = workload.ContainerExited
	close(c.exited)
}

// exit ends a container as its own process ending would.
func (e *stubEngine) exit(c *stubContainer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exitLocked(c)
	e.notify()
}

func (e *stubEngine) WaitExited(ctx context.Context, id string) error {
	e.mu.Lock()
	if err := e.refuse(ctx); err != nil {
		e.mu.Unlock()
		return err
	}
	c, ok := e.lookup(id)
	if !ok {
		e.mu.Unlock()
		return workload.ErrAbsent
	}
	created, exited := c.status == workload.ContainerCreated, c.exited
	e.mu.Unlock()
	if created {
		return nil
	}
	select {
	case <-exited:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *stubEngine) RemoveContainer(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return err
	}
	c, ok := e.lookup(id)
	if !ok {
		return workload.ErrAbsent
	}
	if c.status == workload.ContainerRunning {
		return errorx.New(errorx.KindFailedPrecondition, "stub engine: removing a running container")
	}
	delete(e.containers, c.id)
	delete(e.byName, c.spec.Name)
	e.notify()
	return nil
}

// put records what a volume holds at rel. The caller holds mu.
func (e *stubEngine) put(volume, rel string, st workload.PathState, data []byte) {
	if e.paths[volume] == nil {
		e.paths[volume] = map[string]workload.PathState{}
	}
	if e.data[volume] == nil {
		e.data[volume] = map[string][]byte{}
	}
	e.paths[volume][rel], e.data[volume][rel] = st, data
}

// write puts a regular file into a volume, as a runtime writes one.
func (e *stubEngine) write(volume, rel, content string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.put(volume, rel, workload.PathState{Mode: 0o600}, []byte(content))
}

// snapshot answers what a volume holds, each path with its mode and
// content, for comparing a volume before and after.
func (e *stubEngine) snapshot(volume string) map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]string{}
	for rel, st := range e.paths[volume] {
		out[rel] = st.Mode.String() + " " + string(e.data[volume][rel])
	}
	return out
}

// copyLocked runs the copy's helper as garam's copy-memory does: it
// exits 1 where the source's writer lock is held, and otherwise copies
// the store files and the outbox to the target and exits 0. The
// caller holds mu.
func (e *stubEngine) copyLocked(c *stubContainer) {
	var from, to string
	for _, v := range c.spec.Volumes {
		if v.ReadOnly {
			from = v.Volume
		} else {
			to = v.Volume
		}
	}
	defer e.exitLocked(c)
	if _, held := e.paths[from]["memory.db.lock.held"]; held {
		c.exitCode, c.logs = 1, "level=ERROR msg=\"garam exited\" error=\"the store's writer lock is held\""
		return
	}
	for rel, st := range e.paths[from] {
		if rel == "memory.db" || strings.HasPrefix(rel, "memory.db-") || rel == "outbox" || strings.HasPrefix(rel, "outbox/") {
			e.put(to, rel, st, e.data[from][rel])
		}
	}
}

// ArchiveFrom answers the volume a container mounts at path as a tar
// stream, every entry owned by the runtime's account.
func (e *stubEngine) ArchiveFrom(ctx context.Context, id, at string) (io.ReadCloser, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return nil, err
	}
	c, ok := e.lookup(id)
	if !ok {
		return nil, workload.ErrAbsent
	}
	var volume string
	for _, v := range c.spec.Volumes {
		if v.Target == at {
			volume = v.Volume
		}
	}
	if volume == "" {
		return nil, workload.ErrAbsent
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	base := path.Base(at)
	rels := []string{}
	for rel := range e.paths[volume] {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	entries := append([]string{""}, rels...)
	for _, rel := range entries {
		h := &tar.Header{Name: base + "/", Typeflag: tar.TypeDir, Mode: 0o755, Uid: 65532, Gid: 65532}
		if rel != "" {
			st, data := e.paths[volume][rel], e.data[volume][rel]
			h = &tar.Header{Name: base + "/" + rel, Mode: int64(st.Mode.Perm()), Uid: 65532, Gid: 65532}
			switch {
			case st.Mode.IsDir():
				h.Typeflag, h.Name = tar.TypeDir, h.Name+"/"
			case st.Mode&fs.ModeSymlink != 0:
				h.Typeflag, h.Linkname = tar.TypeSymlink, st.LinkTarget
			default:
				h.Typeflag, h.Size = tar.TypeReg, int64(len(data))
			}
		}
		if err := tw.WriteHeader(h); err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write(e.data[volume][rel]); err != nil {
				return nil, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return io.NopCloser(&buf), nil
}

func (e *stubEngine) Logs(ctx context.Context, id string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := e.refuse(ctx); err != nil {
		return "", err
	}
	c, ok := e.lookup(id)
	if !ok {
		return "", workload.ErrAbsent
	}
	return c.logs, nil
}
