package workload_test

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/workload"
)

func moveTo(name string) func(*workload.Desired) {
	return func(d *workload.Desired) { d.MoveMemoryTo = name }
}

// record is the supervisor's durable record, as far as a move reads it.
type record struct {
	Memory struct {
		Name       string `json:"name"`
		Nonce      string `json:"nonce"`
		Served     bool   `json:"served"`
		WriterExit *int   `json:"writerExit"`
	} `json:"memory"`
	Move *struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	} `json:"move"`
}

func (h *host) record() record {
	h.t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.dir, "workload.json"))
	require.NoError(h.t, err)
	var r record
	require.NoError(h.t, json.Unmarshal(raw, &r))
	return r
}

func (g *stubGaram) requested() []workload.ActivationRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]workload.ActivationRequest(nil), g.requests...)
}

// messageState answers what the runtime says of a message it was
// given, read from inside the adapter.
func (h *host) messageState(adapter, id string) string {
	out, err := h.docker("exec", adapter, h.probe(adapter), "get",
		"http://127.0.0.1:8080/agents/"+testAgent.String()+"/messages/"+id)
	if err != nil {
		return ""
	}
	var m struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal([]byte(out), &m)
	return m.Status
}

// turn gives the runtime a message and waits until it completed it, so
// its store holds the message as a source and its completion.
func (h *host) turn(id string) {
	h.t.Helper()
	adapter := h.container(workload.RoleAdapter)
	posted := h.mustDocker("exec", adapter, h.probe(adapter), "post",
		"http://127.0.0.1:8080/agents/"+testAgent.String()+"/messages",
		fmt.Sprintf(`{"id":%q,"content":"remember this"}`, id))
	require.Equal(h.t, "202", posted)
	eventually(h.t, "the turn completed", func() bool { return h.messageState(adapter, id) == "completed" })
}

// servedWithAStore runs a generation until its runtime served and took
// a turn, then stops the supervisor, leaving the generation running,
// and answers its memory volume.
func (h *host) servedWithAStore() string {
	h.t.Helper()
	first := h.start()
	h.activated(1)
	eventually(h.t, "the serving report recorded", func() bool { return h.record().Memory.Served })
	h.turn("garam-sa-before-move")
	require.NoError(h.t, first.stop())
	return h.volume("memory")
}

// contents answers every entry of a volume, read through a container
// that mounts it read-only and never runs, as its mode, owner,
// modification time and content digest by path.
func (h *host) contents(volume string) map[string]string {
	h.t.Helper()
	name := h.prefix + "reader-" + hex.EncodeToString(randomBytes(3))
	h.mustDocker("create", "--name", name, "--network", "none",
		"--mount", "type=volume,src="+volume+",dst=/v,readonly", h.images.Workspace, "true")
	defer func() { _, _ = h.docker("rm", name) }()
	cmd := exec.Command("docker", "cp", name+":/v", "-")
	cmd.Env = append(os.Environ(), "DOCKER_HOST="+h.daemon)
	raw, err := cmd.Output()
	require.NoError(h.t, err)
	out := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(h.t, err)
		sum := sha256.New()
		_, err = io.Copy(sum, tr)
		require.NoError(h.t, err)
		out[hdr.Name] = fmt.Sprintf("%s %d:%d %s %x", hdr.FileInfo().Mode(), hdr.Uid, hdr.Gid,
			hdr.ModTime.UTC().Format(time.RFC3339Nano), sum.Sum(nil))
	}
}

// inVolume runs the probe as the runtime's account in a container of
// its own that mounts volume at /data: the test's own act on a volume,
// as a fault in storage would act.
func (h *host) inVolume(volume string, args ...string) {
	h.t.Helper()
	name := h.prefix + "fault-" + hex.EncodeToString(randomBytes(3))
	h.mustDocker(append([]string{"create", "--name", name, "--user", "65532:65532", "--network", "none",
		"--mount", "type=volume,src=" + volume + ",dst=/data", "--entrypoint", "/probe", h.images.Workspace}, args...)...)
	defer func() { _, _ = h.docker("rm", name) }()
	path, err := probeBinary()
	require.NoError(h.t, err)
	h.mustDocker("cp", path, name+":/probe")
	h.mustDocker("start", "-a", name)
}

// volumeExists reports whether the daemon holds a volume of that name.
func (h *host) volumeExists(name string) bool {
	h.t.Helper()
	return h.mustDocker("volume", "ls", "-q", "--filter", "name=^"+name+"$") != ""
}

