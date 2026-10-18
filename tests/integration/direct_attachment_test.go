package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/workload"
)

// errStoreLocked is a runtime start its store's lock refused, as
// Sherlock's lockWriter refuses a second opener with ErrStoreLocked.
var errStoreLocked = errors.New("another process owns the memory store")

// directRuntime stands in for a Sherlock process on a direct host. It
// starts in the order Sherlock's serveAgent keeps at
// d63ac48957cea7214cdd4cfb06b45b1b859e3d7d: flock(LOCK_EX|LOCK_NB) on
// <store>.lock first, then a generation minted, and only then the
// listener bound. Its drain closes the listener and releases the lock
// last (Sherlock ADR 0022). A lock is one per open file, so a second
// runtime in this process is refused as a second process would be.
type directRuntime struct {
	agentID    string
	generation string
	lock       *os.File
	srv        *http.Server
	served     chan struct{}
	closeOnce  sync.Once
	unlockOnce sync.Once

	mu sync.Mutex
	// reportedID is the agent_id the status answers, which a runtime
	// answering for another agent sets apart from agentID.
	reportedID string
}

func startDirectRuntime(t *testing.T, store, address, agentID, epoch string) (*directRuntime, error) {
	t.Helper()
	lock, err := os.OpenFile(store+".lock", os.O_RDWR|os.O_CREATE|syscall.O_CLOEXEC, 0o600)
	require.NoError(t, err)
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		require.ErrorIs(t, err, syscall.EWOULDBLOCK)
		return nil, errStoreLocked
	}
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	r := &directRuntime{agentID: agentID, generation: hex.EncodeToString(nonce), lock: lock, reportedID: agentID,
		served: make(chan struct{})}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	started := time.Now().UTC().Format(time.RFC3339)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /agents/"+agentID+"/status", func(w http.ResponseWriter, _ *http.Request) {
		r.mu.Lock()
		reported := r.reportedID
		r.mu.Unlock()
		writeAnswer(w, http.StatusOK, map[string]any{
			"contract_version": 1, "agent_id": reported, "assignment_epoch": epoch,
			"generation": r.generation, "config_revision": "r1", "state": "serving", "started_at": started,
		})
	})
	r.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		defer close(r.served)
		_ = r.srv.Serve(ln)
	}()
	t.Cleanup(r.stop)
	return r, nil
}

// closeListener is the drain's first step: the address is free, and
// the store's lock still held.
func (r *directRuntime) closeListener() {
	r.closeOnce.Do(func() {
		_ = r.srv.Close()
		<-r.served
	})
}

// releaseLock is the drain's last step, the process exiting.
func (r *directRuntime) releaseLock() { r.unlockOnce.Do(func() { _ = r.lock.Close() }) }

func (r *directRuntime) stop() {
	r.closeListener()
	r.releaseLock()
}

// reportAs makes the status answer name id as the agent it serves.
func (r *directRuntime) reportAs(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reportedID = id
}

// directHost is a host an administrator approved under the direct
// placement and that enrolled with the shipped CLI: the supervisor's
// and the adapter's directories, the runtime's configuration and store,
// and the loopback literal its runtime binds.
type directHost struct {
	supervisor, agentDir, root string
	config, store, address     string
	agent, supervisorGRN       grn.GRN
	epoch                      string
}

