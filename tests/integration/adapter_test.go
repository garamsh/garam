package integration_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// heldPair is the agent's certificate as the files hold it, replaceable
// mid-test the way a renewal replaces the files.
type heldPair struct {
	mu   sync.Mutex
	pair tls.Certificate
}

func (h *heldPair) get() (*tls.Certificate, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	pair := h.pair
	return &pair, nil
}

func (h *heldPair) replace(pair tls.Certificate) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pair = pair
}

// presenting is a client that presents what h holds on every handshake,
// as the adapter's own clients do (ADR-0040).
func presenting(t *testing.T, h *heldPair) *http.Client {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify:   true,
		MinVersion:           tls.VersionTLS13,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) { return h.get() },
	}}}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

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

// controlStandIn is the lifecycle authority's adapter-facing activation
// route as agent-execution.v1 pins it, in front of garam's real
// activateAgent: it reads the leaf the adapter presented on its own
// connection, preserves the replacement anchor per request so a replay
// is the same request to garam, and forwards garam's answer. It stands
// in for the Operator control service, which is not this repository's.
type controlStandIn struct {
	stack  *executionStack
	server *httptest.Server

	mu       sync.Mutex
	replaces map[string]*string
	latest   *string
	asked    []string
}

func newControlStandIn(t *testing.T, s *executionStack) *controlStandIn {
	t.Helper()
	c := &controlStandIn{stack: s, replaces: map[string]*string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/agents/{grn}/activations", c.activate)
	mux.HandleFunc("POST /v1/agents/{grn}/runtime-status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Garam-Contract-Version", delivery.ExecutionContract)
		w.WriteHeader(http.StatusNoContent)
	})
	c.server = httptest.NewUnstartedServer(mux)
	c.server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS13}
	c.server.StartTLS()
	t.Cleanup(c.server.Close)
	return c
}

