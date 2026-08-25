// Package delivery_test drives the loop against a garam and an agent
// platform that are both fakes over HTTP: what each answers is what
// api/machine.yaml and the platform's gateway answer, so the loop is
// exercised through the clients it really uses rather than past them.
package delivery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/grn"
)

// agent is the GRN garam knows the agent by. The gateway knows it by
// something else entirely (see agentGateway), which is why the loop
// carries both.
func agentGRN() grn.GRN { return grn.New("acme", grn.DefaultProject, "agent", "0a1b2c3d4e5f6071") }

// emptyPollWait is how long the fake garam holds a poll that has
// nothing to answer with. A long poll is what keeps the loop from
// spinning, so the fake holds one too — briefly.
const emptyPollWait = 10 * time.Millisecond

// queued is one message in the fake queue and the hold that is out on
// it, if any.
type queued struct {
	id         string
	body       string
	claim      string
	expiresAt  time.Time
	deliveries int
	renewals   int
	completed  bool
}

// garamQueue is garam's queue over HTTP: claim, renewal and completion
// as api/machine.yaml describes them, in memory.
//
// The lease is its own and so is handing a message on: a claim nobody
// renewed lapses and the message is claimable again, which is what
// makes every lost completion here a redelivery rather than a loss. The
// ceiling renewals are held under is garam's too, and the tests below
// reach the same state through refuseRenewals.
type garamQueue struct {
	server *httptest.Server

	mu          sync.Mutex
	lease       time.Duration
	messages    []*queued
	claims      map[string]*queued
	refusing    bool
	refused     int
	completions int
}

func newGaramQueue(t *testing.T, lease time.Duration, bodies ...string) *garamQueue {
	t.Helper()
	q := &garamQueue{lease: lease, claims: map[string]*queued{}}
	for i, body := range bodies {
		q.messages = append(q.messages, &queued{id: fmt.Sprintf("message-%d", i), body: body})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agents/{agent}/claims", q.claim)
	mux.HandleFunc("POST /agents/{agent}/claims/{claim}/renewal", q.renew)
	mux.HandleFunc("POST /agents/{agent}/claims/{claim}/completion", q.complete)
	q.server = httptest.NewServer(mux)
	t.Cleanup(q.server.Close)
	return q
}

func (q *garamQueue) claim(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("agent") != agentGRN().String() {
		http.Error(w, "another agent's queue", http.StatusForbidden)
		return
	}
	q.mu.Lock()
	held := q.handOut()
	q.mu.Unlock()
	if held == nil {
		// The poll waited its time and found nothing (204), which the
		// caller answers by asking again.
		select {
		case <-time.After(emptyPollWait):
		case <-r.Context().Done():
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, held)
}

// handOut gives out the first message no live claim holds, and reports
// nothing when every message is completed or held.
func (q *garamQueue) handOut() *machinegen.Claim {
	for _, m := range q.messages {
		if m.completed || (m.claim != "" && time.Now().Before(m.expiresAt)) {
			continue
		}
		delete(q.claims, m.claim)
		m.claim = fmt.Sprintf("claim-%s-%d", m.id, m.deliveries)
		m.expiresAt = time.Now().Add(q.lease)
		m.deliveries++
		q.claims[m.claim] = m
		return &machinegen.Claim{
			Id:        m.claim,
			ExpiresAt: m.expiresAt,
			Attempt:   m.deliveries,
			Message:   machinegen.Message{Id: m.id, Body: m.body},
		}
	}
	return nil
}

func (q *garamQueue) renew(w http.ResponseWriter, r *http.Request) {
	q.mu.Lock()
	defer q.mu.Unlock()
	m, held := q.claims[r.PathValue("claim")]
	if !held || q.refusing || m.completed || time.Now().After(m.expiresAt) {
		q.refused++
		writeJSON(w, http.StatusConflict, machinegen.Error{Kind: "failed_precondition", Message: "claim is stale"})
		return
	}
	m.expiresAt = time.Now().Add(q.lease)
	m.renewals++
	writeJSON(w, http.StatusOK, machinegen.Claim{
		Id:        m.claim,
		ExpiresAt: m.expiresAt,
		Attempt:   m.deliveries,
		Message:   machinegen.Message{Id: m.id, Body: m.body},
	})
}

func (q *garamQueue) complete(w http.ResponseWriter, r *http.Request) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completions++
	m, held := q.claims[r.PathValue("claim")]
	if !held || time.Now().After(m.expiresAt) {
		writeJSON(w, http.StatusConflict, machinegen.Error{Kind: "failed_precondition", Message: "claim is stale"})
		return
	}
	m.completed = true
	delete(q.claims, m.claim)
	w.WriteHeader(http.StatusNoContent)
}

// refuseRenewals answers every renewal the way garam answers one for a
// hold that is gone, until allowRenewals restores it.
func (q *garamQueue) refuseRenewals() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.refusing = true
}