// newDirectHost prepares, approves as acme's owner and enrolls a host
// under the direct placement; replaces, where set, makes the approval a
// replacement. The approval and the enrollment record each name the
// placement.
func newDirectHost(t *testing.T, s *executionStack, address string, replaces map[string]any) *directHost {
	t.Helper()
	// A unix socket's path must fit its address, which a test's own
	// temporary directory may not.
	base, err := os.MkdirTemp("", "da")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	h := &directHost{
		supervisor: filepath.Join(base, "s"), agentDir: filepath.Join(base, "a"), root: filepath.Join(base, "root.pem"),
		config: filepath.Join(base, "config.yaml"), store: filepath.Join(base, "memory.db"), address: address,
	}
	presented, ok := s.config.Certificate.(listenerCertificate)
	require.True(t, ok, "the stack presents a loopback certificate")
	require.NoError(t, os.WriteFile(h.root, certificatePEM(presented), 0o644))
	require.NoError(t, os.WriteFile(h.config, []byte("revision: r1\n"), 0o600))

	out, err := standaloneCommand(t, "prepare", "--dir", h.supervisor, "--agent-dir", h.agentDir)
	require.NoError(t, err, out)
	var request map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &request))
	approval := map[string]any{
		"requestId": request["requestId"], "hostName": "laptop", "secretSha256": request["secretSha256"],
		"expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339), "placement": "direct",
	}
	if replaces != nil {
		approval["replaces"] = replaces
	}
	raw, err := json.Marshal(approval)
	require.NoError(t, err)
	status, body := s.approve(t, s.owner, "acme", string(raw))
	require.Equal(t, http.StatusCreated, status, body)
	var approved map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &approved))
	assert.Equal(t, "direct", approved["placement"], "the approval names its placement")

	out, err = standaloneCommand(t, "enroll", "--dir", h.supervisor, "--agent-dir", h.agentDir,
		"--machine-url", s.machine, "--server-root-file", h.root)
	require.NoError(t, err, out)
	record, err := os.ReadFile(filepath.Join(h.supervisor, "enrollment.json"))
	require.NoError(t, err)
	var enrolled machinegen.StandaloneEnrollment
	require.NoError(t, json.Unmarshal(record, &enrolled))
	assert.Equal(t, machinegen.StandaloneEnrollmentPlacementDirect, enrolled.Placement, "the host learns its placement")
	h.agent, err = grn.Parse(enrolled.Grn)
	require.NoError(t, err)
	h.supervisorGRN, err = grn.Parse(enrolled.SupervisorGrn)
	require.NoError(t, err)
	h.epoch = enrolled.Epoch
	return h
}

// freeLoopback answers a loopback literal no listener holds.
func freeLoopback(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := ln.Addr().String()
	require.NoError(t, ln.Close())
	return address
}

// hostRun is one `garam standalone run` of a direct host, in the
// background. done closes once err holds what it returned.
type hostRun struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func (r *hostRun) returned() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// result waits for the run to return on its own.
func (r *hostRun) result(t *testing.T) error {
	t.Helper()
	select {
	case <-r.done:
		return r.err
	case <-time.After(30 * time.Second):
		t.Fatal("the supervisor did not return")
		return nil
	}
}

// run starts the host's supervisor as the CLI ships it, as the host's
// init would, and waits until its control serves the adapter.
func (h *directHost) run(t *testing.T, s *executionStack) *hostRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &hostRun{cancel: cancel, done: make(chan struct{})}
	root := cli.NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"standalone", "run", "--dir", h.supervisor, "--agent-dir", h.agentDir,
		"--machine-url", s.machine, "--config", h.config, "--runtime-address", h.address})
	go func() {
		defer close(r.done)
		r.err = root.ExecuteContext(ctx)
	}()
	t.Cleanup(func() { r.cancel(); <-r.done })
	socket := filepath.Join(h.agentDir, "control", "control.sock")
	require.Eventually(t, func() bool {
		_, err := os.Stat(socket)
		return err == nil || r.returned()
	}, 10*time.Second, 10*time.Millisecond)
	require.False(t, r.returned(), "the supervisor returned before serving: %v %s", r.err, out.String())
	return r
}

// activation is the supervisor's answer to an activation request.
type activation struct {
	status       int
	kind         string
	activationID string
}

