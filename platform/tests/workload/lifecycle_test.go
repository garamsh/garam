package workload_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/workload"
	"github.com/garamsh/garam/platform/internal/workload/docker"
)

// eventually waits for cond, polling, for as long as a container's
// start or exit takes on a loaded host.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("%s did not happen", what)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// signals answers every signal the engine delivered to a container
// since, as `docker events` recorded them.
func (h *host) signals(id string, since time.Time) []string {
	h.t.Helper()
	out := h.mustDocker("events", "--since", since.Format(time.RFC3339Nano), "--until", time.Now().Format(time.RFC3339Nano),
		"--filter", "container="+id, "--filter", "event=kill", "--format", "{{.Actor.Attributes.signal}}")
	return strings.Fields(out)
}

// exitCodes answers each exit code the engine recorded for a container
// since.
func (h *host) exitCodes(id string, since time.Time) []string {
	h.t.Helper()
	out := h.mustDocker("events", "--since", since.Format(time.RFC3339Nano), "--until", time.Now().Format(time.RFC3339Nano),
		"--filter", "container="+id, "--filter", "event=die", "--format", "{{.Actor.Attributes.exitCode}}")
	return strings.Fields(out)
}

// processesMarked answers the host's processes whose command line
// carries marker. A rootless container's processes are the host's
// too, so one that outlived its container would be found here.
func processesMarked(marker string) []string {
	var found []string
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err == nil && strings.Contains(string(cmdline), marker) {
			found = append(found, e.Name())
		}
	}
	return found
}

// size answers the size of a file inside a container, read by the
// probe, or -1 where there is none.
func (h *host) size(id, path string) int {
	h.t.Helper()
	out, err := h.docker("exec", id, h.probe(id), "size", path)
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(out)
	require.NoError(h.t, err, out)
	return n
}

// TestRealEngine_DrainEndsTheWholeWorkload holds the positive exit to
// the whole workload (T1): a probe that detaches twice into a new
// session and ignores SIGTERM, started in the runtime and in the
// workspace, is gone once the engine reports both exited, and its
// files stop growing. The control is the same probes growing their
// files while the generation runs, and alive on the host beforehand.
func TestRealEngine_DrainEndsTheWholeWorkload(t *testing.T) {
	h := newHost(t)
	first := h.start()
	h.activated(1)
	runtime, space := h.container(workload.RoleRuntime), h.container(workload.RoleWorkspace)
	marker := "probe-" + strings.TrimPrefix(h.prefix, testPrefix)
	memoryFile, workspaceFile := "/data/"+marker, "/var/lib/sherlock-workspace/"+marker
	h.mustDocker("exec", runtime, h.probe(runtime), "daemon", memoryFile)
	h.mustDocker("exec", space, h.probe(space), "daemon", workspaceFile)

	eventually(t, "both probes writing", func() bool {
		return h.size(runtime, memoryFile) > 0 && h.size(space, workspaceFile) > 0
	})
	require.Len(t, processesMarked(marker), 2, "the control: both detached probes run on the host")
	require.NoError(t, first.stop())

	since := time.Now()
	h.start(replace)
	h.activated(2)
	assert.Empty(t, processesMarked(marker), "no probe outlived its container")
	assert.Equal(t, []string{"15"}, h.signals(runtime, since), "the runtime was sent SIGTERM and nothing else")
	assert.Equal(t, []string{"15"}, h.signals(space, since), "and so was the workspace")

	successor, nextSpace := h.container(workload.RoleRuntime), h.container(workload.RoleWorkspace)
	require.NotEqual(t, runtime, successor)
	before := [2]int{h.size(successor, memoryFile), h.size(nextSpace, workspaceFile)}
	time.Sleep(time.Second)
	after := [2]int{h.size(successor, memoryFile), h.size(nextSpace, workspaceFile)}
	assert.Equal(t, before, after, "neither file grows after both writers exited")
	assert.Positive(t, before[0], "the control: the runtime's probe wrote before it was ended")
	assert.Positive(t, before[1], "the control: the workspace's probe wrote before it was ended")
}