func (c *controlStandIn) activate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Garam-Contract-Version", delivery.ExecutionContract)
	refuse := func(status int, kind string) {
		writeAnswer(w, status, map[string]string{"kind": kind, "message": kind})
	}
	if r.Header.Get("Garam-Contract-Version") != delivery.ExecutionContract {
		refuse(http.StatusBadRequest, "invalid_request")
		return
	}
	leaf := r.TLS.PeerCertificates[0]
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != r.PathValue("grn") {
		refuse(http.StatusForbidden, "not_authorized")
		return
	}
	var body struct {
		RequestID      string `json:"requestId"`
		Epoch          string `json:"epoch"`
		Generation     string `json:"generation"`
		ConfigRevision string `json:"configRevision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		refuse(http.StatusBadRequest, "invalid_request")
		return
	}
	epoch, err := strconv.Atoi(body.Epoch)
	if err != nil {
		refuse(http.StatusBadRequest, "invalid_request")
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, body.RequestID)
	replaces, replay := c.replaces[body.RequestID]
	if !replay {
		replaces = c.latest
		c.replaces[body.RequestID] = replaces
	}
	leafPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw}))
	got, err := c.stack.exchange(c.stack.hostedTLS, http.MethodPost, c.stack.agentPath("/activations"),
		agent.ExecutionContractVersion, "", activationBodyOf(body.RequestID, epoch, agent.Generation(body.Generation), replaces, leafPEM))
	switch {
	case err != nil || got.status >= http.StatusInternalServerError:
		refuse(http.StatusServiceUnavailable, "undecided")
		return
	case got.status == http.StatusConflict:
		refuse(http.StatusConflict, "activation_superseded")
		return
	case got.status != http.StatusOK && got.status != http.StatusCreated:
		refuse(http.StatusForbidden, "credential_fenced")
		return
	}
	var a machinegen.Activation
	if err := json.Unmarshal([]byte(got.body), &a); err != nil {
		refuse(http.StatusServiceUnavailable, "undecided")
		return
	}
	c.latest = &a.ActivationId
	writeAnswer(w, got.status, map[string]any{
		"activationId": a.ActivationId, "tokenVersion": a.TokenVersion, "token": a.Token,
		"grn": a.Grn, "epoch": a.Epoch, "generation": a.Generation, "configRevision": body.ConfigRevision,
	})
}

// requests is every request identifier the adapter asked under.
func (c *controlStandIn) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.asked...)
}

// adapterProcess is one fenced adapter process beside rt, presenting
// what held holds, under the placement token in tokenFile, forwarding
// the outbox it was given.
type adapterProcess struct {
	execution *delivery.Execution
	queue     *delivery.HTTPQueue
	stop      func()
}

func startAdapter(t *testing.T, s *executionStack, c *controlStandIn, rt *runtimeDouble, held *heldPair, tokenFile string) *adapterProcess {
	t.Helper()
	return startAdapterWith(t, s, c, rt, held, tokenFile, t.TempDir())
}

// startAdapterWith is startAdapter forwarding the outbox in dir.
func startAdapterWith(t *testing.T, s *executionStack, c *controlStandIn, rt *runtimeDouble, held *heldPair, tokenFile, dir string) *adapterProcess {
	t.Helper()
	return startAdapterSigning(t, s, c, rt, held, tokenFile, dir, false)
}

// startAdapterSigning is startAdapterWith with the adapter's signing
// switch as sign says.
func startAdapterSigning(t *testing.T, s *executionStack, c *controlStandIn, rt *runtimeDouble, held *heldPair, tokenFile, dir string, sign bool) *adapterProcess {
	t.Helper()
	execution := delivery.NewFencedExecution()
	client := presenting(t, held)
	queue, err := delivery.NewHTTPQueue(client, s.machine, s.agent, execution, delivery.NewCertificate(nil))
	require.NoError(t, err)
	gateway, err := delivery.NewHTTPGateway(rt.server.Client(), rt.server.URL, s.agent)
	require.NoError(t, err)
	authority, err := delivery.NewHTTPAuthority(presenting(t, held), c.server.URL, s.agent, delivery.NewCertificate(held.get))
	require.NoError(t, err)
	outbox, err := delivery.NewOutbox(dir)
	require.NoError(t, err)
	loop, err := delivery.NewService(delivery.Ends{
		Agent: s.agent, Queue: queue, Gateway: gateway, Execution: execution, Authority: authority,
		Credentials: delivery.NewFileCredentials(tokenFile, delivery.NewCertificate(held.get)), Outbox: outbox,
	}, delivery.Config{
		RetryDelay: 10 * time.Millisecond, MaxRetryDelay: 50 * time.Millisecond, MinRenewInterval: 50 * time.Millisecond,
		StatusInterval: 10 * time.Millisecond, OutboxInterval: 50 * time.Millisecond,
		ClaimTimeout: 5 * time.Second, CallTimeout: 2 * time.Second,
		ForwardSignature: sign,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		assert.NoError(t, loop.Run(ctx))
	}()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return &adapterProcess{execution: execution, queue: queue, stop: stop}
}

// placementToken writes a placement token file and answers its path.
func placementToken(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte(token+"\n"), 0o600))
	return path
}

// stackPair is the agent's current certificate as the stack issued it.
func (s *executionStack) agentPair(t *testing.T) tls.Certificate {
	t.Helper()
	tr, ok := s.agentTLS.Transport.(*http.Transport)
	require.True(t, ok)
	return tr.TLSClientConfig.Certificates[0]
}

// postTo has the peer post a message to the agent and answers its
// identifier.
func (s *executionStack) postTo(t *testing.T, body string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"recipient": s.agent.String(), "idempotencyKey": s.key(), "body": body})
	require.NoError(t, err)
	got := s.send(t, s.peerTLS, http.MethodPost, "/messages", "", "", string(raw))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var m machinegen.Message
	require.NoError(t, json.Unmarshal([]byte(got.body), &m))
	return m.Id
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

// TestAdapter_ReadsAClaimsConflictAsTheFenceAndARenewalsAsALapse holds
// ADR-0084's reading of the claim route against the real listener: a
// 409 on a claim has no source but the fence, so a certificate past its
// validity on the connection it opened is read as the fence and closes
// the execution. The same certificate's own current successor claiming
// unfenced is the control, and a renewal's 409 for a hold that is gone
// still reads as a lapse.
func TestAdapter_ReadsAClaimsConflictAsTheFenceAndARenewalsAsALapse(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	issued, err := s.past.Issue(context.Background(), s.agent)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair(issued.CertificatePEM(), issued.PrivateKeyPEM())
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	require.NoError(t, err)
	expiredTLS := machineClient(t, pair)
	s.clock.shift(leaf.NotAfter.Add(-time.Minute).Sub(time.Now()))
	s.open(t, expiredTLS)
	s.clock.shift(0)

	expired := delivery.NewLegacyExecution()
	queue, err := delivery.NewHTTPQueue(expiredTLS, s.machine, s.agent, expired, delivery.NewCertificate(nil))
	require.NoError(t, err)
	_, err = queue.Claim(context.Background())
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "the claim's 409 is the fence: %v", err)
	assert.Contains(t, err.Error(), "409 failed_precondition: credential is not current")
	_, err = expired.Present()
	assert.ErrorIs(t, err, delivery.ErrInactive, "and closes the execution")

	current := delivery.NewLegacyExecution()
	control, err := delivery.NewHTTPQueue(s.agentTLS, s.machine, s.agent, current, delivery.NewCertificate(nil))
	require.NoError(t, err)
	s.postTo(t, "hello")
	claim, err := control.Claim(context.Background())
	require.NoError(t, err, "the control: the current certificate claims unfenced")
	require.NoError(t, control.Complete(context.Background(), claim.ID))
	_, err = control.Renew(context.Background(), claim.ID)
	assert.ErrorIs(t, err, delivery.ErrClaimLapsed, "a renewal's 409 for a hold that is gone is a lapse")
	_, err = current.Present()
	assert.NoError(t, err, "which fences nothing")
}

// TestAdapter_ALegacyAdapterOfACutOverAgentIsRefusedByGaram is the
// misconfiguration ADR-0084 leaves to garam: an adapter run without
// control settings for an agent already activated presents no token,
// and garam refuses its claim. The same legacy claim before the cutover
// is the control.
func TestAdapter_ALegacyAdapterOfACutOverAgentIsRefusedByGaram(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)

	before := delivery.NewLegacyExecution()
	queue, err := delivery.NewHTTPQueue(s.agentTLS, s.machine, s.agent, before, delivery.NewCertificate(nil))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = queue.Claim(ctx)
	assert.ErrorIs(t, err, delivery.ErrNoMessageReady, "the control: a legacy agent claims without a token")

	s.activated(t, "r-1", generationOf(1), nil, s.leaf)
	after := delivery.NewLegacyExecution()
	queue, err = delivery.NewHTTPQueue(s.agentTLS, s.machine, s.agent, after, delivery.NewCertificate(nil))
	require.NoError(t, err)
	_, err = queue.Claim(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "%v", err)
	assert.Contains(t, err.Error(), "401 unauthenticated")
}

// TestAdapter_ReadsAnotherTenantsRefusalAsTheFence is an adapter
// presenting another organization's agent certificate against this
// agent's queue: garam refuses it, and the adapter stops rather than
// retrying. That certificate's own queue is the control.
func TestAdapter_ReadsAnotherTenantsRefusalAsTheFence(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	other := scopedGRN("globex", "agent", "0f0e0d0c0b0a0908")
	otherTLS := machineClient(t, installed(t, p, s.issuer, other))

	wrong := delivery.NewLegacyExecution()
	queue, err := delivery.NewHTTPQueue(otherTLS, s.machine, s.agent, wrong, delivery.NewCertificate(nil))
	require.NoError(t, err)
	_, err = queue.Claim(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "%v", err)
	_, err = wrong.Present()
	assert.ErrorIs(t, err, delivery.ErrInactive, "another tenant's refusal stops protected work")

	own := delivery.NewLegacyExecution()
	queue, err = delivery.NewHTTPQueue(otherTLS, s.machine, other, own, delivery.NewCertificate(nil))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = queue.Claim(ctx)
	assert.ErrorIs(t, err, delivery.ErrNoMessageReady, "the control: its own queue answers")
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
	held := &heldPair{pair: s.agentPair(t)}
	token := placementToken(t, "placement-1")

	first := newRuntimeDouble(t, s.agent, s.epoch, generationOf(1))
	one := startAdapter(t, s, c, first, held, token)
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
	startAdapter(t, s, c, second, held, token)
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
	held := &heldPair{pair: s.agentPair(t)}
	token := placementToken(t, "placement-1")

	signing := newRuntimeDouble(t, s.agent, s.epoch, generationOf(1))
	one := startAdapterSigning(t, s, c, signing, held, token, t.TempDir(), true)
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
	startAdapter(t, s, c, unsigned, held, token)
	next := s.postTo(t, "café")
	require.Eventually(t, func() bool { return len(unsigned.received()) == 1 }, 10*time.Second, 10*time.Millisecond,
		"the next message reaches the runtime")
	assert.Equal(t, map[string]string{"id": next, "content": got["content"]}, unsigned.received()[0],
		"the switch off sends the same content and no signature member")
}

// TestAdapter_ReplaysItsActivationAcrossRenewalAndRestart is the
// consumer test #1169's 16:47 decision asks for. A restarted adapter
// presenting a renewed certificate replays the same request and is
// answered the same activation at a higher token version. The expired
// first certificate, and the same request after the activation ended,
// are refused, and the adapter pauses on each rather than retrying.
func TestAdapter_ReplaysItsActivationAcrossRenewalAndRestart(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	c := newControlStandIn(t, s)
	rt := newRuntimeDouble(t, s.agent, s.epoch, generationOf(1))
	token := placementToken(t, "placement-1")
	held := &heldPair{pair: s.agentPair(t)}

	one := startAdapter(t, s, c, rt, held, token)
	require.Eventually(t, func() bool { _, err := one.execution.Present(); return err == nil },
		10*time.Second, 10*time.Millisecond, "the first process is activated")
	first := s.standing(t, s.leaf, ptr(generationOf(1)))
	require.Equal(t, machinegen.ExecutionIntrospectionGenerationCurrent, first.Generation)

	// The agent renews its own certificate over its connection, as
	// ADR-0040 has it, presenting the token it holds.
	presented, err := one.execution.Present()
	require.NoError(t, err)
	renewal := s.send(t, s.agentTLS, http.MethodPost, s.agentPath("/certificate"), "", bearer(string(presented)), "")
	require.Equal(t, http.StatusCreated, renewal.status, renewal.body)
	var renewed machinegen.MachineCertificate
	require.NoError(t, json.Unmarshal([]byte(renewal.body), &renewed))
	one.stop()

	held.replace(pairOf(t, renewed))
	two := startAdapter(t, s, c, rt, held, token)
	require.Eventually(t, func() bool { _, err := two.execution.Present(); return err == nil },
		10*time.Second, 10*time.Millisecond, "the restarted process is activated with the renewed certificate")
	asked := c.requests()
	require.Len(t, asked, 2)
	assert.Equal(t, asked[0], asked[1], "the restart replays the same request: the certificate is not part of it")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = two.queue.Claim(ctx)
	assert.ErrorIs(t, err, delivery.ErrNoMessageReady, "and the newer token is admitted")
	two.stop()

	// The first certificate, expired: control forwards it and garam
	// refuses it, so the adapter pauses.
	issued, err := s.past.Issue(context.Background(), s.agent)
	require.NoError(t, err)
	expired, err := tls.X509KeyPair(issued.CertificatePEM(), issued.PrivateKeyPEM())
	require.NoError(t, err)
	held.replace(expired)
	three := startAdapter(t, s, c, rt, held, token)
	require.Eventually(t, func() bool { return len(c.requests()) == 3 }, 10*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	assert.Len(t, c.requests(), 3, "an expired certificate's refusal is not resent on a timer")
	_, err = three.execution.Present()
	assert.ErrorIs(t, err, delivery.ErrInactive)
	three.stop()

	// The activation ends; the same request is refused.
	require.NotNil(t, first.ActivationId)
	ended := s.deactivate(t, *first.ActivationId)
	require.Equal(t, http.StatusOK, ended.status, ended.body)
	held.replace(pairOf(t, renewed))
	four := startAdapter(t, s, c, rt, held, token)
	require.Eventually(t, func() bool { return len(c.requests()) == 4 }, 10*time.Second, 10*time.Millisecond)
	assert.Equal(t, asked[0], c.requests()[3], "the same request")
	time.Sleep(200 * time.Millisecond)
	_, err = four.execution.Present()
	assert.ErrorIs(t, err, delivery.ErrInactive, "an ended activation is not replayed into a token")
}
