package workload_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/delivery"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/workload"
)

// stubStatus is an attached runtime's status route as the supervisor
// reads it: one answer, or one error, replaced as the runtime changes.
type stubStatus struct {
	mu     sync.Mutex
	status *workload.RuntimeStatus
	err    error
	reads  int
}

func (s *stubStatus) Status(context.Context) (*workload.RuntimeStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.err != nil {
		return nil, s.err
	}
	answer := *s.status
	return &answer, nil
}

// serving makes the runtime report generation serving at the host's
// epoch and revision.
func (s *stubStatus) serving(generation string) {
	s.answer(&workload.RuntimeStatus{Epoch: "3", Generation: generation, Revision: "r1", State: "serving"}, nil)
}

func (s *stubStatus) answer(status *workload.RuntimeStatus, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status, s.err = status, err
}

// readCount answers how many reads the supervisor has made.
func (s *stubStatus) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

// adapterDir is where an attached runtime's adapter reads its files.
func (h *host) adapterDir() string { return filepath.Join(h.dir, "agent") }

// attachConfig is a direct host's configuration: no engine, no image,
// the token mode, and the runtime's status from status.
func (h *host) attachConfig(status workload.StatusReader, tokens workload.AgentTokens) workload.Config {
	h.t.Helper()
	require.NoError(h.t, os.MkdirAll(h.adapterDir(), 0o700))
	return workload.Config{
		Dir: h.dir, Prefix: "garam-test-",
		Desired: workload.Desired{Agent: testAgent, Epoch: "3", Config: []byte("revision: r1\n"), Revision: "r1"},
		Tokens:  tokens, TokenCheckInterval: 5 * time.Millisecond,
		Attach: &workload.Attachment{Status: status, AdapterDir: h.adapterDir(), StatusInterval: 5 * time.Millisecond},
	}
}

// runAttached runs an attaching supervisor in the background, as the
// latest run, and waits until its control serves.
func (h *host) runAttached(cfg workload.Config) *running {
	h.t.Helper()
	s, err := workload.NewSupervisor(nil, h.garam, cfg)
	require.NoError(h.t, err)
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		r.err = s.Run(ctx)
	}()
	h.t.Cleanup(func() { r.cancel(); <-r.done })
	h.latest = r
	socket := filepath.Join(h.adapterDir(), "control", "control.sock")
	require.Eventually(h.t, func() bool {
		_, err := os.Stat(socket)
		return err == nil || r.returned()
	}, 5*time.Second, 5*time.Millisecond)
	require.False(h.t, r.returned(), "the run returned before serving: %v", r.err)
	return r
}

// attachedAdapter is a direct host's adapter, built from the files the
// supervisor wrote into its directory: the root, the control token and
// the socket, and its placement token.
func (h *host) attachedAdapter() (*delivery.HTTPAuthority, string) {
	h.t.Helper()
	dir := h.adapterDir()
	rootPEM, err := os.ReadFile(filepath.Join(dir, "control-root.pem"))
	require.NoError(h.t, err)
	roots := x509.NewCertPool()
	require.True(h.t, roots.AppendCertsFromPEM(rootPEM))
	socket := filepath.Join(dir, "control", "control.sock")
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	control, err := delivery.NewTokenFile(filepath.Join(dir, "control-token"))
	require.NoError(h.t, err)
	auth, err := delivery.NewHTTPAuthority(client, "https://"+workload.ControlName, testAgent, control)
	require.NoError(h.t, err)
	placement, err := os.ReadFile(filepath.Join(dir, "placement-token"))
	require.NoError(h.t, err)
	return auth, string(placement)
}

// activateAttached has the attached adapter ask for generation and
// answers what came back.
func activateAttached(auth *delivery.HTTPAuthority, placement, generation string) (*delivery.Activation, error) {
	b := binding(generation)
	return auth.Activate(context.Background(), delivery.RequestID(delivery.TokenExecutionContract, b, placement), b, placement)
}

