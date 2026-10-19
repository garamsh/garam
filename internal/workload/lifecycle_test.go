package workload_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/workload"
)

// activateOnce has the adapter of the generation running now activate
// it, and answers the activation.
func (h *host) activateOnce(adapter *stubContainer) (*delivery.Activation, string) {
	h.t.Helper()
	auth, token := h.adapterOf(adapter)
	generation := newGeneration()
	a, err := auth.Activate(context.Background(), delivery.RequestID(delivery.TokenExecutionContract, binding(generation), token), binding(generation), token)
	require.NoError(h.t, err)
	return a, generation
}

// serve has the adapter activate the generation running now and report
// its runtime serving, which is what records that it served.
func (h *host) serve(adapter *stubContainer) {
	h.t.Helper()
	a, generation := h.activateOnce(adapter)
	auth, _ := h.adapterOf(adapter)
	status := &delivery.RuntimeStatus{Agent: testAgent, Epoch: "3", Generation: generation, Revision: "r1",
		State: delivery.RuntimeServing, StartedAt: time.Now()}
	require.NoError(h.t, auth.Report(context.Background(), a.ID, status, time.Now()))
}

func replace(d *workload.Desired) { d.Replace = true }

// count answers how many containers of role the engine holds.
func (h *host) count(role workload.Role) int {
	h.engine.mu.Lock()
	defer h.engine.mu.Unlock()
	return len(h.engine.named(role))
}

// TestRun_RequiresTheStoreOnceARuntimeServed holds --require-store to
// the durable record (ADR-0085): a first start on a new volume goes
// without it, a supervisor restart adopts the same runtime without
// starting another, and every start after a runtime served on the
// volume carries it, whether the supervisor was restarted in between
// or the generation ended on its own.
func TestRun_RequiresTheStoreOnceARuntimeServed(t *testing.T) {
	h := newHost(t)
	first := h.start()
	runtime, _, _ := h.serving()
	assert.NotContains(t, runtime.spec.Cmd, "--require-store", "the control: the volume's first start creates the store")
	require.NoError(t, first.stop(), "stopping the supervisor leaves the workload")

	again := h.start()
	adopted, _, _ := h.serving()
	assert.Same(t, runtime, adopted, "a restarted supervisor adopts the runtime rather than starting one")
	require.NoError(t, again.stop())

	again = h.start(replace)
	require.NoError(t, h.engine.await(func() bool { return runtime.status == workload.ContainerExited }))
	successor, _, _ := h.serving()
	assert.NotContains(t, successor.spec.Cmd, "--require-store",
		"the control: nothing served on the volume yet, so nothing says a store exists")
	_, _, adapter := h.serving()
	h.serve(adapter)
	require.NoError(t, again.stop(), "the supervisor stops after its runtime served")

	again = h.start(replace)
	require.NoError(t, h.engine.await(func() bool { return successor.status == workload.ContainerExited }))
	third, _, adapter := h.serving()
	assert.Contains(t, third.spec.Cmd, "--require-store", "a start after a runtime served requires the store")

	h.serve(adapter)
	h.engine.exit(third)
	require.ErrorIs(t, again.result(t), workload.ErrGenerationEnded)
	h.start()
	fourth, _, _ := h.serving()
	assert.Contains(t, fourth.spec.Cmd, "--require-store", "and so does the start after a generation ended")
}