// TestRealEngine_StuckTurnKeepsTheDrainPending holds the drain with no
// deadline (T2): a turn waiting on a workspace that does not answer
// keeps the runtime draining, the replacement pending and no successor
// created, with SIGTERM the only signal sent. The control is the same
// drain completing, and the successor activating, once the workspace
// answers again.
func TestRealEngine_StuckTurnKeepsTheDrainPending(t *testing.T) {
	h := newHost(t)
	first := h.start()
	h.activated(1)
	runtime, space, adapter := h.container(workload.RoleRuntime), h.container(workload.RoleWorkspace), h.container(workload.RoleAdapter)
	gateway := "http://127.0.0.1:8080/agents/" + testAgent.String()

	// The mock model's turn calls the workspace first; stopped, the
	// workspace takes the connection and never answers it. It is stopped
	// from the host, where a rootless container's processes are the
	// host user's: an engine pause would not hold, since the engine
	// lifts it to deliver the drain's signal.
	pid, err := strconv.Atoi(h.inspect(space, "{{.State.Pid}}"))
	require.NoError(t, err)
	require.NoError(t, syscall.Kill(pid, syscall.SIGSTOP))
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGCONT) })
	posted := h.mustDocker("exec", adapter, h.probe(adapter), "post", gateway+"/messages",
		`{"id":"garam-1180-stuck","content":"hello"}`)
	require.Equal(t, "202", posted)
	require.NoError(t, first.stop())

	since := time.Now()
	pending := h.start(replace)
	status := func() string {
		out, err := h.docker("exec", adapter, h.probe(adapter), "get", gateway+"/status")
		if err != nil {
			return ""
		}
		var s struct {
			State string `json:"state"`
		}
		_ = json.Unmarshal([]byte(out), &s)
		return s.State
	}
	eventually(t, "the runtime draining", func() bool { return status() == "draining" })
	// Draining is the runtime holding its turn after SIGTERM; it is
	// still so once the drain has had time to finish anything not
	// stuck.
	time.Sleep(5 * time.Second)
	assert.Equal(t, "draining", status(), "the runtime keeps its turn")
	assert.Equal(t, "true", h.inspect(runtime, "{{.State.Running}}"))
	assert.False(t, pending.returned(), "the replacement is pending")
	assert.Len(t, h.garam.generations(), 1, "no successor was activated")
	ids := strings.Fields(h.mustDocker("ps", "-aq", "--filter", "name=^"+h.prefix+"runtime-"))
	assert.Equal(t, []string{runtime}, ids, "no successor was created")

	require.NoError(t, syscall.Kill(pid, syscall.SIGCONT))
	h.activated(2)
	assert.Equal(t, []string{"15"}, h.signals(runtime, since), "the runtime was sent SIGTERM and nothing else")
	assert.NotContains(t, h.exitCodes(runtime, since), "137", "the runtime was not killed")
}

// TestRealEngine_HeldLockKeepsTheSuccessorFromServing holds Sherlock's
// writer lock as the writer check (T3): with the memory store's lock
// held by a leftover holder, the successor exits before it reports any
// status, so nothing is activated for it and the generation ends. The
// control is the same start once the lock is free.
func TestRealEngine_HeldLockKeepsTheSuccessorFromServing(t *testing.T) {
	h := newHost(t)
	first := h.start()
	h.activated(1)
	h.mustDocker("kill", "--signal", "TERM", h.container(workload.RoleRuntime))
	require.ErrorIs(t, first.result(t), workload.ErrGenerationEnded)
	// A leftover writer: the store's lock taken and held, as a runtime
	// nothing reaped would hold it.
	holder := h.prefix + "lock-holder"
	h.mustDocker("run", "-d", "--name", holder, "--user", "65532:65532", "--network", "none",
		"--mount", "type=volume,src="+h.volume("memory")+",dst=/data",
		"--entrypoint", "flock", h.images.Workspace, "--nonblock", "/data/memory.db.lock", "sleep", "3600")
	require.Equal(t, "true", h.inspect(holder, "{{.State.Running}}"), "the holder took the lock")

	err := h.start().result(t)
	require.ErrorIs(t, err, workload.ErrGenerationEnded)
	assert.Len(t, h.garam.generations(), 1, "the successor was never activated")

	h.mustDocker("rm", "-f", holder)
	h.start()
	h.activated(2)
}