// TestActivate_AttachedForwardsOnlyTheGenerationTheRuntimeReportsServing
// holds the attach mode's provenance (ADR-0111): the supervisor reads
// the runtime's status itself and forwards only the generation the
// request names, serving at the intent's epoch and revision. Each
// mismatch is placement_not_current, and an answer it cannot read is
// undecided; none reaches garam. The generation the runtime does
// report is forwarded, and its retry is the same activation.
func TestActivate_AttachedForwardsOnlyTheGenerationTheRuntimeReportsServing(t *testing.T) {
	h := newHost(t)
	status := &stubStatus{}
	reported := newGeneration()
	status.serving(reported)
	h.runAttached(h.attachConfig(status, &stubTokens{current: agentToken("a")}))
	auth, placement := h.attachedAdapter()

	refusedAs := func(t *testing.T, generation, kind string) {
		t.Helper()
		_, err := activateAttached(auth, placement, generation)
		require.Error(t, err)
		assert.Contains(t, err.Error(), kind)
	}
	t.Run("a stale generation", func(t *testing.T) {
		refusedAs(t, newGeneration(), "placement_not_current")
	})
	for name, report := range map[string]workload.RuntimeStatus{
		"a runtime draining":            {Epoch: "3", Generation: reported, Revision: "r1", State: "draining"},
		"a runtime at another epoch":    {Epoch: "2", Generation: reported, Revision: "r1", State: "serving"},
		"a runtime on another revision": {Epoch: "3", Generation: reported, Revision: "r0", State: "serving"},
	} {
		t.Run(name, func(t *testing.T) {
			status.answer(&report, nil)
			refusedAs(t, reported, "placement_not_current")
		})
	}
	t.Run("a runtime serving another agent", func(t *testing.T) {
		status.answer(nil, errorx.New(errorx.KindFailedPrecondition, "the runtime serves another agent"))
		refusedAs(t, reported, "placement_not_current")
	})
	t.Run("a status that cannot be read", func(t *testing.T) {
		status.answer(nil, errorx.New(errorx.KindUnavailable, "connection refused"))
		_, err := activateAttached(auth, placement, reported)
		assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "undecided, which the adapter retries: %v", err)
	})
	assert.Empty(t, h.garam.asked(), "nothing refused reached garam")

	status.serving(reported)
	first, err := activateAttached(auth, placement, reported)
	require.NoError(t, err, "the control: the generation the runtime reports serving is forwarded")
	again, err := activateAttached(auth, placement, reported)
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID, "a retry is the same activation")
	asked := h.garam.asked()
	require.Len(t, asked, 2)
	assert.Equal(t, asked[0].RequestID, asked[1].RequestID, "a retry repeats the first request")
	assert.Equal(t, reported, asked[0].Generation)
}

// TestRun_AttachedHandsTheAdapterItsFilesAndStartsNothing holds what a
// direct host's adapter is given: the control's root, the placement
// token, the control token and the agent token, each readable by its
// owner alone, with the agent token rewritten when the supervisor holds
// a newer one. The supervisor holds no engine at all.
func TestRun_AttachedHandsTheAdapterItsFilesAndStartsNothing(t *testing.T) {
	h := newHost(t)
	status := &stubStatus{}
	status.serving(newGeneration())
	tokens := h.tokens
	h.runAttached(h.attachConfig(status, tokens))

	for _, name := range []string{"control-root.pem", "placement-token", "control-token", "agent-token"} {
		info, err := os.Stat(filepath.Join(h.adapterDir(), name))
		require.NoError(t, err, name)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), name)
	}
	token := func() string {
		raw, err := os.ReadFile(filepath.Join(h.adapterDir(), "agent-token"))
		require.NoError(t, err)
		return string(raw)
	}
	assert.Equal(t, agentToken("a"), token())
	tokens.set(agentToken("b"))
	require.Eventually(t, func() bool { return token() == agentToken("b") }, 5*time.Second, 5*time.Millisecond,
		"the newer agent token reaches the adapter")
	assert.Zero(t, h.count(workload.RoleRuntime)+h.count(workload.RoleWorkspace)+h.count(workload.RoleAdapter),
		"an attaching supervisor creates no container")
}