// TestDrain_WaitsForTheRuntimeWithoutForce holds the drain (ADR-0085):
// a runtime that does not exit on SIGTERM keeps the replacement
// pending, is never sent anything stronger, and no successor is
// created; once it exits the successor starts. The control is the same
// replacement of a runtime that exits on the signal.
func TestDrain_WaitsForTheRuntimeWithoutForce(t *testing.T) {
	h := newHost(t)
	first := h.start()
	runtime, workspace, adapter := h.serving()
	require.NoError(t, first.stop())
	h.engine.mu.Lock()
	runtime.ignoresTerm = true
	h.engine.mu.Unlock()

	pending := h.start(replace)
	require.NoError(t, h.engine.await(func() bool {
		return slices.Contains(h.engine.signals, runtime.spec.Name+":SIGTERM")
	}))
	assert.Equal(t, workload.ContainerRunning, runtime.status, "a runtime finishing its turn keeps running")
	assert.Equal(t, workload.ContainerRunning, workspace.status, "and keeps the tools its turn calls")
	assert.Equal(t, workload.ContainerRunning, adapter.status, "the adapter outlasts the writers it serves")
	assert.Equal(t, 1, h.count(workload.RoleRuntime), "no successor while the runtime runs")
	h.engine.mu.Lock()
	for _, s := range h.engine.signals {
		assert.Contains(t, s, ":SIGTERM", "nothing but SIGTERM is sent")
	}
	h.engine.mu.Unlock()

	h.engine.exit(runtime)
	successor, _, _ := h.serving()
	assert.NotSame(t, runtime, successor, "the successor starts once the runtime exited")
	require.NoError(t, pending.stop())
}

// TestDrain_WaitsForTheWorkspaceToo holds the positive exit to both
// writers: a runtime that exited is not enough while the workspace,
// where the runtime's tools run, still does.
func TestDrain_WaitsForTheWorkspaceToo(t *testing.T) {
	h := newHost(t)
	first := h.start()
	runtime, workspace, _ := h.serving()
	require.NoError(t, first.stop())
	h.engine.mu.Lock()
	workspace.ignoresTerm = true
	h.engine.mu.Unlock()

	h.start(replace)
	require.NoError(t, h.engine.await(func() bool { return runtime.status == workload.ContainerExited }))
	assert.Equal(t, 1, h.count(workload.RoleRuntime), "no successor while the workspace runs")

	h.engine.exit(workspace)
	successor, _, _ := h.serving()
	assert.NotSame(t, runtime, successor)
}

// TestRun_EndsTheGenerationAnyContainerOfItLeaves holds the generation
// as one workload: whichever of the three exits, the other two are
// drained and removed and the run ends.
func TestRun_EndsTheGenerationAnyContainerOfItLeaves(t *testing.T) {
	for _, role := range []workload.Role{workload.RoleRuntime, workload.RoleWorkspace, workload.RoleAdapter} {
		t.Run(string(role), func(t *testing.T) {
			h := newHost(t)
			r := h.start()
			runtime, workspace, adapter := h.serving()
			gone := map[workload.Role]*stubContainer{
				workload.RoleRuntime: runtime, workload.RoleWorkspace: workspace, workload.RoleAdapter: adapter,
			}[role]
			h.engine.exit(gone)
			require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)
			for _, role := range []workload.Role{workload.RoleRuntime, workload.RoleWorkspace, workload.RoleAdapter} {
				assert.Zero(t, h.count(role), "%s is removed once it exited", role)
			}
		})
	}
}

// TestRun_AdoptsTheContainerALostCreateMade holds intent recovery by
// name and nonce: a create whose answer was lost made a container the
// record has no ID for, and the next run adopts it rather than creating
// a second.
func TestRun_AdoptsTheContainerALostCreateMade(t *testing.T) {
	h := newHost(t)
	h.engine.loseCreate = func(spec workload.ContainerSpec) bool { return true }
	err := h.start().result(t)
	require.Error(t, err)
	require.Equal(t, 1, h.count(workload.RoleRuntime), "the lost create made one runtime")

	h.engine.mu.Lock()
	h.engine.loseCreate = nil
	h.engine.mu.Unlock()
	h.start()
	runtime, _, _ := h.serving()
	assert.Equal(t, 1, h.count(workload.RoleRuntime), "the run adopted it and created none")
	assert.Equal(t, "c1", runtime.id)
}