// TestRealEngine_RefusesARestartedRuntime holds activation to the
// runtime the supervisor started (T4): a runtime restarted under it
// mints a new generation the adapter asks for, which is refused, and
// the generation ends. The control is the first activation of the same
// runtime, before its restart.
func TestRealEngine_RefusesARestartedRuntime(t *testing.T) {
	h := newHost(t)
	r := h.start()
	h.activated(1)
	h.mustDocker("restart", h.container(workload.RoleRuntime))
	require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)
	assert.Len(t, h.garam.generations(), 1, "the restarted runtime was not activated")
}

// TestRealEngine_RefusesStorageItCannotProve holds the memory volume to
// the nonce this host created it with (T6): deleted, or recreated
// under the same name, it refuses and nothing is created on it. The
// control is the volume as it was, which runs a successor.
func TestRealEngine_RefusesStorageItCannotProve(t *testing.T) {
	h := newHost(t)
	end := func(r *running) {
		t.Helper()
		h.mustDocker("kill", "--signal", "TERM", h.container(workload.RoleRuntime))
		require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)
	}
	r := h.start()
	h.activated(1)
	memory := h.volume("memory")
	end(r)
	r = h.start()
	h.activated(2)
	end(r)

	h.mustDocker("volume", "rm", memory)
	require.ErrorIs(t, h.start().result(t), workload.ErrStorageMissing)
	assert.Empty(t, h.container(workload.RoleRuntime), "nothing was created")

	h.mustDocker("volume", "create", memory)
	require.ErrorIs(t, h.start().result(t), workload.ErrStorageReplaced)
	assert.Empty(t, h.container(workload.RoleRuntime), "nothing was created")
}

// losingEngine is the real engine with the answer to every runtime
// create lost after the container was made.
type losingEngine struct {
	workload.Engine
}

func (e losingEngine) CreateContainer(ctx context.Context, spec workload.ContainerSpec) (string, error) {
	id, err := e.Engine.CreateContainer(ctx, spec)
	if err == nil && strings.Contains(spec.Name, "runtime-") {
		return "", errorx.New(errorx.KindUnavailable, "the create's answer was lost")
	}
	return id, err
}

// inFlightEngine is the real engine with the run ended as it first
// creates role's container: the create goes on in the daemon, as one
// whose client went away does, and the run is answered that its client
// went. landed answers what that create made.
type inFlightEngine struct {
	workload.Engine
	role   workload.Role
	landed chan landedCreate
	once   sync.Once
}

type landedCreate struct {
	id  string
	err error
}

func (e *inFlightEngine) CreateContainer(ctx context.Context, spec workload.ContainerSpec) (string, error) {
	cut := false
	if strings.Contains(spec.Name, string(e.role)+"-") {
		e.once.Do(func() { cut = true })
	}
	if !cut {
		return e.Engine.CreateContainer(ctx, spec)
	}
	go func() {
		id, err := e.Engine.CreateContainer(context.Background(), spec)
		e.landed <- landedCreate{id, err}
	}()
	return "", errorx.New(errorx.KindUnavailable, "the run's client went away")
}