// mountedAt answers the volume a container mounts at target.
func (h *host) mountedAt(id, target string) string {
	h.t.Helper()
	return h.inspect(id, `{{range .Mounts}}{{if eq .Destination "`+target+`"}}{{.Name}}{{end}}{{end}}`)
}

// faultingEngine is the real engine with the test's own act at one
// point of a move: beforeCopy as the copy's helper starts, afterCopy
// once it has exited and before the supervisor reads the copy, and
// beforeRuntime as a runtime is created, after the copy verified. None
// of these is the supervisor's behaviour.
type faultingEngine struct {
	workload.Engine
	mu            sync.Mutex
	copier        string
	beforeCopy    func()
	afterCopy     func()
	beforeRuntime func(spec workload.ContainerSpec)
}

func (e *faultingEngine) CreateContainer(ctx context.Context, spec workload.ContainerSpec) (string, error) {
	if strings.Contains(spec.Name, "runtime-") && e.beforeRuntime != nil {
		e.beforeRuntime(spec)
	}
	id, err := e.Engine.CreateContainer(ctx, spec)
	if err == nil && spec.Cmd[0] == workload.CopyCommand {
		e.mu.Lock()
		e.copier = id
		e.mu.Unlock()
	}
	return id, err
}

func (e *faultingEngine) isCopier(id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return id == e.copier
}

func (e *faultingEngine) StartContainer(ctx context.Context, id string) error {
	if e.isCopier(id) && e.beforeCopy != nil {
		e.beforeCopy()
	}
	return e.Engine.StartContainer(ctx, id)
}

func (e *faultingEngine) WaitExited(ctx context.Context, id string) error {
	err := e.Engine.WaitExited(ctx, id)
	if err == nil && e.isCopier(id) && e.afterCopy != nil {
		e.afterCopy()
	}
	return err
}

// TestRealEngine_MovesTheStoreToAVerifiedCopy holds the handover end to
// end on the real engine (#1202): the running generation is drained,
// its store copied and verified, and a new generation of the same agent
// runs on the new volume with --require-store, knowing the message the
// old one completed, its activation naming the old one as replaced.
// The source is byte-identical afterwards, read entry by entry with
// times, owners and modes. The control is the new runtime's own turn,
// which changes its volume and not the source.
func TestRealEngine_MovesTheStoreToAVerifiedCopy(t *testing.T) {
	h := newHost(t)
	source := h.servedWithAStore()
	old := h.container(workload.RoleRuntime)
	target := h.prefix + "moved"
	var before map[string]string
	engine := &faultingEngine{Engine: h.engine, beforeCopy: func() { before = h.contents(source) }}

	h.startOn(engine, moveTo(target))
	h.activated(2)
	runtime, adapter := h.container(workload.RoleRuntime), h.container(workload.RoleAdapter)
	require.NotEqual(t, old, runtime)
	assert.Equal(t, target, h.mountedAt(runtime, "/data"), "the new generation runs on the new volume")
	cmd := h.inspect(runtime, "{{json .Config.Cmd}}")
	assert.Contains(t, cmd, "--require-store", "and requires the store it was given")
	assert.Contains(t, cmd, `"--agent-id","`+testAgent.String()+`"`, "as the same agent")
	asked := h.garam.requested()
	assert.NotEqual(t, asked[0].Generation, asked[len(asked)-1].Generation, "a new generation")
	assert.Equal(t, "activation-"+asked[0].RequestID, asked[len(asked)-1].Replaces,
		"its activation replaces the old generation's")
	nonce := h.inspect(target, `{{index .Labels "sh.garam.workload.nonce"}}`)
	assert.NotEmpty(t, nonce, "the move created the new volume")
	assert.Equal(t, nonce, h.record().Memory.Nonce)
	assert.NotEqual(t, source, h.mountedAt(runtime, "/data"), "the runtime mounts another volume than the old one")
	assert.NotEqual(t, h.inspect(source, `{{index .Labels "sh.garam.workload.nonce"}}`), h.record().Memory.Nonce,
		"and the record names a volume of another nonce")
	assert.Equal(t, "completed", h.messageState(adapter, "garam-sa-before-move"),
		"the store came across: the new runtime knows what the old one completed")
	assert.Equal(t, "unknown", h.messageState(adapter, "garam-sa-never-sent"), "the control: an id it never had")

	require.NotNil(t, before, "the source was read before the copy started")
	h.turn("garam-sa-after-move")
	assert.Equal(t, before, h.contents(source), "the source is byte-identical")
	copied := h.contents(target)
	changed := false
	for name, v := range copied {
		if strings.HasPrefix(name, "v/memory.db") && before[name] != v {
			changed = true
		}
	}
	assert.True(t, changed, "the control: the new runtime's turn changed its own store")
	assert.Empty(t, h.mustDocker("ps", "-aq", "--filter", "volume="+source), "nothing mounts the source")
}