// activate asks the supervisor's control to activate generation under
// requestID, at epoch, as the host's adapter would: over the socket in
// its directory, verifying the root written there, presenting the
// control token and the placement token the supervisor wrote for the
// intent it serves now.
func (h *directHost) activate(t *testing.T, requestID, epoch, generation string) activation {
	t.Helper()
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(h.agentDir, name))
		require.NoError(t, err)
		return strings.TrimSpace(string(raw))
	}
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM([]byte(read("control-root.pem"))))
	socket := filepath.Join(h.agentDir, "control", "control.sock")
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
	body, err := json.Marshal(map[string]string{
		"requestId": requestID, "epoch": epoch, "generation": generation, "configRevision": "r1",
	})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, "https://"+workload.ControlName+"/v1/agents/"+h.agent.String()+"/activations",
		bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Garam-Contract-Version", workload.TokenExecutionContract)
	req.Header.Set("Authorization", "Garam-Token "+read("control-token"))
	req.Header.Set("Garam-Placement", read("placement-token"))
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var answer struct {
		Kind         string `json:"kind"`
		ActivationID string `json:"activationId"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&answer))
	return activation{status: resp.StatusCode, kind: answer.Kind, activationID: answer.ActivationID}
}

// current answers the generation garam's current activation of g is
// of, and empty where none is current.
func current(t *testing.T, s *executionStack, g grn.GRN) string {
	t.Helper()
	var generation string
	err := s.pool.QueryRow(context.Background(), `SELECT coalesce(
		(SELECT a.generation FROM agent_executions e JOIN agent_activations a ON a.id = e.activation_id
		 WHERE e.agent_grn = $1), '')`, g.String()).Scan(&generation)
	require.NoError(t, err)
	return generation
}

// TestDirectAttachment_FirstAttachmentAndItsRetryActivateWhatTheRuntimeReports
// is ADR-0111's first attachment end to end. An administrator approves
// a direct host, the shipped CLI enrolls it under
// standalone-enrollment.v2 and runs its supervisor in the attach mode
// against a runtime started under the GRN. The adapter's activation of
// the generation that runtime reports is forwarded and activated at
// garam, and its retry answers the same activation without a second
// one.
func TestDirectAttachment_FirstAttachmentAndItsRetryActivateWhatTheRuntimeReports(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newDirectHost(t, s, freeLoopback(t), nil)
	assert.Equal(t, 0, countRows(t, s.pool, `SELECT count(*) FROM agent_credentials WHERE agent_grn = $1`, h.agent.String()),
		"a direct agent holds no certificate")
	rt, err := startDirectRuntime(t, h.store, h.address, h.agent.String(), h.epoch)
	require.NoError(t, err)
	h.run(t, s)

	first := h.activate(t, "first", h.epoch, rt.generation)
	require.Equal(t, http.StatusCreated, first.status, first.kind)
	assert.Equal(t, rt.generation, current(t, s, h.agent), "garam activated the generation the runtime reports")

	again := h.activate(t, "first", h.epoch, rt.generation)
	require.Equal(t, http.StatusOK, again.status, again.kind)
	assert.Equal(t, first.activationID, again.activationID, "a retry is the same activation")
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agent_activations WHERE agent_grn = $1`, h.agent.String()))
}

// TestDirectAttachment_RefusesWhatTheRuntimeDoesNotReport holds the
// status read standing in for provenance (ADR-0111). Nothing listening
// at the address and a runtime still under its own ID, which answers
// the GRN's path 404, are undecided. A listener reporting another
// agent_id, and a generation other than the one reported, are
// placement_not_current. None reaches garam. The control is the
// generation the runtime under the GRN reports, which is activated.
func TestDirectAttachment_RefusesWhatTheRuntimeDoesNotReport(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newDirectHost(t, s, freeLoopback(t), nil)
	h.run(t, s)
	stale := string(generationOf(7))

	got := h.activate(t, "unread", h.epoch, stale)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, "nothing answers at the address")
	assert.Equal(t, "undecided", got.kind)

	own, err := startDirectRuntime(t, h.store, h.address, "sherlock-laptop", h.epoch)
	require.NoError(t, err)
	got = h.activate(t, "own-id", h.epoch, own.generation)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, "a runtime under its own ID answers the GRN's path 404")
	assert.Equal(t, "undecided", got.kind)
	own.stop()

	rt, err := startDirectRuntime(t, h.store, h.address, h.agent.String(), h.epoch)
	require.NoError(t, err)
	rt.reportAs("sherlock-laptop")
	got = h.activate(t, "other-agent", h.epoch, rt.generation)
	assert.Equal(t, http.StatusForbidden, got.status, "the status names another agent_id")
	assert.Equal(t, "placement_not_current", got.kind)
	rt.reportAs(h.agent.String())

	got = h.activate(t, "stale", h.epoch, stale)
	assert.Equal(t, http.StatusForbidden, got.status, "a generation the runtime does not report")
	assert.Equal(t, "placement_not_current", got.kind)
	assert.Equal(t, 0, countRows(t, s.pool, `SELECT count(*) FROM agent_activations WHERE agent_grn = $1`, h.agent.String()),
		"no refusal reached garam")

	got = h.activate(t, "reported", h.epoch, rt.generation)
	require.Equal(t, http.StatusCreated, got.status, "the control: the generation reported serving: %s", got.kind)
	assert.Equal(t, rt.generation, current(t, s, h.agent))
}