// TestRealEngine_AdoptsOnlyWhatItCreated holds intent recovery by name
// and nonce on the real engine (T7, T8): a runtime whose create answer
// was lost is adopted and started, not duplicated; a create a stopped
// run left in flight is waited for by the next run, which adopts what
// it made and activates without another start (#1604); a container
// under the same name without the intent's nonce is left untouched and
// the run stays pending.
func TestRealEngine_AdoptsOnlyWhatItCreated(t *testing.T) {
	for _, role := range []workload.Role{workload.RoleWorkspace, workload.RoleAdapter} {
		t.Run("a create left in flight is waited for: "+string(role), func(t *testing.T) {
			h := newHost(t)
			engine := &inFlightEngine{Engine: h.engine, role: role, landed: make(chan landedCreate, 1)}
			require.Error(t, h.startOn(engine).result(t), "the first run ends as it creates")

			next := h.start()
			made := <-engine.landed
			eventually(t, "the next run activating or returning", func() bool {
				return len(h.garam.generations()) > 0 || next.returned()
			})
			require.False(t, next.returned(), "the run that met it waits and runs on: %v", next.err)
			h.activated(1)
			running := h.inspect(h.container(role), "{{.Id}}")
			// The cut-off create almost always reaches the daemon first,
			// and the next run then adopts what it made. Where the next
			// run's own create got there first, the cut-off one is refused
			// the name, and the next run's is the one container.
			if made.err == nil {
				t.Log("the next run adopted the cut-off create")
				assert.Equal(t, made.id, running, "the container the cut-off create made is the one running")
			} else {
				t.Log("the next run's own create took the name first")
				assert.ErrorIs(t, made.err, workload.ErrNameInUse, "the cut-off create lost the name to the next run's")
			}
		})
	}
	t.Run("a lost create is adopted", func(t *testing.T) {
		h := newHost(t)
		require.Error(t, h.startOn(losingEngine{h.engine}).result(t))
		made := h.container(workload.RoleRuntime)
		require.NotEmpty(t, made)
		h.start()
		h.activated(1)
		assert.Equal(t, made, h.container(workload.RoleRuntime), "the container the lost create made is the one running")
	})
	t.Run("a stranger under the name is left", func(t *testing.T) {
		h := newHost(t)
		require.Error(t, h.startOn(losingEngine{h.engine}).result(t))
		made := h.container(workload.RoleRuntime)
		name := strings.TrimPrefix(h.inspect(made, "{{.Name}}"), "/")
		intent := h.inspect(made, `{{index .Config.Labels "sh.garam.workload.intent"}}`)
		h.mustDocker("rm", made)
		// Everything the supervisor would have made but the nonce: its
		// name, intent, image and volume.
		stranger := h.mustDocker("create", "--name", name, "--label", "sh.garam.workload.nonce=someone-else",
			"--label", "sh.garam.workload.intent="+intent,
			"--mount", "type=volume,src="+h.volume("memory")+",dst=/data", h.images.Runtime)

		require.ErrorIs(t, h.start().result(t), workload.ErrForeign)
		assert.Equal(t, "created", h.inspect(stranger, "{{.State.Status}}"), "never started")
		assert.Empty(t, h.garam.generations())
	})
}

// TestRealEngine_DecidesNothingWithoutTheEngine holds an unreachable
// engine as pending (T11): the supervisor starts, stops and activates
// nothing, and the generation it ran keeps running with its adapter.
// The control is the same supervisor adopting it once the engine is
// reachable.
func TestRealEngine_DecidesNothingWithoutTheEngine(t *testing.T) {
	h := newHost(t)
	first := h.start()
	h.activated(1)
	require.NoError(t, first.stop())
	runtime, adapter := h.container(workload.RoleRuntime), h.container(workload.RoleAdapter)

	unreachable, err := docker.New("unix://" + filepath.Join(h.dir, "no-engine.sock"))
	require.NoError(t, err)
	err = h.startOn(unreachable, replace).result(t)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "%v", err)
	assert.False(t, errors.Is(err, workload.ErrGenerationEnded))
	assert.Equal(t, "true", h.inspect(runtime, "{{.State.Running}}"), "the runtime runs on")
	assert.Equal(t, "true", h.inspect(adapter, "{{.State.Running}}"), "the adapter keeps its token")
	assert.Len(t, h.garam.generations(), 1)

	again := h.start()
	eventually(t, "the adopted generation's socket accepting", func() bool {
		sockets, _ := filepath.Glob(filepath.Join(h.dir, "generations", "*", "control", "control.sock"))
		if len(sockets) != 1 {
			return false
		}
		conn, err := net.Dial("unix", sockets[0])
		if err == nil {
			_ = conn.Close()
		}
		return err == nil
	})
	assert.False(t, again.returned(), "the control: the reachable engine supervises the generation")
	assert.Equal(t, runtime, h.container(workload.RoleRuntime), "the same runtime, adopted")
	assert.Len(t, h.garam.generations(), 1)
}