// TestRun_AttachedEndsOnlyWhenTheRuntimeReportsAnotherGeneration holds
// the attach mode's watch (ADR-0111). A status it cannot read decides
// nothing, and the forwarded generation read again resumes; another
// generation ends the intent. The next run opens a new intent, whose
// first activation names the one it replaces, so garam fences the
// generation that was.
func TestRun_AttachedEndsOnlyWhenTheRuntimeReportsAnotherGeneration(t *testing.T) {
	h := newHost(t)
	status := &stubStatus{}
	old := newGeneration()
	status.serving(old)
	tokens := h.tokens
	run := h.runAttached(h.attachConfig(status, tokens))
	auth, placement := h.attachedAdapter()
	first, err := activateAttached(auth, placement, old)
	require.NoError(t, err)

	status.answer(nil, errorx.New(errorx.KindUnavailable, "connection refused"))
	reads := status.readCount()
	require.Eventually(t, func() bool { return status.readCount() > reads+3 }, 5*time.Second, time.Millisecond)
	assert.False(t, run.returned(), "a status that cannot be read ends nothing")
	status.serving(old)
	reads = status.readCount()
	require.Eventually(t, func() bool { return status.readCount() > reads+3 }, 5*time.Second, time.Millisecond)
	assert.False(t, run.returned(), "the forwarded generation read again resumes")

	restarted := newGeneration()
	status.serving(restarted)
	require.ErrorIs(t, run.result(t), workload.ErrGenerationEnded, "another generation ends the intent")

	h.runAttached(h.attachConfig(status, tokens))
	auth, next := h.attachedAdapter()
	assert.NotEqual(t, placement, next, "the next intent draws its own placement token")
	_, err = activateAttached(auth, next, restarted)
	require.NoError(t, err)
	asked := h.garam.asked()
	require.Len(t, asked, 2)
	assert.Equal(t, restarted, asked[1].Generation)
	assert.Equal(t, first.ID, asked[1].Replaces, "the restarted generation's activation replaces the old one")
}

// TestRun_AttachedResumesItsIntentAcrossARestart holds a supervisor
// its init restarts while the runtime still serves: the record's intent
// is taken up again, so the adapter's placement token stands and its
// retry is the same activation.
func TestRun_AttachedResumesItsIntentAcrossARestart(t *testing.T) {
	h := newHost(t)
	status := &stubStatus{}
	generation := newGeneration()
	status.serving(generation)
	tokens := h.tokens
	run := h.runAttached(h.attachConfig(status, tokens))
	auth, placement := h.attachedAdapter()
	first, err := activateAttached(auth, placement, generation)
	require.NoError(t, err)
	require.NoError(t, run.stop(), "a requested stop is not a failure")

	h.runAttached(h.attachConfig(status, tokens))
	auth, again := h.attachedAdapter()
	assert.Equal(t, placement, again, "the intent is taken up again")
	retried, err := activateAttached(auth, again, generation)
	require.NoError(t, err)
	assert.Equal(t, first.ID, retried.ID)
}

// TestRun_AttachedRefusesAGenerationThisHostStarted holds an attaching
// supervisor off a record holding a generation it started as
// containers and never drained: attaching would leave that runtime
// beside the attached one. A host with no such record attaches.
func TestRun_AttachedRefusesAGenerationThisHostStarted(t *testing.T) {
	h := newHost(t)
	tokens := h.tokens
	h.start()
	h.serving()
	require.NoError(t, h.latest.stop())

	status := &stubStatus{}
	status.serving(newGeneration())
	s, err := workload.NewSupervisor(nil, h.garam, h.attachConfig(status, tokens))
	require.NoError(t, err)
	assert.ErrorIs(t, s.Run(context.Background()), workload.ErrStartedHere)

	fresh := newHost(t)
	fresh.runAttached(fresh.attachConfig(status, tokens))
	assert.False(t, fresh.latest.returned(), "the control: a host with no started generation attaches")
}