// TestRealEngine_RefusesACopyCorruptedBeforeItIsVerified holds the
// digest (#1202): a byte of the copied store flipped between the copy
// and its verification refuses the move; nothing runs on the copy,
// nothing is activated, the move is recorded refused, the record stays
// on the old volume, which is unchanged, and the copy is kept. The next
// run starts on the old volume. The control is the same move with the
// copy left as written.
func TestRealEngine_RefusesACopyCorruptedBeforeItIsVerified(t *testing.T) {
	for _, corrupted := range []bool{false, true} {
		t.Run(fmt.Sprintf("corrupted %v", corrupted), func(t *testing.T) {
			h := newHost(t)
			source := h.servedWithAStore()
			target := h.prefix + "moved"
			var before map[string]string
			engine := &faultingEngine{Engine: h.engine, beforeCopy: func() { before = h.contents(source) }}
			if corrupted {
				engine.afterCopy = func() { h.inVolume(target, "flip", "/data/memory.db") }
			}
			r := h.startOn(engine, moveTo(target))
			if !corrupted {
				h.activated(2)
				assert.Equal(t, target, h.mountedAt(h.container(workload.RoleRuntime), "/data"))
				return
			}
			err := r.result(t)
			require.ErrorIs(t, err, workload.ErrMoveRefused)
			assert.ErrorContains(t, err, "does not match")
			assert.Empty(t, h.container(workload.RoleRuntime), "nothing runs on the copy")
			assert.Len(t, h.garam.generations(), 1, "nothing is activated")
			assert.Equal(t, source, h.record().Memory.Name, "the agent stays on the old volume")
			require.NotNil(t, h.record().Move)
			assert.Equal(t, "refused", h.record().Move.Outcome, "the move is settled refused")
			assert.Equal(t, before, h.contents(source), "the old volume is unchanged")
			assert.True(t, h.volumeExists(target), "the refused copy is kept")

			h.start()
			h.activated(2)
			assert.Equal(t, source, h.mountedAt(h.container(workload.RoleRuntime), "/data"),
				"the next run starts on the old volume")
		})
	}
}

// TestRealEngine_KeepsTheMovePendingWhileTheOldGenerationRuns holds the
// drain before the copy (#1202): a workspace stopped from the host,
// the test's own act, cannot act on the drain's SIGTERM, so the old
// generation does not exit and the move stays pending, with no volume
// created, nothing copied and nothing activated. The control is the
// same move completing once the workspace runs again.
func TestRealEngine_KeepsTheMovePendingWhileTheOldGenerationRuns(t *testing.T) {
	h := newHost(t)
	h.servedWithAStore()
	runtime, space := h.container(workload.RoleRuntime), h.container(workload.RoleWorkspace)
	resume := h.stopWorkspace(space)
	target := h.prefix + "moved"

	since := time.Now()
	pending := h.start(moveTo(target))
	eventually(t, "the drain signalling the workspace", func() bool {
		return len(h.signals(space, since)) > 0
	})
	time.Sleep(5 * time.Second)
	assert.Equal(t, "true", h.inspect(space, "{{.State.Running}}"), "the old workspace has not exited")
	assert.Equal(t, []string{"15"}, h.signals(space, since), "it was sent SIGTERM and nothing else")
	assert.False(t, pending.returned(), "the move is pending")
	assert.False(t, h.volumeExists(target), "no volume is created while the old generation runs")
	assert.Empty(t, h.mustDocker("ps", "-aq", "--filter", "name=^"+h.prefix+"copier-"), "nothing is copied")
	assert.Len(t, h.garam.generations(), 1, "nothing is activated")

	resume()
	h.activated(2)
	successor := h.container(workload.RoleRuntime)
	require.NotEqual(t, runtime, successor)
	assert.Equal(t, target, h.mountedAt(successor, "/data"), "the control: once it exits, the move completes")
}