func (q *garamQueue) allowRenewals() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.refusing = false
}

// delivered is how many messages the queue holds as delivered.
func (q *garamQueue) delivered() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	completed := 0
	for _, m := range q.messages {
		if m.completed {
			completed++
		}
	}
	return completed
}

// completionCalls is how many times completion was called at all,
// refusals included. It is what tells an event that completed nothing
// from one the loop never acted on: a message the queue would refuse to
// complete still leaves a call here.
func (q *garamQueue) completionCalls() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.completions
}

// renewalsRefused is how many renewals the queue has answered the way
// it answers one for a hold that is gone.
func (q *garamQueue) renewalsRefused() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.refused
}

// renewalsOf is how often the hold on one message has been carried
// forward. It is the queue's own evidence that the adapter is holding
// the pairing for that message: the loop renews what it holds and
// nothing else, so a turn ending before this moves is a turn ending
// before the adapter knows what the gateway called the message.
func (q *garamQueue) renewalsOf(message int) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.messages[message].renewals
}

// deliveries is how many times the queue has handed its first message
// out. Above one is a redelivery.
func (q *garamQueue) deliveries() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.messages[0].deliveries
}

// agentGateway is the agent platform's gateway: it mints an identifier
// for every message it takes, and streams the agent's events. A turn is
// ended by the test rather than by a running agent, which is what makes
// the order of a post and a turn's end something a test decides.
type agentGateway struct {
	t      *testing.T
	server *httptest.Server

	mu          sync.Mutex
	taken       []string
	subscribers []chan string
}

// gatewayAgentID is what the gateway calls the agent. It is not garam's
// GRN: the gateway serves one agent under an identifier of its own and
// answers 404 for any other.
const gatewayAgentID = "default"

func newAgentGateway(t *testing.T) *agentGateway {
	t.Helper()
	g := &agentGateway{t: t}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agents/{agent}/messages", g.post)
	mux.HandleFunc("GET /agents/{agent}/events", g.events)
	g.server = httptest.NewServer(mux)
	t.Cleanup(g.server.Close)
	return g
}

func (g *agentGateway) post(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("agent") != gatewayAgentID {
		http.Error(w, "unknown agent", http.StatusNotFound)
		return
	}
	var submitted struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil || submitted.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}
	g.mu.Lock()
	id := fmt.Sprintf("msg-%d", len(g.taken))
	g.taken = append(g.taken, submitted.Content)
	g.mu.Unlock()
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "agent_id": gatewayAgentID, "status": "queued"})
}

func (g *agentGateway) events(w http.ResponseWriter, r *http.Request) {
	events := make(chan string, 32)
	g.mu.Lock()
	g.subscribers = append(g.subscribers, events)
	g.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		g.t.Errorf("the gateway's writer does not flush, so no event reaches the subscriber")
		return
	}
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()
		}
	}
}

// endTurn emits one terminal event naming the messages that turn
// drained, as the platform stamps them on a terminal event and on
// nothing else.
func (g *agentGateway) endTurn(outcome delivery.TurnOutcome, messages ...string) {
	event, err := json.Marshal(map[string]any{
		"agent_id":    gatewayAgentID,
		"kind":        string(outcome),
		"turn":        1,
		"message_ids": messages,
	})
	if err != nil {
		panic(err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, subscriber := range g.subscribers {
		subscriber <- string(event)
	}
}

// speak emits an event that is not a turn ending, which names nothing
// and completes nothing.
func (g *agentGateway) speak() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, subscriber := range g.subscribers {
		subscriber <- `{"agent_id":"default","kind":"tool_call","turn":1,"tool_name":"files"}`
	}
}

// taken is what the gateway has been handed, oldest first.
func (g *agentGateway) posted() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.taken...)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// brief is a configuration a test can wait through. Nothing about the
// loop changes; only how long it waits before trying again.
func brief() delivery.Config {
	return delivery.Config{RetryDelay: 5 * time.Millisecond, MinRenewInterval: 5 * time.Millisecond}
}

