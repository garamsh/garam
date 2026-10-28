package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/delivery"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// runtimeDouble is Sherlock v0.1.0's loopback gateway as the adapter
// reads it: its status, its ingress keyed by the caller's identifier,
// the lookup of one it took, and its event stream. A turn is ended by
// the test.
type runtimeDouble struct {
	t      *testing.T
	server *httptest.Server

	mu          sync.Mutex
	status      map[string]any
	posts       []map[string]string
	completed   map[string]bool
	subscribers []chan string
	// looks counts status reads: the adapter's supervisor reads the
	// status once at the start of each look.
	looks int
}

func newRuntimeDouble(t *testing.T, agentGRN grn.GRN, epoch int, generation agent.Generation) *runtimeDouble {
	t.Helper()
	r := &runtimeDouble{t: t, completed: map[string]bool{}, status: map[string]any{
		"contract_version": 1, "agent_id": agentGRN.String(), "assignment_epoch": strconv.Itoa(epoch),
		"generation": string(generation), "config_revision": "1", "state": "serving",
		"started_at": time.Now().UTC().Format(time.RFC3339),
	}}
	mux := http.NewServeMux()
	served := "/agents/" + agentGRN.String()
	mux.HandleFunc("GET "+served+"/status", func(w http.ResponseWriter, _ *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.looks++
		writeAnswer(w, http.StatusOK, r.status)
	})
	mux.HandleFunc("POST "+served+"/messages", func(w http.ResponseWriter, req *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeAnswer(w, http.StatusBadRequest, map[string]string{"error": "body"})
			return
		}
		r.mu.Lock()
		r.posts = append(r.posts, body)
		done := r.completed[body["id"]]
		r.mu.Unlock()
		if done {
			writeAnswer(w, http.StatusOK, map[string]string{"id": body["id"], "status": "completed"})
			return
		}
		writeAnswer(w, http.StatusAccepted, map[string]string{"id": body["id"], "status": "queued"})
	})
	mux.HandleFunc("GET "+served+"/messages/{id}", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		status := "pending"
		if r.completed[req.PathValue("id")] {
			status = "completed"
		}
		writeAnswer(w, http.StatusOK, map[string]string{"id": req.PathValue("id"), "status": status})
	})
	mux.HandleFunc("GET "+served+"/events", func(w http.ResponseWriter, req *http.Request) {
		events := make(chan string, 16)
		r.mu.Lock()
		r.subscribers = append(r.subscribers, events)
		r.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		flusher.Flush()
		for {
			select {
			case <-req.Context().Done():
				return
			case event := <-events:
				fmt.Fprintf(w, "data: %s\n\n", event)
				flusher.Flush()
			}
		}
	})
	r.server = httptest.NewServer(mux)
	t.Cleanup(r.server.Close)
	return r
}

func writeAnswer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// received is every post the runtime took, oldest first.
func (r *runtimeDouble) received() []map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]map[string]string(nil), r.posts...)
}

// looked is how many times the status was read.
func (r *runtimeDouble) looked() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.looks
}

// finish ends a turn that drained the message, committing it first.
func (r *runtimeDouble) finish(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completed[id] = true
	event := `{"kind":"turn_finished","turn":1,"message_ids":["` + id + `"]}`
	for _, s := range r.subscribers {
		s <- event
	}
}

// newControlStandIn is the lifecycle authority in front of the
// stack's own agent, which its controller holds outside any managed
// creation, so its first activation names no reference.
func newControlStandIn(t *testing.T, s *executionStack) *tokenControlStandIn {
	t.Helper()
	c := newTokenControlStandIn(t, &tokenStack{executionStack: s}, managed{agent: s.agent})
	c.ref = nil
	return c
}

// adapterProcess is one fenced adapter process: its execution, and the
// queue its loop claims on, until stop.
type adapterProcess struct {
	execution *delivery.Execution
	queue     *delivery.HTTPQueue
	stop      func()
}

// agentFiles are the stack's agent's token files: its own token for
// garam, the token shown to the hosted control service as its control
// token, and the placement token.
func (s *executionStack) agentFiles(t *testing.T) tokenFiles {
	t.Helper()
	return newTokenFiles(t, s.agentToken(t, s.agent, nil).Token, s.forwarded)
}