// TestRealEngine_RefusesAWriterNotPositivelyStopped holds the move to a
// writer that exited 0 after its drain and whose lock is free (#1202):
// a runtime killed rather than drained refuses before anything of the
// move exists, and a store whose writer lock a leftover holder keeps
// refuses the copy with the helper's own reason. The control is a
// runtime the move drains itself.
func TestRealEngine_RefusesAWriterNotPositivelyStopped(t *testing.T) {
	t.Run("drained", func(t *testing.T) {
		h := newHost(t)
		h.servedWithAStore()
		h.start(moveTo(h.prefix + "moved"))
		h.activated(2)
		assert.Equal(t, h.prefix+"moved", h.mountedAt(h.container(workload.RoleRuntime), "/data"))
	})
	t.Run("killed", func(t *testing.T) {
		h := newHost(t)
		source := h.servedWithAStore()
		runtime := h.container(workload.RoleRuntime)
		h.mustDocker("kill", "--signal", "KILL", runtime)
		h.mustDocker("wait", runtime)

		err := h.start(moveTo(h.prefix + "moved")).result(t)
		require.ErrorIs(t, err, workload.ErrMoveRefused)
		assert.ErrorContains(t, err, "exited 137")
		assert.False(t, h.volumeExists(h.prefix+"moved"), "nothing of the move was created")
		assert.Equal(t, source, h.record().Memory.Name)
	})
	t.Run("lock held", func(t *testing.T) {
		h := newHost(t)
		source := h.servedWithAStore()
		r := h.start()
		h.mustDocker("kill", "--signal", "TERM", h.container(workload.RoleRuntime))
		require.ErrorIs(t, r.result(t), workload.ErrGenerationEnded)
		require.NotNil(t, h.record().Memory.WriterExit)
		require.Zero(t, *h.record().Memory.WriterExit, "the runtime exited 0 after its drain")
		holder := h.prefix + "lock-holder"
		h.mustDocker("run", "-d", "--name", holder, "--user", "65532:65532", "--network", "none",
			"--mount", "type=volume,src="+source+",dst=/data",
			"--entrypoint", "flock", h.images.Workspace, "--nonblock", "/data/memory.db.lock", "sleep", "3600")
		require.Equal(t, "true", h.inspect(holder, "{{.State.Running}}"), "the holder took the lock")

		err := h.start(moveTo(h.prefix + "moved")).result(t)
		require.ErrorIs(t, err, workload.ErrMoveRefused)
		assert.ErrorContains(t, err, "writer lock is held", "the helper's own reason is stated")
		assert.Empty(t, h.container(workload.RoleRuntime), "nothing runs")
		assert.Equal(t, source, h.record().Memory.Name)
	})
}

// TestRealEngine_RefusesACopyItsRuntimeDoesNotOpen holds the runtime's
// acceptance under --require-store (#1202): with the memory file gone
// from a copy after it verified, the runtime on it exits before it
// serves, nothing is activated, and the record returns to the old
// volume, where the next run starts. The control is the same copy left
// whole, which is activated.
func TestRealEngine_RefusesACopyItsRuntimeDoesNotOpen(t *testing.T) {
	for _, removed := range []bool{false, true} {
		t.Run(fmt.Sprintf("removed %v", removed), func(t *testing.T) {
			h := newHost(t)
			source := h.servedWithAStore()
			target := h.prefix + "moved"
			engine := &faultingEngine{Engine: h.engine}
			if removed {
				engine.beforeRuntime = func(spec workload.ContainerSpec) {
					if len(spec.Volumes) == 1 && spec.Volumes[0].Volume == target {
						h.inVolume(target, "remove", "/data/memory.db")
						engine.beforeRuntime = nil
					}
				}
			}
			r := h.startOn(engine, moveTo(target))
			if !removed {
				h.activated(2)
				return
			}
			err := r.result(t)
			require.ErrorIs(t, err, workload.ErrMoveRefused)
			assert.ErrorContains(t, err, "--require-store")
			assert.Len(t, h.garam.generations(), 1, "nothing is activated")
			assert.Equal(t, source, h.record().Memory.Name, "the record is back on the old volume")
			assert.Equal(t, "refused", h.record().Move.Outcome)

			h.start()
			h.activated(2)
			assert.Equal(t, source, h.mountedAt(h.container(workload.RoleRuntime), "/data"))
		})
	}
}