// run starts the loop against the two fakes and stops it when the test
// ends. It answers the context, so a test can end one process and start
// another against the same two fakes.
func run(t *testing.T, queue *garamQueue, gateway *agentGateway) context.CancelFunc {
	t.Helper()
	claims, err := delivery.NewHTTPQueue(queue.server.Client(), queue.server.URL, agentGRN())
	require.NoError(t, err)
	ingress, err := delivery.NewHTTPGateway(gateway.server.Client(), gateway.server.URL, gatewayAgentID)
	require.NoError(t, err)
	loop, err := delivery.NewService(claims, ingress, brief())
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		assert.NoError(t, loop.Run(ctx), "a requested stop is not a failure")
	}()
	t.Cleanup(func() {
		stop()
		<-stopped
	})
	return func() {
		stop()
		<-stopped
	}
}

// eventually is how long a test waits for the loop to get somewhere.
// Everything it waits on is a call between two processes on this
// machine, so the bound is generous and the interval is short.
func eventually(t *testing.T, condition func() bool, message string) {
	t.Helper()
	require.Eventually(t, condition, 2*time.Second, 2*time.Millisecond, message)
}

// holding waits until the adapter is holding the pairing for a message
// — until the hold on it has been carried forward once more than it had
// been. A turn that ends before that completes nothing, because the
// gateway's identifier for the message has not reached the loop yet;
// waiting on the renewal is waiting on the condition the completion
// depends on rather than on the post it followed.
func holding(t *testing.T, queue *garamQueue, message, renewals int) {
	t.Helper()
	eventually(t, func() bool { return queue.renewalsOf(message) > renewals },
		"the adapter holds the pairing for the message it posted")
}

// TestNewService_RefusesAConfigurationThatDeliversNothing holds the
// constructor to the values it can run under. The default configuration
// is the control: what follows is refused for the value it names rather
// than because nothing is accepted.
func TestNewService_RefusesAConfigurationThatDeliversNothing(t *testing.T) {
	queue := newGaramQueue(t, time.Second)
	gateway := newAgentGateway(t)
	claims, err := delivery.NewHTTPQueue(queue.server.Client(), queue.server.URL, agentGRN())
	require.NoError(t, err)
	ingress, err := delivery.NewHTTPGateway(gateway.server.Client(), gateway.server.URL, gatewayAgentID)
	require.NoError(t, err)

	_, err = delivery.NewService(claims, ingress, delivery.Config{})
	require.NoError(t, err, "the defaults run")

	tests := map[string]delivery.Config{
		"a negative retry delay":    {RetryDelay: -time.Second},
		"a negative renew interval": {MinRenewInterval: -time.Second},
	}
	for name, cfg := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := delivery.NewService(claims, ingress, cfg)
			require.Error(t, err)
		})
	}

	_, err = delivery.NewService(nil, ingress, delivery.Config{})
	require.Error(t, err, "a loop with no queue carries nothing")
	_, err = delivery.NewService(claims, nil, delivery.Config{})
	require.Error(t, err, "a loop with no gateway carries nothing")
}

// TestRun_CompletesTheClaimATurnSucceededOnAndNamedIt is the loop as
// docs/architecture/adapter.md states it: claimed, posted, and
// completed by the turn that succeeded and named the message.
//
// The two assertions before the terminal event are what say it is that
// event and nothing else: a message the gateway has taken is not
// completed, which is the design that completes on the 202 (ADR-0018
// rejected it), and neither is an event that names no message.
func TestRun_CompletesTheClaimATurnSucceededOnAndNamedIt(t *testing.T) {
	queue := newGaramQueue(t, 200*time.Millisecond, "what the sender said")
	gateway := newAgentGateway(t)
	run(t, queue, gateway)

	eventually(t, func() bool { return len(gateway.posted()) == 1 }, "the message reaches the gateway")
	assert.Equal(t, []string{"what the sender said"}, gateway.posted())
	holding(t, queue, 0, 0)
	assert.Zero(t, queue.completionCalls(), "a message the gateway took is not a message a turn handled")

	// The events arrive in the order they are emitted, so the
	// completion below is proof that the one above was read and
	// completed nothing.
	gateway.speak()
	gateway.endTurn(delivery.TurnFinished, "msg-0")
	eventually(t, func() bool { return queue.delivered() == 1 }, "the turn that named it completes it")
	assert.Equal(t, 1, queue.completionCalls(), "an event that is not a turn ending completes nothing")
	assert.Equal(t, 1, queue.deliveries(), "a completed message is not handed out again")
}