// TestRun_LeavesAForeignContainerUntouched holds that a name alone
// adopts nothing: a container under the derived name without the
// intent's nonce is neither started, signalled nor removed, and the
// run stays pending. The control is
// TestRun_AdoptsTheContainerALostCreateMade, where the same name
// carries the nonce and is adopted.
func TestRun_LeavesAForeignContainerUntouched(t *testing.T) {
	h := newHost(t)
	h.engine.loseCreate = func(workload.ContainerSpec) bool { return true }
	require.Error(t, h.start().result(t))
	// The create's answer is lost, and the container under the name is
	// then a stranger's: the same name and intent, another nonce.
	h.engine.mu.Lock()
	h.engine.loseCreate = nil
	planted := h.engine.named(workload.RoleRuntime)[0]
	planted.spec.Labels = map[string]string{
		"sh.garam.workload.nonce": "someone-else", "sh.garam.workload.intent": planted.spec.Labels["sh.garam.workload.intent"],
	}
	h.engine.mu.Unlock()

	err := h.start().result(t)
	require.ErrorIs(t, err, workload.ErrForeign)
	h.engine.mu.Lock()
	defer h.engine.mu.Unlock()
	assert.Equal(t, workload.ContainerCreated, planted.status, "never started")
	assert.Empty(t, h.engine.signals, "never signalled")
	assert.Len(t, h.engine.named(workload.RoleRuntime), 1, "never removed or duplicated")
}

// TestRun_RefusesStorageItCannotProve holds a volume to the nonce this
// host created it with: one deleted, one recreated under the same name,
// and one carrying another nonce each refuse, while the volume as it
// was runs.
func TestRun_RefusesStorageItCannotProve(t *testing.T) {
	tests := []struct {
		name string
		edit func(volumes map[string]map[string]string, name string)
		want error
	}{
		{"deleted", func(v map[string]map[string]string, n string) { delete(v, n) }, workload.ErrStorageMissing},
		{"recreated without a nonce", func(v map[string]map[string]string, n string) { v[n] = map[string]string{} }, workload.ErrStorageReplaced},
		{"another nonce", func(v map[string]map[string]string, n string) {
			v[n] = map[string]string{"sh.garam.workload.nonce": "0123"}
		}, workload.ErrStorageReplaced},
	}
	for _, tt := range tests {
		for _, role := range []workload.Role{workload.RoleRuntime, workload.RoleWorkspace} {
			t.Run(tt.name+" "+string(role)+" volume", func(t *testing.T) {
				h := newHost(t)
				r := h.start()
				runtime, workspace, _ := h.serving()
				volume := map[workload.Role]string{
					workload.RoleRuntime: runtime.spec.Volumes[0].Volume, workload.RoleWorkspace: workspace.spec.Volumes[0].Volume,
				}[role]
				h.engine.exit(runtime)
				require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)

				r = h.start()
				successor, _, _ := h.serving()
				assert.NotSame(t, runtime, successor, "the control: the volume as it was runs a successor")
				h.engine.exit(successor)
				require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)

				h.engine.mu.Lock()
				tt.edit(h.engine.volumes, volume)
				h.engine.mu.Unlock()
				err := h.start().result(t)
				require.True(t, errors.Is(err, tt.want), "want %v, got %v", tt.want, err)
				assert.Zero(t, h.count(workload.RoleRuntime)+h.count(workload.RoleWorkspace), "nothing was created on it")
			})
		}
	}
}

// TestRun_DecidesNothingWhileTheEngineIsDown holds an engine outage as
// pending: no container is started, signalled or removed and nothing is
// activated, and the workload is supervised again once it answers.
func TestRun_DecidesNothingWhileTheEngineIsDown(t *testing.T) {
	h := newHost(t)
	first := h.start()
	runtime, _, _ := h.serving()
	require.NoError(t, first.stop())

	h.engine.mu.Lock()
	h.engine.down = true
	h.engine.mu.Unlock()
	require.Error(t, h.start(replace).result(t))
	h.engine.mu.Lock()
	assert.Empty(t, h.engine.signals, "nothing is signalled while the engine is down")
	h.engine.down = false
	h.engine.mu.Unlock()
	assert.Empty(t, h.garam.asked())

	h.start()
	adopted, _, _ := h.serving()
	assert.Same(t, runtime, adopted, "the control: with the engine back, the same runtime is supervised")
}