func startAdapter(t *testing.T, s *executionStack, c *tokenControlStandIn, rt *runtimeDouble, files tokenFiles) *adapterProcess {
	t.Helper()
	return startAdapterWith(t, s, c, rt, files, t.TempDir())
}

// startAdapterWith is startAdapter forwarding the outbox in dir.
func startAdapterWith(t *testing.T, s *executionStack, c *tokenControlStandIn, rt *runtimeDouble, files tokenFiles, dir string) *adapterProcess {
	t.Helper()
	return startTokenAdapterSigning(t, s, c, rt, s.agent, files, dir, false)
}

// placementToken writes a placement token file and answers its path.
func placementToken(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte(token+"\n"), 0o600))
	return path
}

// postTo has the peer post a message to the agent and answers its
// identifier.
func (s *executionStack) postTo(t *testing.T, body string) string {
	t.Helper()
	return s.postToAgent(t, s.agent, body)
}

// stateOf is the state the peer reads for a message it sent.
func (s *executionStack) stateOf(t *testing.T, id string) string {
	t.Helper()
	got := s.send(t, s.peerTLS, http.MethodGet, "/messages/"+id, "", "", "")
	require.Equal(t, http.StatusOK, got.status, got.body)
	var status machinegen.MessageStatus
	require.NoError(t, json.Unmarshal([]byte(got.body), &status))
	return string(status.State)
}

// activatedProcess runs a fenced adapter for the stack's agent until
// its execution holds an activation, then stops its loop, so a test can
// make calls of its own on what that activation opened.
func activatedProcess(t *testing.T, s *executionStack) *adapterProcess {
	t.Helper()
	c := newControlStandIn(t, s)
	proc := startAdapter(t, s, c, newRuntimeDouble(t, s.agent, s.epoch, generationOf(1)), s.agentFiles(t))
	require.Eventually(t, func() bool {
		_, err := proc.execution.Present()
		return err == nil
	}, 10*time.Second, 10*time.Millisecond, "the adapter activates its generation")
	proc.stop()
	return proc
}

// TestAdapter_ReadsARenewalsConflictAsALapse holds ADR-0084's reading
// of a renewal against the real listener: a renewal's 409 for a hold
// that is gone is a lapse and fences nothing. The claim and completion
// it follows, under the activation's token, are the control.
func TestAdapter_ReadsARenewalsConflictAsALapse(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	proc := activatedProcess(t, s)

	s.postTo(t, "hello")
	claim, err := proc.queue.Claim(context.Background())
	require.NoError(t, err, "the control: the agent's token claims under the activation")
	require.NoError(t, proc.queue.Complete(context.Background(), claim.ID))
	_, err = proc.queue.Renew(context.Background(), claim.ID)
	assert.ErrorIs(t, err, delivery.ErrClaimLapsed, "a renewal's 409 for a hold that is gone is a lapse")
	_, err = proc.execution.Present()
	assert.NoError(t, err, "which fences nothing")
}