// TestRun_LeavesAFailedTurnsMessageForRedelivery is §Failure and
// attempts: a turn that fails is an attempt rather than a completion,
// so nothing is completed and the message comes back once the lease
// lapses. The successful turn on the same message is the control — the
// outcome is what decides it, not the machinery around it.
func TestRun_LeavesAFailedTurnsMessageForRedelivery(t *testing.T) {
	queue := newGaramQueue(t, 200*time.Millisecond, "what the sender said")
	gateway := newAgentGateway(t)
	run(t, queue, gateway)

	eventually(t, func() bool { return len(gateway.posted()) == 1 }, "the message reaches the gateway")
	holding(t, queue, 0, 0)
	gateway.endTurn(delivery.TurnFailed, "msg-0")

	eventually(t, func() bool { return len(gateway.posted()) >= 2 }, "the message is delivered again")
	assert.GreaterOrEqual(t, queue.deliveries(), 2, "the lease lapsed and the message was handed out again")

	held := queue.renewalsOf(0)
	holding(t, queue, 0, held)
	gateway.endTurn(delivery.TurnFinished, "msg-1")
	eventually(t, func() bool { return queue.delivered() == 1 }, "the turn that succeeded completes it")
	assert.Equal(t, 1, queue.completionCalls(), "the turn that failed completed nothing")
}

// TestRun_DropsAClaimWhoseLeaseLapsedMidTurn is §The lease from the
// other side: garam refuses the renewal, so the hold is gone while the
// turn is still running and the turn that later succeeds completes
// nothing. The message completed before it and the redelivery
// completed after it are the controls — the same terminal event
// completes a message whose hold stands, on both sides of the one that
// does not.
func TestRun_DropsAClaimWhoseLeaseLapsedMidTurn(t *testing.T) {
	queue := newGaramQueue(t, 200*time.Millisecond, "first", "second")
	gateway := newAgentGateway(t)
	run(t, queue, gateway)

	eventually(t, func() bool { return len(gateway.posted()) >= 2 }, "both messages reach the gateway")
	holding(t, queue, 0, 0)
	holding(t, queue, 1, 0)
	gateway.endTurn(delivery.TurnFinished, "msg-0")
	eventually(t, func() bool { return queue.delivered() == 1 }, "a hold that stands completes")

	// The hold on the second message goes while its turn is still
	// running, which is the state a renewal garam refuses leaves.
	queue.refuseRenewals()
	eventually(t, func() bool { return queue.renewalsRefused() > 0 }, "the renewal garam refuses is made")
	queue.allowRenewals()

	gateway.endTurn(delivery.TurnFinished, "msg-1")
	eventually(t, func() bool { return len(gateway.posted()) >= 3 }, "the message whose hold lapsed is delivered again")
	holding(t, queue, 1, queue.renewalsOf(1))
	gateway.endTurn(delivery.TurnFinished, "msg-2")
	eventually(t, func() bool { return queue.delivered() == 2 }, "the delivery that follows completes")
	assert.Equal(t, 2, queue.completionCalls(), "the claim that lapsed was not completed")
}

// TestRun_RedeliversAMessagePostedBeforeARestart is what
// docs/architecture/adapter.md means by holding the pairing in memory:
// a process that dies between the post and the completion takes the
// pairing with it, so the turn it started completes nothing and garam
// hands the message to the process that follows.
func TestRun_RedeliversAMessagePostedBeforeARestart(t *testing.T) {
	queue := newGaramQueue(t, 200*time.Millisecond, "what the sender said")
	gateway := newAgentGateway(t)
	stop := run(t, queue, gateway)

	eventually(t, func() bool { return len(gateway.posted()) == 1 }, "the message reaches the gateway")
	holding(t, queue, 0, 0)
	held := queue.renewalsOf(0)
	stop()

	// The turn the stopped process started ends, and nothing holds its
	// pairing any more.
	gateway.endTurn(delivery.TurnFinished, "msg-0")

	run(t, queue, gateway)
	eventually(t, func() bool { return len(gateway.posted()) >= 2 }, "the new process is handed the same message")
	assert.Equal(t, "what the sender said", gateway.posted()[1],
		"the agent sees the same message twice, which is what at-least-once means here")
	assert.Zero(t, queue.completionCalls(), "the pairing died with the process that made it")

	// The new process knows nothing of what the old one posted: the
	// identifier of the first delivery completes nothing, which the
	// completion of the second — emitted after it on one stream — is
	// what proves.
	holding(t, queue, 0, held)
	gateway.endTurn(delivery.TurnFinished, "msg-0")
	gateway.endTurn(delivery.TurnFinished, "msg-1")
	eventually(t, func() bool { return queue.delivered() == 1 }, "the pairing this process made completes it")
	assert.Equal(t, 1, queue.completionCalls(), "a message this process never posted is not completed")
}
