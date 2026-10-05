package workload_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/workload"
)

// activateOnce has the adapter of the generation running now activate
// it, which is what records that its runtime served.
func (h *host) activateOnce(adapter *stubContainer) string {
	h.t.Helper()
	auth, token := h.adapterOf(adapter)
	generation := newGeneration()
	_, err := auth.Activate(context.Background(), delivery.RequestID(binding(generation), token), binding(generation), token)
	require.NoError(h.t, err)
	return generation
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
	h.activateOnce(adapter)
	require.NoError(t, again.stop(), "the supervisor stops after its runtime served")

	again = h.start(replace)
	require.NoError(t, h.engine.await(func() bool { return successor.status == workload.ContainerExited }))
	third, _, adapter := h.serving()
	assert.Contains(t, third.spec.Cmd, "--require-store", "a start after a runtime served requires the store")

	h.activateOnce(adapter)
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
		return workspace.status == workload.ContainerExited && slices.Contains(h.engine.signals, runtime.spec.Name+":SIGTERM")
	}))
	assert.Equal(t, workload.ContainerRunning, runtime.status, "a runtime finishing its turn keeps running")
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
