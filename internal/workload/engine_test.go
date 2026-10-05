package workload_test

import (
	"context"
	"fmt"
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
	mu         sync.Mutex
	volumes    map[string]map[string]string
	containers map[string]*stubContainer
	byName     map[string]string
	seq        int
	signals    []string
	// down answers every call unavailable.
	down bool
	// loseCreate creates the container and then answers unavailable,
	// as a create whose answer was lost does.
	loseCreate func(spec workload.ContainerSpec) bool
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
}

func newStubEngine() *stubEngine {
	return &stubEngine{
		volumes: map[string]map[string]string{}, containers: map[string]*stubContainer{},
		byName: map[string]string{}, changed: make(chan struct{}),
	}
}

var errDown = errorx.New(errorx.KindUnavailable, "stub engine: unavailable")

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

func (e *stubEngine) CreateVolume(_ context.Context, name string, labels map[string]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return errDown
	}
	if _, ok := e.volumes[name]; !ok {
		e.volumes[name] = labels
	}
	e.notify()
	return nil
}

func (e *stubEngine) InspectVolume(_ context.Context, name string) (*workload.VolumeState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return nil, errDown
	}
	labels, ok := e.volumes[name]
	if !ok {
		return nil, workload.ErrAbsent
	}
	return &workload.VolumeState{Name: name, Labels: labels}, nil
}

func (e *stubEngine) CreateContainer(_ context.Context, spec workload.ContainerSpec) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return "", errDown
	}
	if _, ok := e.byName[spec.Name]; ok {
		return "", workload.ErrNameInUse
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

func (e *stubEngine) InspectContainer(_ context.Context, nameOrID string) (*workload.ContainerState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return nil, errDown
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
		StartedAt: c.startedAt, RestartCount: c.restarts, Volumes: volumes,
	}, nil
}

func (e *stubEngine) CopyFiles(_ context.Context, id, dir string, files []workload.File) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return errDown
	}
	c, ok := e.lookup(id)
	if !ok {
		return workload.ErrAbsent
	}
	for _, f := range files {
		c.files[dir+"/"+f.Path] = f
	}
	return nil
}

func (e *stubEngine) StartContainer(_ context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return errDown
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
	e.notify()
	return nil
}

func (e *stubEngine) SignalContainer(_ context.Context, id, signal string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return errDown
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
	if e.down {
		e.mu.Unlock()
		return errDown
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

func (e *stubEngine) RemoveContainer(_ context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return errDown
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