// TestAdapter_ReadsAnotherTenantsRefusalAsTheFence is an adapter
// presenting another organization's agent token against this agent's
// queue, beside the activation's token: garam refuses it, and the
// adapter stops rather than retrying. The agent's own token on the
// same queue under the same execution token is the control.
func TestAdapter_ReadsAnotherTenantsRefusalAsTheFence(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	foreign, _, _ := lineaged(t, p, registerOperator(t, p, scopedGRN("globex", operator.ServiceType, "controller")))
	otherToken := newTokenFiles(t, foreign.mintedIn(t).Value, "")
	proc := activatedProcess(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := proc.queue.Claim(ctx)
	assert.ErrorIs(t, err, delivery.ErrNoMessageReady, "the control: its own token's queue answers")

	wrong, err := delivery.NewHTTPQueue(serverTLSOnly(t), s.machine, s.agent, proc.execution, tokenFileOf(t, otherToken.garam))
	require.NoError(t, err)
	_, err = wrong.Claim(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "%v", err)
	_, err = proc.execution.Present()
	assert.ErrorIs(t, err, delivery.ErrInactive, "another tenant's refusal stops protected work")
}

// TestAdapter_CarriesAMessageAndAReplacementFencesTheGenerationBefore
// runs the adapter between the real listener and a runtime: it
// activates the generation the runtime reports, posts the peer's
// message under garam's identifier with the verified sender, and
// completes it when the turn ends. A second process beside a restarted
// runtime activates the new generation, which replaces the first: the
// first process's next claim is refused and it stops, while the second
// carries the next message.
func TestAdapter_CarriesAMessageAndAReplacementFencesTheGenerationBefore(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	c := newControlStandIn(t, s)
	files := s.agentFiles(t)

	first := newRuntimeDouble(t, s.agent, s.epoch, generationOf(1))
	one := startAdapter(t, s, c, first, files)
	id := s.postTo(t, `{"sender":"grn:evil:default:agent:1"}`)
	require.Eventually(t, func() bool { return len(first.received()) == 1 }, 10*time.Second, 10*time.Millisecond,
		"the message reaches the runtime")
	got := first.received()[0]
	assert.Equal(t, id, got["id"], "garam's message identifier, not the lease")
	assert.Equal(t, `{"contract":"garam-message.v1","sender":"`+s.peer.String()+
		`","body":"{\"sender\":\"grn:evil:default:agent:1\"}"}`, got["content"])
	first.finish(id)
	require.Eventually(t, func() bool { return s.stateOf(t, id) == "completed" }, 10*time.Second, 20*time.Millisecond,
		"the turn's end completes the message at garam")

	second := newRuntimeDouble(t, s.agent, s.epoch, generationOf(2))
	startAdapter(t, s, c, second, files)
	require.Eventually(t, func() bool {
		_, err := one.execution.Present()
		return err != nil
	}, 10*time.Second, 10*time.Millisecond, "the replaced generation's process is fenced")
	asked := c.requests()
	require.Len(t, asked, 2)
	assert.NotEqual(t, asked[0], asked[1], "a new generation is a new request")

	next := s.postTo(t, "the next message")
	require.Eventually(t, func() bool { return len(second.received()) == 1 }, 10*time.Second, 10*time.Millisecond,
		"the current generation carries the next message")
	assert.Equal(t, next, second.received()[0]["id"])
	assert.Len(t, first.received(), 1, "the fenced process carried nothing more")
}

// TestAdapter_ForwardsWhatGaramSignedSoTheRuntimeCanVerifyIt is
// ADR-0101 step 2 end to end: garam signs the claim, the adapter with
// its switch on posts the content and the signature, and the bytes the
// runtime received verify against the set garam publishes, under the
// audience the runtime serves and the identifier the message arrived
// under. A second adapter with the switch off is the control: it posts
// the same content and no signature member at all.
func TestAdapter_ForwardsWhatGaramSignedSoTheRuntimeCanVerifyIt(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	c := newControlStandIn(t, s)
	files := s.agentFiles(t)

	signing := newRuntimeDouble(t, s.agent, s.epoch, generationOf(1))
	one := startTokenAdapterSigning(t, s, c, signing, s.agent, files, t.TempDir(), true)
	id := s.postTo(t, "café")
	require.Eventually(t, func() bool { return len(signing.received()) == 1 }, 10*time.Second, 10*time.Millisecond,
		"the message reaches the runtime")
	got := signing.received()[0]
	require.Contains(t, got, "signature", "the switch on sends the signature")

	claims, err := verify(keySet(t, s.machine), string(testIssuer), s.agent.String(), got["id"], got["content"], got["signature"])
	require.NoError(t, err, "the bytes the runtime received verify against garam's published set")
	assert.Equal(t, id, claims.MessageID)
	assert.Equal(t, s.peer.String(), claims.Sub, "garam vouches for the sender its listener authenticated")
	_, err = verify(keySet(t, s.machine), string(testIssuer), s.agent.String(), got["id"], got["content"]+" ", got["signature"])
	require.Error(t, err, "the control on the verifier: one more byte of content is refused")
	signing.finish(id)
	require.Eventually(t, func() bool { return s.stateOf(t, id) == "completed" }, 10*time.Second, 20*time.Millisecond,
		"the signed message completes like any other")
	one.stop()

	unsigned := newRuntimeDouble(t, s.agent, s.epoch, generationOf(2))
	startAdapter(t, s, c, unsigned, files)
	next := s.postTo(t, "café")
	require.Eventually(t, func() bool { return len(unsigned.received()) == 1 }, 10*time.Second, 10*time.Millisecond,
		"the next message reaches the runtime")
	assert.Equal(t, map[string]string{"id": next, "content": got["content"]}, unsigned.received()[0],
		"the switch off sends the same content and no signature member")
}

// TestAdapter_ReplaysItsActivationAcrossRefreshAndRestart is the
// consumer test #1169's 16:47 decision asks for. A restarted adapter
// holding refreshed tokens of the agent replays the same request and
// is answered the same activation at a higher token version. A control
// token past its expiry, and the same request after the activation
// ended, are refused, and the adapter pauses on each rather than
// retrying.
func TestAdapter_ReplaysItsActivationAcrossRefreshAndRestart(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	c := newControlStandIn(t, s)
	rt := newRuntimeDouble(t, s.agent, s.epoch, generationOf(1))

	one := startAdapter(t, s, c, rt, s.agentFiles(t))
	require.Eventually(t, func() bool { _, err := one.execution.Present(); return err == nil },
		10*time.Second, 10*time.Millisecond, "the first process is activated")
	first := s.standing(t, s.forwarded, ptr(generationOf(1)))
	require.Equal(t, machinegen.ExecutionIntrospectionGenerationCurrent, first.Generation)

	// The agent holds refreshed tokens its controller minted.
	one.stop()
	refreshed := newTokenFiles(t, s.agentToken(t, s.agent, nil).Token, s.agentToken(t, s.agent, &s.hosted).Token)
	two := startAdapter(t, s, c, rt, refreshed)
	require.Eventually(t, func() bool { _, err := two.execution.Present(); return err == nil },
		10*time.Second, 10*time.Millisecond, "the restarted process is activated with the refreshed tokens")
	asked := c.requests()
	require.Len(t, asked, 2)
	assert.Equal(t, asked[0], asked[1], "the restart replays the same request: the token is not part of it")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := two.queue.Claim(ctx)
	assert.ErrorIs(t, err, delivery.ErrNoMessageReady, "and the newer execution token is admitted")
	two.stop()

	// A control token past its expiry: control forwards it and garam
	// refuses it, so the adapter pauses.
	expired := s.agentToken(t, s.agent, &s.hosted)
	_, err = p.Exec(context.Background(), `UPDATE agent_tokens SET expires_at = now() - interval '1 second',
		issued_at = now() - interval '1 hour' WHERE token_hash = $1`, agent.HashAccessToken(expired.Token))
	require.NoError(t, err)
	three := startAdapter(t, s, c, rt, newTokenFiles(t, s.agentToken(t, s.agent, nil).Token, expired.Token))
	require.Eventually(t, func() bool { return len(c.requests()) == 3 }, 10*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	assert.Len(t, c.requests(), 3, "an expired token's refusal is not resent on a timer")
	_, err = three.execution.Present()
	assert.ErrorIs(t, err, delivery.ErrInactive)
	three.stop()

	// The activation ends; the same request is refused.
	require.NotNil(t, first.ActivationId)
	ended := s.deactivate(t, *first.ActivationId)
	require.Equal(t, http.StatusOK, ended.status, ended.body)
	four := startAdapter(t, s, c, rt, refreshed)
	require.Eventually(t, func() bool { return len(c.requests()) == 4 }, 10*time.Second, 10*time.Millisecond)
	assert.Equal(t, asked[0], c.requests()[3], "the same request")
	time.Sleep(200 * time.Millisecond)
	_, err = four.execution.Present()
	assert.ErrorIs(t, err, delivery.ErrInactive, "an ended activation is not replayed into a token")
}