// eventTimes answers when the engine recorded event for a container
// since, in nanoseconds.
func (h *host) eventTimes(id, event string, since time.Time) []int64 {
	h.t.Helper()
	out := h.mustDocker("events", "--since", since.Format(time.RFC3339Nano), "--until", time.Now().Format(time.RFC3339Nano),
		"--filter", "container="+id, "--filter", "event="+event, "--format", "{{.TimeNano}}")
	var times []int64
	for _, f := range strings.Fields(out) {
		n, err := strconv.ParseInt(f, 10, 64)
		require.NoError(h.t, err)
		times = append(times, n)
	}
	return times
}

// gatewayState answers the runtime's state as its loopback status
// says, read from inside the adapter, or "" where it does not answer.
func (h *host) gatewayState(adapter string) string {
	out, err := h.docker("exec", adapter, h.probe(adapter), "get",
		"http://127.0.0.1:8080/agents/"+testAgent.String()+"/status")
	if err != nil {
		return ""
	}
	var s struct {
		State string `json:"state"`
	}
	_ = json.Unmarshal([]byte(out), &s)
	return s.State
}

// stopWorkspace stops the workspace's first process from the host, so
// a tool call the runtime makes waits on it, and answers what resumes
// it.
func (h *host) stopWorkspace(space string) func() {
	h.t.Helper()
	pid, err := strconv.Atoi(h.inspect(space, "{{.State.Pid}}"))
	require.NoError(h.t, err)
	require.NoError(h.t, syscall.Kill(pid, syscall.SIGSTOP))
	resume := func() { _ = syscall.Kill(pid, syscall.SIGCONT) }
	h.t.Cleanup(resume)
	return resume
}

// postStuckTurn has the runtime take a message whose turn's first tool
// call waits on the stopped workspace.
func (h *host) postStuckTurn(adapter string) {
	h.t.Helper()
	posted := h.mustDocker("exec", adapter, h.probe(adapter), "post",
		"http://127.0.0.1:8080/agents/"+testAgent.String()+"/messages",
		`{"id":"garam-1180-turn","content":"hello"}`)
	require.Equal(h.t, "202", posted)
}

// TestRealEngine_DrainFindsARenamedRuntimeByItsID holds the drain to
// the container IDs it recorded: a runtime renamed under the
// supervisor is still sent SIGTERM and waited for before the successor
// starts, which would otherwise meet its store's lock held. The control
// is the same replacement with nothing renamed.
func TestRealEngine_DrainFindsARenamedRuntimeByItsID(t *testing.T) {
	for _, renamed := range []bool{false, true} {
		t.Run(fmt.Sprintf("renamed %v", renamed), func(t *testing.T) {
			h := newHost(t)
			first := h.start()
			h.activated(1)
			require.NoError(t, first.stop())
			runtime := h.container(workload.RoleRuntime)
			if renamed {
				h.mustDocker("rename", runtime, h.prefix+"renamed-by-someone")
			}
			since := time.Now()
			h.start(replace)
			h.activated(2)
			assert.Equal(t, []string{"15"}, h.signals(runtime, since), "the runtime was sent SIGTERM by its ID")
			assert.Len(t, h.exitCodes(runtime, since), 1, "and it exited")
		})
	}
}

