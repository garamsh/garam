package workload_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/workload"
)

const movedVolume = "garam-test-moved"

func moveTo(name string) func(*workload.Desired) {
	return func(d *workload.Desired) { d.MoveMemoryTo = name }
}

// served runs a generation until its runtime served, with a store and
// an outbox entry on its memory volume, stops the supervisor, and
// answers the runtime and that volume.
func (h *host) served() (*stubContainer, string) {
	h.t.Helper()
	r := h.start()
	runtime, _, adapter := h.serving()
	h.serve(adapter)
	memory := runtime.spec.Volumes[0].Volume
	h.engine.write(memory, "memory.db", "what the agent remembers")
	h.engine.write(memory, "memory.db.lock", "")
	h.engine.write(memory, "outbox/reply.json", `{"id":"reply"}`)
	require.NoError(h.t, r.stop())
	return runtime, memory
}

// successor waits for a generation whose runtime is not prev, and
// answers its runtime and adapter.
func (h *host) successor(prev *stubContainer) (runtime, adapter *stubContainer) {
	h.t.Helper()
	require.NoError(h.t, h.engine.await(func() bool {
		runtime, adapter = h.live(workload.RoleRuntime), h.live(workload.RoleAdapter)
		return runtime != nil && runtime != prev && adapter != nil && adapter.spec.NetworkOf == runtime.id
	}))
	return runtime, adapter
}

// copiers answers the spec of every copy helper created.
func (h *host) copiers() []workload.ContainerSpec {
	h.engine.mu.Lock()
	defer h.engine.mu.Unlock()
	var out []workload.ContainerSpec
	for _, c := range h.engine.containers {
		if len(c.spec.Cmd) > 0 && c.spec.Cmd[0] == workload.CopyCommand {
			out = append(out, c.spec)
		}
	}
	return out
}

func (h *host) volumeExists(name string) bool {
	h.engine.mu.Lock()
	defer h.engine.mu.Unlock()
	_, ok := h.engine.volumes[name]
	return ok
}

// TestMove_RunsTheNextGenerationOnAVerifiedCopy holds the handover's
// order and outcome: the old generation is drained, a helper copies
// the store from the source mounted read-only into a volume the move
// created, and the next generation runs on that copy with
// --require-store, its activation naming the old one as replaced. The
// source is unchanged, and the copy holds its state and not its lock.
func TestMove_RunsTheNextGenerationOnAVerifiedCopy(t *testing.T) {
	h := newHost(t)
	old, source := h.served()
	before := h.engine.snapshot(source)
	var copier workload.ContainerSpec
	h.engine.beforeCreate = func(spec workload.ContainerSpec) {
		if spec.Cmd[0] == workload.CopyCommand {
			copier = spec
			assert.Equal(t, workload.ContainerExited, old.status, "the old runtime exited before the copy")
		}
	}

	h.start(moveTo(movedVolume))
	runtime, adapter := h.successor(old)
	assert.Equal(t, []workload.VolumeMount{{Volume: movedVolume, Target: "/data"}}, runtime.spec.Volumes)
	assert.Contains(t, runtime.spec.Cmd, "--require-store", "the copy is accepted only by a runtime that requires it")
	assert.Equal(t, []workload.VolumeMount{
		{Volume: source, Target: "/source", ReadOnly: true}, {Volume: movedVolume, Target: "/target"},
	}, copier.Volumes, "the source is mounted read-only")
	assert.True(t, copier.NoNetwork)
	assert.Equal(t, "65532:65532", copier.User)
	assert.Equal(t, adapterImage, copier.Image)
	assert.Empty(t, h.copiers(), "the helper is removed once the copy verified")
	h.engine.mu.Lock()
	assert.NotEmpty(t, h.engine.volumes[movedVolume]["sh.garam.workload.nonce"], "the move created the volume")
	h.engine.mu.Unlock()

	copied := h.engine.snapshot(movedVolume)
	for _, rel := range []string{"memory.db", "outbox", "outbox/reply.json"} {
		assert.Equal(t, before[rel], copied[rel], rel)
	}
	assert.NotContains(t, copied, "memory.db.lock", "the lock is not state")
	assert.Equal(t, before, h.engine.snapshot(source), "the source is unchanged")

	first := h.garam.asked()[0]
	h.activateOnce(adapter)
	asked := h.garam.asked()
	assert.Equal(t, "activation-"+first.RequestID, asked[len(asked)-1].Replaces,
		"the new generation replaces the old one's activation")
}

// TestMove_RefusesACopyThatDoesNotMatchItsSource holds the digest: a
// copy changed after the helper wrote it refuses the move, no runtime
// starts on it, and the next run starts on the source; the refused
// volume is not used again. The control is the same move with the copy
// left as written, which starts the next generation on it.
func TestMove_RefusesACopyThatDoesNotMatchItsSource(t *testing.T) {
	for _, corrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "as written", true: "corrupted"}[corrupted], func(t *testing.T) {
			h := newHost(t)
			old, source := h.served()
			h.engine.afterStart = func(c *stubContainer) {
				if corrupted && c.spec.Cmd[0] == workload.CopyCommand {
					h.engine.data[movedVolume]["memory.db"] = []byte("what the agent remembers, changed")
				}
			}
			r := h.start(moveTo(movedVolume))
			if !corrupted {
				runtime, _ := h.successor(old)
				assert.Equal(t, movedVolume, runtime.spec.Volumes[0].Volume)
				return
			}
			err := r.result(t)
			require.ErrorIs(t, err, workload.ErrMoveRefused)
			assert.ErrorContains(t, err, "does not match")
			assert.Zero(t, h.count(workload.RoleRuntime), "nothing runs on the copy")
			assert.Len(t, h.garam.asked(), 1, "nothing is activated")
			assert.True(t, h.volumeExists(movedVolume), "the refused copy is kept")

			require.ErrorIs(t, h.start(moveTo(movedVolume)).result(t), workload.ErrMoveRefused,
				"a refused move's volume is not used again")
			h.engine.afterStart = nil
			h.start()
			runtime, _ := h.successor(old)
			assert.Equal(t, source, runtime.spec.Volumes[0].Volume, "the agent stays on the old volume")
		})
	}
}