// TestRealEngine_KeepsTheMovePendingWhileTheRuntimeDoesNotExit holds
// the drain on the writer itself (#1202): a runtime stopped from the
// host, the test's own act, cannot act on SIGTERM, so for thirty
// seconds the move stays pending, no signal but that one SIGTERM
// reaches any container of the generation, the workspace and the
// adapter are not yet told to stop, and nothing of the move exists.
// The control is the same move completing once the runtime runs again,
// without its having been killed.
func TestRealEngine_KeepsTheMovePendingWhileTheRuntimeDoesNotExit(t *testing.T) {
	h := newHost(t)
	h.servedWithAStore()
	runtime, space, adapter := h.container(workload.RoleRuntime), h.container(workload.RoleWorkspace),
		h.container(workload.RoleAdapter)
	// stopWorkspace stops any container's first process from the host.
	resume := h.stopWorkspace(runtime)
	target := h.prefix + "moved"

	since := time.Now()
	signals := h.watchSignals(since, runtime, space, adapter)
	pending := h.start(moveTo(target))
	eventually(t, "the drain signalling the runtime", func() bool { return len(signals.of(runtime)) > 0 })
	time.Sleep(30 * time.Second)
	assert.Equal(t, "true", h.inspect(runtime, "{{.State.Running}}"), "the runtime has not exited")
	assert.Equal(t, []string{"15"}, signals.of(runtime), "it was sent SIGTERM and nothing else")
	assert.Empty(t, signals.of(space), "the workspace is not told to stop before the runtime exits")
	assert.Empty(t, signals.of(adapter), "nor is the adapter")
	assert.False(t, pending.returned(), "the move is pending")
	assert.False(t, h.volumeExists(target), "no volume is created while the runtime runs")
	assert.Empty(t, h.mustDocker("ps", "-aq", "--filter", "name=^"+h.prefix+"copier-"), "nothing is copied")
	assert.Len(t, h.garam.generations(), 1, "nothing is activated")

	resume()
	h.activated(2)
	assert.Equal(t, target, h.mountedAt(h.container(workload.RoleRuntime), "/data"),
		"the control: once it exits, the move completes")
	assert.Equal(t, []string{"15"}, signals.of(runtime), "still SIGTERM alone")
	assert.Equal(t, []string{"0"}, h.exitCodes(runtime, since), "and it exited after its drain")
}

// TestRealEngine_HoldsEveryRunToAnUnsettledMove holds a move whose copy
// verified and whose generation is not yet activated, garam being out
// of reach: a run with no move asked for, or another volume, refuses
// rather than start on either volume, and creates nothing. The control
// is the same move asked for again, which adopts its generation and
// activates it once garam answers.
func TestRealEngine_HoldsEveryRunToAnUnsettledMove(t *testing.T) {
	h := newHost(t)
	h.servedWithAStore()
	target := h.prefix + "moved"
	h.garam.setUndecided(true)
	r := h.start(moveTo(target))
	var runtime string
	eventually(t, "the generation on the copy running", func() bool {
		runtime = h.container(workload.RoleRuntime)
		// The old runtime may be removed between the listing and this
		// read, so a failed read is a no.
		mounted, err := h.docker("inspect", "-f", `{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}`, runtime)
		return runtime != "" && err == nil && mounted == target
	})
	require.NoError(t, r.stop())

	require.ErrorIs(t, h.start().result(t), workload.ErrMovePending)
	elsewhere := h.prefix + "elsewhere"
	require.ErrorIs(t, h.start(moveTo(elsewhere)).result(t), workload.ErrMovePending)
	assert.False(t, h.volumeExists(elsewhere), "nothing is created for another move")
	assert.Equal(t, runtime, h.container(workload.RoleRuntime), "the generation on the copy runs on")

	h.start(moveTo(target))
	h.garam.setUndecided(false)
	h.activated(2)
	assert.Equal(t, runtime, h.container(workload.RoleRuntime), "the control: the same move adopts its generation")
}

// signalWatch is every signal the engine delivered to some containers
// from a moment on, read from one `docker events` stream the test
// holds open: a busy daemon trims the history a later query would read.
type signalWatch struct {
	mu   sync.Mutex
	seen map[string][]string
}

func (h *host) watchSignals(since time.Time, ids ...string) *signalWatch {
	h.t.Helper()
	args := []string{"events", "--since", since.Format(time.RFC3339Nano), "--filter", "event=kill",
		"--format", "{{.Actor.ID}} {{.Actor.Attributes.signal}}"}
	for _, id := range ids {
		args = append(args, "--filter", "container="+id)
	}
	cmd := exec.Command("docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_HOST="+h.daemon)
	out, err := cmd.StdoutPipe()
	require.NoError(h.t, err)
	require.NoError(h.t, cmd.Start())
	h.t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	w := &signalWatch{seen: map[string][]string{}}
	go func() {
		lines := bufio.NewScanner(out)
		for lines.Scan() {
			if f := strings.Fields(lines.Text()); len(f) == 2 {
				w.mu.Lock()
				w.seen[f[0]] = append(w.seen[f[0]], f[1])
				w.mu.Unlock()
			}
		}
	}()
	return w
}

// of answers the signals delivered to a container, named by its ID or
// a prefix of it.
func (w *signalWatch) of(id string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := []string{}
	for full, signals := range w.seen {
		if strings.HasPrefix(full, id) {
			out = append(out, signals...)
		}
	}
	return out
}