// TestRun_RefusesAVolumeACreateNamedIntoExistence holds the check after
// a create: a volume removed between the supervisor's check and the
// runtime's create is made again by the mount, without the nonce, and
// the runtime is never started on it. The control is the run before,
// on the same volume as it was.
func TestRun_RefusesAVolumeACreateNamedIntoExistence(t *testing.T) {
	h := newHost(t)
	r := h.start()
	runtime, _, _ := h.serving()
	memory := runtime.spec.Volumes[0].Volume
	h.engine.exit(runtime)
	require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded, "the control: a generation ran on the volume")

	h.engine.mu.Lock()
	h.engine.beforeCreate = func(spec workload.ContainerSpec) {
		if len(spec.Volumes) > 0 && spec.Volumes[0].Volume == memory {
			delete(h.engine.volumes, memory)
		}
	}
	h.engine.mu.Unlock()
	require.ErrorIs(t, h.start().result(t), workload.ErrStorageReplaced)
	h.engine.mu.Lock()
	defer h.engine.mu.Unlock()
	for _, c := range h.engine.named(workload.RoleRuntime) {
		assert.Equal(t, workload.ContainerCreated, c.status, "the runtime was never started on it")
	}
}

// TestRun_EndsTheGenerationWhoseRuntimeWasRemoved holds a container
// that is gone as one that exited: a runtime removed before the
// supervisor watches it ends the generation and the other two are
// drained, rather than the run stopping with them still running. The
// control is TestRun_EndsTheGenerationAnyContainerOfItLeaves, where the
// runtime exits and stays.
func TestRun_EndsTheGenerationWhoseRuntimeWasRemoved(t *testing.T) {
	h := newHost(t)
	h.engine.afterStart = func(c *stubContainer) {
		if c.spec.User == "65532:65532" && len(c.spec.Binds) > 0 {
			runtime := h.engine.containers[c.spec.NetworkOf]
			runtime.status = workload.ContainerExited
			close(runtime.exited)
			delete(h.engine.containers, runtime.id)
			delete(h.engine.byName, runtime.spec.Name)
		}
	}
	require.ErrorIs(t, h.start().result(t), workload.ErrGenerationEnded)
	assert.Zero(t, h.count(workload.RoleWorkspace)+h.count(workload.RoleAdapter), "the rest is drained and removed")
}

// TestRun_ReturnsNothingWhenStoppedMidStart holds a requested stop as
// no failure wherever it lands: stopped while the generation is being
// brought up, the run returns nil and leaves what it started running,
// and the next run takes it up. The control is the same stop after the
// generation is up, in TestRun_RequiresTheStoreOnceARuntimeServed.
func TestRun_ReturnsNothingWhenStoppedMidStart(t *testing.T) {
	h := newHost(t)
	var r *running
	h.engine.afterStart = func(c *stubContainer) {
		if c.spec.NetworkOf == "" {
			r.cancel()
		}
	}
	h.engine.mu.Lock()
	r = h.start()
	h.engine.mu.Unlock()
	require.NoError(t, r.result(t), "a stop mid-start is a stop")
	h.engine.mu.Lock()
	h.engine.afterStart = nil
	runtime := h.live(workload.RoleRuntime)
	h.engine.mu.Unlock()
	require.NotNil(t, runtime, "the runtime it started runs on")

	h.start()
	adopted, _, _ := h.serving()
	assert.Same(t, runtime, adopted, "the next run takes it up")
}

// TestRun_RequiresTheStoreOnlyAfterAServingReport holds --require-store
// to a serving report, never to an activation: a runtime activated and
// gone before any report starts again without the flag, and the same
// sequence with a serving report, the control, starts with it.
func TestRun_RequiresTheStoreOnlyAfterAServingReport(t *testing.T) {
	for _, reported := range []bool{false, true} {
		t.Run(fmt.Sprintf("reported serving: %v", reported), func(t *testing.T) {
			h := newHost(t)
			r := h.start()
			runtime, _, adapter := h.serving()
			if reported {
				h.serve(adapter)
			} else {
				h.activateOnce(adapter)
			}
			h.engine.exit(runtime)
			require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)
			h.start()
			next, _, _ := h.serving()
			assert.Equal(t, reported, slices.Contains(next.spec.Cmd, "--require-store"))
		})
	}
}