// TestMove_RefusesAWriterNotPositivelyStopped holds the move to a
// writer that exited 0 after its drain and left its lock free: a
// runtime killed rather than drained refuses before anything of the
// move exists, and a lock still held refuses the copy. The control is
// a runtime drained by the move, whose lock is free.
func TestMove_RefusesAWriterNotPositivelyStopped(t *testing.T) {
	t.Run("drained", func(t *testing.T) {
		h := newHost(t)
		old, _ := h.served()
		h.start(moveTo(movedVolume))
		runtime, _ := h.successor(old)
		assert.Equal(t, movedVolume, runtime.spec.Volumes[0].Volume)
	})
	t.Run("killed", func(t *testing.T) {
		h := newHost(t)
		old, _ := h.served()
		// Killed while no supervisor ran: the move's own run drains it.
		h.engine.mu.Lock()
		old.exitCode = 137
		h.engine.mu.Unlock()
		h.engine.exit(old)

		err := h.start(moveTo(movedVolume)).result(t)
		require.ErrorIs(t, err, workload.ErrMoveRefused)
		assert.ErrorContains(t, err, "exited 137")
		assert.False(t, h.volumeExists(movedVolume), "nothing of the move was created")
		assert.Empty(t, h.copiers())
	})
	t.Run("lock held", func(t *testing.T) {
		h := newHost(t)
		_, source := h.served()
		h.engine.write(source, "memory.db.lock.held", "")
		err := h.start(moveTo(movedVolume)).result(t)
		require.ErrorIs(t, err, workload.ErrMoveRefused)
		assert.ErrorContains(t, err, "writer lock is held", "the helper's own reason is stated")
		assert.Zero(t, h.count(workload.RoleRuntime))
	})
}

// TestMove_WaitsForTheOldGenerationToExit holds the drain before the
// copy: a runtime that does not exit on SIGTERM keeps the move pending,
// with no volume created and nothing copied. The control is the same
// runtime exiting, after which the move completes.
func TestMove_WaitsForTheOldGenerationToExit(t *testing.T) {
	h := newHost(t)
	old, _ := h.served()
	h.engine.mu.Lock()
	old.ignoresTerm = true
	h.engine.mu.Unlock()
	r := h.start(moveTo(movedVolume))
	require.NoError(t, h.engine.await(func() bool {
		for _, s := range h.engine.signals {
			if s == old.spec.Name+":SIGTERM" {
				return true
			}
		}
		return false
	}))
	assert.False(t, r.returned(), "the move is pending")
	assert.False(t, h.volumeExists(movedVolume), "no volume is created while the writer runs")
	assert.Empty(t, h.copiers(), "nothing is copied while the writer runs")

	h.engine.exit(old)
	runtime, _ := h.successor(old)
	assert.Equal(t, movedVolume, runtime.spec.Volumes[0].Volume, "the control: once it exits, the move completes")
}

// TestMove_IsAcceptedByTheFirstActivation holds acceptance to the
// runtime on the copy serving: one that ends before its adapter asks
// for an activation refuses the move, and the next run starts on the
// source. The control is the same runtime activated first, after which
// its end keeps the agent on the copy.
func TestMove_IsAcceptedByTheFirstActivation(t *testing.T) {
	for _, activated := range []bool{false, true} {
		t.Run(map[bool]string{false: "ended first", true: "activated"}[activated], func(t *testing.T) {
			h := newHost(t)
			old, source := h.served()
			r := h.start(moveTo(movedVolume))
			runtime, adapter := h.successor(old)
			if activated {
				h.activateOnce(adapter)
			}
			h.engine.exit(runtime)
			if !activated {
				err := r.result(t)
				require.ErrorIs(t, err, workload.ErrMoveRefused)
				assert.ErrorContains(t, err, "--require-store")
			} else {
				require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)
			}
			h.start()
			next, _ := h.successor(runtime)
			want := map[bool]string{false: source, true: movedVolume}[activated]
			assert.Equal(t, want, next.spec.Volumes[0].Volume)
			assert.Contains(t, next.spec.Cmd, "--require-store")
		})
	}
}

// TestRun_HoldsEveryRunToAnUnsettledMove holds a move that is neither
// accepted nor refused against a run asking for something else: with
// no move asked for, or another volume, the run refuses. The control
// is the same move asked for again, which adopts its generation.
func TestRun_HoldsEveryRunToAnUnsettledMove(t *testing.T) {
	h := newHost(t)
	old, _ := h.served()
	r := h.start(moveTo(movedVolume))
	runtime, _ := h.successor(old)
	require.NoError(t, r.stop())

	require.ErrorIs(t, h.start().result(t), workload.ErrMovePending)
	require.ErrorIs(t, h.start(moveTo("garam-test-elsewhere")).result(t), workload.ErrMovePending)
	assert.False(t, h.volumeExists("garam-test-elsewhere"))

	again := h.start(moveTo(movedVolume))
	adopted, _ := h.successor(old)
	assert.Same(t, runtime, adopted, "the control: the same move adopts its generation")
	assert.False(t, again.returned())
}