// TestRealEngine_RequiresTheStoreOnlyAfterAServingReport holds
// --require-store to a serving report: a generation whose adapter is
// gone before its activation is answered, so no report follows, starts
// its successor without the flag. The control is the same generation
// reporting serving, whose successor starts with it.
func TestRealEngine_RequiresTheStoreOnlyAfterAServingReport(t *testing.T) {
	for _, reported := range []bool{false, true} {
		t.Run(fmt.Sprintf("reported %v", reported), func(t *testing.T) {
			h := newHost(t)
			if !reported {
				h.garam.before = func() {
					h.garam.mu.Lock()
					h.garam.before = nil
					h.garam.mu.Unlock()
					out, _ := h.docker("ps", "-q", "--filter", "name=^"+h.prefix+"adapter-")
					_, _ = h.docker("kill", "--signal", "TERM", out)
					_, _ = h.docker("wait", out)
				}
			}
			r := h.start()
			h.activated(1)
			if reported {
				eventually(t, "the serving report recorded", func() bool {
					raw, err := os.ReadFile(filepath.Join(h.dir, "workload.json"))
					var st struct {
						Memory struct {
							Served bool `json:"served"`
						} `json:"memory"`
					}
					return err == nil && json.Unmarshal(raw, &st) == nil && st.Memory.Served
				})
				h.mustDocker("kill", "--signal", "TERM", h.container(workload.RoleRuntime))
			}
			require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)
			h.start()
			h.activated(2)
			cmd := h.inspect(h.container(workload.RoleRuntime), "{{json .Config.Cmd}}")
			assert.Equal(t, reported, strings.Contains(cmd, "--require-store"), cmd)
		})
	}
}

// TestRealEngine_ActivatesOnlyAServingRuntime holds activation to a
// runtime that reports serving: one told to stop while garam was out
// of reach reports draining, and nothing is activated for it once
// garam is back. The control is the same runtime still serving, which
// is activated once garam is back.
func TestRealEngine_ActivatesOnlyAServingRuntime(t *testing.T) {
	for _, draining := range []bool{false, true} {
		t.Run(fmt.Sprintf("draining %v", draining), func(t *testing.T) {
			h := newHost(t)
			h.garam.setUndecided(true)
			h.start()
			var runtime, space, adapter string
			eventually(t, "the generation up", func() bool {
				runtime, space, adapter = h.container(workload.RoleRuntime), h.container(workload.RoleWorkspace),
					h.container(workload.RoleAdapter)
				return adapter != "" && h.inspect(adapter, "{{.State.Running}}") == "true" && h.gatewayState(adapter) == "serving"
			})
			if draining {
				h.stopWorkspace(space)
				h.postStuckTurn(adapter)
				h.mustDocker("kill", "--signal", "TERM", runtime)
				eventually(t, "the runtime draining", func() bool { return h.gatewayState(adapter) == "draining" })
			}
			h.garam.setUndecided(false)
			if !draining {
				h.activated(1)
				return
			}
			seen := func() int { return strings.Count(h.mustDocker("logs", adapter), "runtime not serving; not activated") }
			after := seen()
			eventually(t, "the adapter reading the draining runtime twice more", func() bool { return seen() >= after+2 })
			assert.Empty(t, h.garam.generations(), "nothing is activated for a draining runtime")
		})
	}
}

// TestRealEngine_DrainKeepsTheWorkspaceForTheTurn holds the drain's
// order: the runtime is told to stop first and waited for, and the
// workspace only after it exited, so a tool call in flight completes.
// The control is the turn's tool result, recorded in the workspace.
func TestRealEngine_DrainKeepsTheWorkspaceForTheTurn(t *testing.T) {
	h := newHost(t)
	first := h.start()
	h.activated(1)
	runtime, space, adapter := h.container(workload.RoleRuntime), h.container(workload.RoleWorkspace), h.container(workload.RoleAdapter)
	resume := h.stopWorkspace(space)
	h.postStuckTurn(adapter)
	require.NoError(t, first.stop())

	since := time.Now()
	h.start(replace)
	eventually(t, "the runtime draining", func() bool { return h.gatewayState(adapter) == "draining" })
	assert.Empty(t, h.signals(space, since), "the workspace is not told to stop while the runtime drains")
	resume()
	h.activated(2)
	runtimeExit, spaceSignal := h.eventTimes(runtime, "die", since), h.eventTimes(space, "kill", since)
	require.Len(t, runtimeExit, 1)
	require.Len(t, spaceSignal, 1)
	assert.Less(t, runtimeExit[0], spaceSignal[0], "the workspace is signalled after the runtime exited")
	next := h.container(workload.RoleWorkspace)
	found := h.mustDocker("exec", next, h.probe(next), "find", "/var/lib/sherlock-workspace", "question.txt")
	assert.NotEmpty(t, found, "the control: the turn's tool call completed and wrote its result")
}