// TestDirectAttachment_ARestartIsTakenOnlyOnceTheOldProcessHasExited
// holds a runtime restarted under the same assignment (ADR-0111). The
// old process closes its listener and still holds the store's lock, so
// the new one is refused at the lock and mints nothing, and a status
// that cannot be read ends nothing at the supervisor. Once the old
// process exits, the new one serves a new generation. The supervisor
// then ends its intent, the next run activates the new generation, and
// garam fences the old one.
func TestDirectAttachment_ARestartIsTakenOnlyOnceTheOldProcessHasExited(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newDirectHost(t, s, freeLoopback(t), nil)
	old, err := startDirectRuntime(t, h.store, h.address, h.agent.String(), h.epoch)
	require.NoError(t, err)
	run := h.run(t, s)
	first := h.activate(t, "first", h.epoch, old.generation)
	require.Equal(t, http.StatusCreated, first.status, first.kind)

	old.closeListener()
	_, err = startDirectRuntime(t, h.store, h.address, h.agent.String(), h.epoch)
	require.ErrorIs(t, err, errStoreLocked, "the old process still holds the store")

	// Something that accepts and answers nothing stands at the free
	// address while the old process drains, so the supervisor's reads
	// can be counted.
	unread, err := net.Listen("tcp", h.address)
	require.NoError(t, err)
	var reads sync.WaitGroup
	reads.Add(2)
	go func() {
		for i := 0; ; i++ {
			conn, err := unread.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
			if i < 2 {
				reads.Done()
			}
		}
	}()
	reads.Wait()
	assert.False(t, run.returned(), "a status that cannot be read ends nothing")
	require.NoError(t, unread.Close())

	old.releaseLock()
	restarted, err := startDirectRuntime(t, h.store, h.address, h.agent.String(), h.epoch)
	require.NoError(t, err, "the retry after the old process exited takes the store")
	require.Error(t, run.result(t), "the supervisor ends the intent once another generation serves")
	assert.Equal(t, old.generation, current(t, s, h.agent), "nothing is activated until the adapter asks")

	h.run(t, s)
	got := h.activate(t, "old-again", h.epoch, old.generation)
	assert.Equal(t, http.StatusForbidden, got.status, "the old generation is no longer reported")
	got = h.activate(t, "restarted", h.epoch, restarted.generation)
	require.Equal(t, http.StatusCreated, got.status, got.kind)
	assert.Equal(t, restarted.generation, current(t, s, h.agent))
	assert.Equal(t, 1, countRows(t, s.pool,
		`SELECT count(*) FROM agent_activations WHERE id = $1 AND ended_reason = 'superseded'`, first.activationID),
		"garam fences the generation the restart replaced")
}