// TestNewSupervisor_AttachesWithNoEngineAndInTheTokenModeAlone holds
// the attach mode's configuration: it takes no engine, hands the
// adapter tokens, and neither moves memory nor renders a runtime
// configuration. An engine with no attachment, and an attachment with
// no engine, are each what NewSupervisor takes.
func TestNewSupervisor_AttachesWithNoEngineAndInTheTokenModeAlone(t *testing.T) {
	h := newHost(t)
	tokens := h.tokens
	status := &stubStatus{}
	refused := map[string]func() (workload.Engine, workload.Config){
		"an engine beside the attachment": func() (workload.Engine, workload.Config) {
			return h.engine, h.attachConfig(status, tokens)
		},
		"no engine and no attachment": func() (workload.Engine, workload.Config) {
			return nil, h.config()
		},
		"no agent tokens": func() (workload.Engine, workload.Config) {
			cfg := h.attachConfig(status, tokens)
			cfg.Tokens = nil
			return nil, cfg
		},
		"a move of the memory": func() (workload.Engine, workload.Config) {
			cfg := h.attachConfig(status, tokens)
			cfg.Desired.MoveMemoryTo = "garam-memory-2"
			return nil, cfg
		},
		"garam's message issuer": func() (workload.Engine, workload.Config) {
			cfg := h.attachConfig(status, tokens)
			cfg.MessageIssuer = testIssuer
			return nil, cfg
		},
		"no status to read": func() (workload.Engine, workload.Config) {
			cfg := h.attachConfig(status, tokens)
			cfg.Attach.Status = nil
			return nil, cfg
		},
	}
	for name, configured := range refused {
		t.Run(name, func(t *testing.T) {
			engine, cfg := configured()
			_, err := workload.NewSupervisor(engine, h.garam, cfg)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)
		})
	}
	_, err := workload.NewSupervisor(nil, h.garam, h.attachConfig(status, tokens))
	assert.NoError(t, err, "the control: an attachment, no engine, the token mode")
	_, err = workload.NewSupervisor(h.engine, h.garam, h.config())
	assert.NoError(t, err, "the control: an engine and no attachment")
}

// TestRun_SeedsTheLastActivationOnlyWhereTheRecordHoldsNone holds the
// anchor a replacement's enrollment answers: a supervisor that never
// activated names it on its first activation, and one that has
// activated names its own last activation and never the seed.
func TestRun_SeedsTheLastActivationOnlyWhereTheRecordHoldsNone(t *testing.T) {
	h := newHost(t)
	status := &stubStatus{}
	generation := newGeneration()
	status.serving(generation)
	tokens := h.tokens
	cfg := h.attachConfig(status, tokens)
	cfg.Replaces = "ended-by-the-replacement"
	run := h.runAttached(cfg)
	auth, placement := h.attachedAdapter()
	first, err := activateAttached(auth, placement, generation)
	require.NoError(t, err)
	require.NoError(t, run.stop())

	next := newGeneration()
	status.serving(next)
	cfg = h.attachConfig(status, tokens)
	cfg.Replaces = "a-seed-the-record-does-not-need"
	cfg.Desired.Replace = true
	h.runAttached(cfg)
	auth, placement = h.attachedAdapter()
	_, err = activateAttached(auth, placement, next)
	require.NoError(t, err)

	asked := h.garam.asked()
	require.Len(t, asked, 2)
	assert.Equal(t, "ended-by-the-replacement", asked[0].Replaces, "a supervisor that never activated names the seed")
	assert.Equal(t, first.ID, asked[1].Replaces, "one that activated names its own, never the seed")
}