// TestDrain_FindsARenamedContainerByItsID holds the drain to the IDs it
// recorded: a runtime renamed under the supervisor is still signalled
// and waited for, and a stranger given its old name is neither, so no
// successor starts while the renamed runtime runs. The control is the
// same drain with nothing renamed.
func TestDrain_FindsARenamedContainerByItsID(t *testing.T) {
	for _, renamed := range []bool{false, true} {
		t.Run(fmt.Sprintf("renamed: %v", renamed), func(t *testing.T) {
			h := newHost(t)
			first := h.start()
			runtime, _, _ := h.serving()
			require.NoError(t, first.stop())
			h.engine.mu.Lock()
			runtime.ignoresTerm = true
			if renamed {
				delete(h.engine.byName, runtime.spec.Name)
				old := runtime.spec.Name
				runtime.spec.Name = "renamed-by-someone"
				h.engine.byName[runtime.spec.Name] = runtime.id
				h.engine.seq++
				stranger := &stubContainer{spec: workload.ContainerSpec{Name: old}, id: fmt.Sprintf("c%d", h.engine.seq),
					status: workload.ContainerRunning, exited: make(chan struct{}), files: map[string]workload.File{}}
				h.engine.containers[stranger.id], h.engine.byName[old] = stranger, stranger.id
			}
			h.engine.mu.Unlock()

			pending := h.start(replace)
			require.NoError(t, h.engine.await(func() bool {
				return slices.Contains(h.engine.signals, runtime.spec.Name+":SIGTERM")
			}), "the runtime is signalled by its ID")
			assert.False(t, pending.returned(), "the drain waits for it")
			h.engine.mu.Lock()
			assert.Len(t, h.engine.signals, 1, "nothing else is signalled while it runs")
			h.engine.mu.Unlock()
			h.engine.exit(runtime)
			require.NoError(t, h.engine.await(func() bool {
				for _, c := range h.engine.named(workload.RoleRuntime) {
					if c != runtime && c.status == workload.ContainerRunning && c.spec.Labels != nil {
						return true
					}
				}
				return false
			}), "the successor starts once it exited")
		})
	}
}

// TestRun_RefusesAnOutboxThatIsNotADirectory holds the adapter's mount
// of the outbox alone: a link or a file where the outbox is refuses
// before the adapter is created, and the path is left as it was. The
// control is the outbox the runtime's first start made, a directory,
// which the adapter mounts alone.
func TestRun_RefusesAnOutboxThatIsNotADirectory(t *testing.T) {
	tests := map[string]*workload.PathState{
		"a directory": nil,
		"a link":      {Mode: fs.ModeSymlink | 0o777, LinkTarget: "."},
		"a file":      {Mode: 0o600},
	}
	for name, planted := range tests {
		t.Run(name, func(t *testing.T) {
			h := newHost(t)
			r := h.start()
			runtime, _, adapter := h.serving()
			assert.Equal(t, []workload.VolumeMount{{Volume: runtime.spec.Volumes[0].Volume, Target: "/outbox", Subpath: "outbox"}},
				adapter.spec.Volumes, "the adapter mounts the outbox alone")
			memory := runtime.spec.Volumes[0].Volume
			h.engine.mu.Lock()
			require.True(t, h.engine.paths[memory]["outbox"].Mode.IsDir(), "the first start made the outbox a directory")
			if planted != nil {
				h.engine.paths[memory]["outbox"] = *planted
			}
			h.engine.mu.Unlock()
			h.engine.exit(runtime)
			require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)

			next := h.start()
			if planted == nil {
				h.serving()
				return
			}
			require.ErrorIs(t, next.result(t), workload.ErrOutboxReplaced)
			assert.Zero(t, h.count(workload.RoleAdapter), "no adapter is created")
			h.engine.mu.Lock()
			defer h.engine.mu.Unlock()
			assert.Equal(t, *planted, h.engine.paths[memory]["outbox"], "the path is not repaired")
		})
	}
}