// TestDirectAttachment_AReplacementActivatesOnlyAGenerationReportingTheNewEpoch
// holds a replacement to a new direct host (ADR-0111): the
// administrator's replacement is admitted at the next epoch and ends
// the old activation. The new supervisor forwards only a generation
// reporting that epoch. A runtime still at the old epoch is
// placement_not_current, and a request naming the old epoch is
// epoch_superseded. The runtime restarted at the new epoch is
// activated.
func TestDirectAttachment_AReplacementActivatesOnlyAGenerationReportingTheNewEpoch(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	address := freeLoopback(t)
	before := newDirectHost(t, s, address, nil)
	old, err := startDirectRuntime(t, before.store, address, before.agent.String(), before.epoch)
	require.NoError(t, err)
	beforeRun := before.run(t, s)
	first := before.activate(t, "first", before.epoch, old.generation)
	require.Equal(t, http.StatusCreated, first.status, first.kind)
	beforeRun.cancel()
	<-beforeRun.done

	after := newDirectHost(t, s, address, map[string]any{
		"agentGrn": before.agent.String(), "assigneeGrn": before.supervisorGRN.String(), "epoch": 1,
	})
	after.store = before.store
	require.Equal(t, before.agent, after.agent, "a replacement keeps the GRN")
	require.Equal(t, "2", after.epoch)
	assert.Empty(t, current(t, s, after.agent), "the replacement ended the old activation")
	after.run(t, s)

	got := after.activate(t, "old-runtime", after.epoch, old.generation)
	assert.Equal(t, http.StatusForbidden, got.status, "the runtime still reports the old epoch")
	assert.Equal(t, "placement_not_current", got.kind)
	got = after.activate(t, "old-epoch", before.epoch, old.generation)
	assert.Equal(t, http.StatusConflict, got.status)
	assert.Equal(t, "epoch_superseded", got.kind)

	old.stop()
	restarted, err := startDirectRuntime(t, after.store, address, after.agent.String(), after.epoch)
	require.NoError(t, err)
	got = after.activate(t, "new-epoch", after.epoch, restarted.generation)
	require.Equal(t, http.StatusCreated, got.status, "the control: a generation reporting the new epoch: %s", got.kind)
	assert.Equal(t, restarted.generation, current(t, s, after.agent))
}

// TestDirectAttachment_AdmissionIsTheAdministratorsAndNeverAnOperators
// holds where a direct agent is admitted. agent-enrollment.v1 refuses
// direct placement to the hosted operator by its permanent reason, and
// an approval binding the host's keys cannot be direct. The control is
// the same direct approval binding no key, which is recorded.
func TestDirectAttachment_AdmissionIsTheAdministratorsAndNeverAnOperators(t *testing.T) {
	s := newTokenStack(t)
	ref := s.creationRef(t, s.member, "enrol-1")
	got := s.prepare(t, bindingBody("enrol-1", "direct", s.controller, ref, nil))
	assert.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "direct_placement_gated", reasonOf(t, got))

	h := newStandaloneHost(t)
	keyed := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(h.approval(t, time.Now().Add(time.Hour), nil)), &keyed))
	keyed["placement"] = "direct"
	raw, err := json.Marshal(keyed)
	require.NoError(t, err)
	status, body := s.approve(t, s.owner, "acme", string(raw))
	assert.Equal(t, http.StatusBadRequest, status, "a direct host binds no key: %s", body)

	keyless := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(h.credentialApproval(t)), &keyless))
	keyless["placement"] = "direct"
	raw, err = json.Marshal(keyless)
	require.NoError(t, err)
	status, body = s.approve(t, s.owner, "acme", string(raw))
	assert.Equal(t, http.StatusCreated, status, "the control: a direct approval binding no key: %s", body)
	assert.Equal(t, agent.CredentialStandaloneContractVersion, credentialContract(t, s, h))
}

// credentialContract spends h's keyless approval and answers the value
// the answer names, which is the contract a direct host enrolls under.
func credentialContract(t *testing.T, s *tokenStack, h standaloneHost) string {
	t.Helper()
	got := s.spendV2(t, h.secret, h.credentialEnrollment())
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var e machinegen.StandaloneEnrollment
	require.NoError(t, json.Unmarshal([]byte(got.body), &e))
	assert.Equal(t, machinegen.StandaloneEnrollmentPlacementDirect, e.Placement)
	return got.version
}
