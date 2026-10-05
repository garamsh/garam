// Package delivery_test drives the loop against a garam, an agent
// platform and a lifecycle authority that are fakes: what garam and the
// platform answer over HTTP is what api/machine.yaml and the platform's
// gateway answer, so the loop is exercised through the clients it
// really uses rather than past them.
package delivery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// agentGRN is the GRN garam and the runtime both know the agent by.
func agentGRN() grn.GRN { return grn.New("acme", grn.DefaultProject, "agent", "0a1b2c3d4e5f6071") }

// senderGRN is who sent every message the fake queue holds, as garam's
// claim verified it.
func senderGRN() grn.GRN { return grn.New("acme", grn.DefaultProject, "agent", "9f8e7d6c5b4a3021") }

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

// garamQueue is garam's machine listener over HTTP: claim, renewal and
// completion as api/machine.yaml describes them, the outbox route, and
// the execution fence over all of them, in memory.
//
// The lease is its own and so is handing a message on: a claim nobody
// renewed lapses and the message is claimable again, which is what
// makes every lost completion here a redelivery rather than a loss. The
// ceiling renewals are held under is garam's too, and the tests below
// reach the same state through refuseRenewals.
type garamQueue struct {
	server *httptest.Server

	mu       sync.Mutex
	lease    time.Duration
	messages []*queued
	claims   map[string]*queued
	refusing bool
	refused  int
	// hangFirstCompletion parks the first completion until the test
	// ends, which is the stalled peer with hold's one goroutine on the
	// other side of it.
	hangFirstCompletion chan struct{}
	completionCallsSeen int
	claimCallsSeen      int
	claimsToRefuse      int
	claimsRefusedSeen   int
	renewals            int
	completions         int
	// token is the one execution token garam admits, empty for an agent
	// never cut over, which is admitted presenting none.
	token string
	// presented is every Authorization a claim carried, in order, and
	// fenced how many claims the fence refused.
	presented []string
	fenced    int
	// outbox is the outbox route's state.
	outbox outboxRoute
}

// outboxRoute is `POST /outbox-messages` under durable-delivery.v1: a
// submission is stored once per key and its replay answers the stored
// message. answer overrides what one submission is answered with.
type outboxRoute struct {
	submitted []outboxSubmission
	stored    map[string]machinegen.Message
	answer    func(outboxSubmission) (status int, version string, body any)
	// legacyPosts counts posts to `POST /messages`, which an outbox
	// reply must never fall back to.
	legacyPosts int
}

// outboxSubmission is what the route was sent.
type outboxSubmission struct {
	Recipient      string    `json:"recipient"`
	IdempotencyKey string    `json:"idempotencyKey"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"createdAt"`
	Authorization  string    `json:"-"`
}

func newGaramQueue(t *testing.T, lease time.Duration, bodies ...string) *garamQueue {
	t.Helper()
	q := &garamQueue{lease: lease, claims: map[string]*queued{}}
	q.outbox.stored = map[string]machinegen.Message{}
	for i, body := range bodies {
		q.messages = append(q.messages, &queued{id: fmt.Sprintf("message-%d", i), body: body})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agents/{agent}/claims", q.claim)
	mux.HandleFunc("POST /agents/{agent}/claims/{claim}/renewal", q.renew)
	mux.HandleFunc("POST /agents/{agent}/claims/{claim}/completion", q.complete)
	mux.HandleFunc("POST /outbox-messages", q.submit)
	mux.HandleFunc("POST /messages", func(w http.ResponseWriter, _ *http.Request) {
		q.mu.Lock()
		q.outbox.legacyPosts++
		q.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	})
	q.server = httptest.NewServer(mux)
	t.Cleanup(q.server.Close)
	return q
}

// admits reports whether garam admits what a request presents, and
// answers the refusal it would write otherwise. The caller holds mu.
func (q *garamQueue) admits(w http.ResponseWriter, r *http.Request) bool {
	got := r.Header.Get("Authorization")
	switch {
	case q.token == "" && got == "":
		return true
	case q.token == "":
		writeJSON(w, http.StatusConflict, machinegen.Error{Kind: "failed_precondition", Message: "agent has not cut over to activated execution"})
	case got == "":
		writeJSON(w, http.StatusUnauthorized, machinegen.Error{Kind: "unauthenticated", Message: "execution token required"})
	case got != "Garam-Execution "+q.token:
		writeJSON(w, http.StatusForbidden, machinegen.Error{Kind: "permission_denied", Message: "execution token is not current"})
	default:
		return true
	}
	return false
}

// admit makes token the one garam admits, as an activation does.
func (q *garamQueue) admit(token string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.token = token
}

func (q *garamQueue) claim(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("agent") != agentGRN().String() {
		writeJSON(w, http.StatusForbidden, machinegen.Error{Kind: "permission_denied", Message: "caller is not the recipient"})
		return
	}
	q.mu.Lock()
	q.claimCallsSeen++
	q.presented = append(q.presented, r.Header.Get("Authorization"))
	if !q.admits(w, r) {
		q.fenced++
		q.mu.Unlock()
		return
	}
	if q.claimsToRefuse > 0 {
		q.claimsToRefuse--
		q.claimsRefusedSeen++
		q.mu.Unlock()
		http.Error(w, "garam is not answering", http.StatusInternalServerError)
		return
	}
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
		return q.claimOf(m)
	}
	return nil
}

func (q *garamQueue) claimOf(m *queued) *machinegen.Claim {
	return &machinegen.Claim{
		Id:        m.claim,
		ExpiresAt: m.expiresAt,
		Attempt:   m.deliveries,
		Message: machinegen.Message{
			Id: m.id, Sender: senderGRN().String(), Recipient: agentGRN().String(), Body: m.body,
		},
	}
}

func (q *garamQueue) renew(w http.ResponseWriter, r *http.Request) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.renewals++
	if !q.admits(w, r) {
		return
	}
	m, held := q.claims[r.PathValue("claim")]
	if !held || q.refusing || m.completed || time.Now().After(m.expiresAt) {
		q.refused++
		writeJSON(w, http.StatusConflict, machinegen.Error{Kind: "failed_precondition", Message: "claim is stale"})
		return
	}
	m.expiresAt = time.Now().Add(q.lease)
	m.renewals++
	writeJSON(w, http.StatusOK, q.claimOf(m))
}

func (q *garamQueue) complete(w http.ResponseWriter, r *http.Request) {
	q.mu.Lock()
	q.completionCallsSeen++
	hang := q.hangFirstCompletion
	first := q.completionCallsSeen == 1
	q.mu.Unlock()
	if hang != nil && first {
		// Accepted and never answered. Only the caller's own deadline
		// ends this, and before #747 there was none.
		select {
		case <-hang:
		case <-r.Context().Done():
		}
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.admits(w, r) {
		return
	}
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

// submit is the outbox route: a new key is stored and answered 201, and
// a stored key is answered with what was stored.
func (q *garamQueue) submit(w http.ResponseWriter, r *http.Request) {
	var sub outboxSubmission
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
		writeJSON(w, http.StatusBadRequest, machinegen.Error{Kind: "invalid_argument", Message: "body"})
		return
	}
	sub.Authorization = r.Header.Get("Authorization")
	q.mu.Lock()
	defer q.mu.Unlock()
	if r.Header.Get("Garam-Contract-Version") != delivery.DeliveryContract {
		writeJSON(w, http.StatusBadRequest, machinegen.Error{Kind: "invalid_argument", Message: "version"})
		return
	}
	if !q.admits(w, r) {
		return
	}
	q.outbox.submitted = append(q.outbox.submitted, sub)
	if q.outbox.answer != nil {
		status, version, body := q.outbox.answer(sub)
		if version != "" {
			w.Header().Set("Garam-Contract-Version", version)
		}
		writeJSON(w, status, body)
		return
	}
	stored, ok := q.outbox.stored[sub.IdempotencyKey]
	if !ok {
		stored = machinegen.Message{
			Id: "stored-" + sub.IdempotencyKey, Sender: agentGRN().String(), Recipient: sub.Recipient,
			IdempotencyKey: sub.IdempotencyKey, Body: sub.Body, CreatedAt: time.Now(),
		}
		q.outbox.stored[sub.IdempotencyKey] = stored
	}
	w.Header().Set("Garam-Contract-Version", delivery.DeliveryContract)
	writeJSON(w, http.StatusCreated, stored)
}

// submissions is every submission the outbox route took, in order.
func (q *garamQueue) submissions() []outboxSubmission {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]outboxSubmission(nil), q.outbox.submitted...)
}

// answerOutbox makes every later submission answered by answer.
func (q *garamQueue) answerOutbox(answer func(outboxSubmission) (int, string, any)) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.outbox.answer = answer
}

func (q *garamQueue) legacyPosts() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.outbox.legacyPosts
}

// claimsPresenting is every Authorization the claims carried.
func (q *garamQueue) claimsPresenting() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.presented...)
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

// refuseClaims makes the next n polls fail as a garam that is not
// answering does. Nothing is handed out on a refused poll, so nothing
// is lost by one.
func (q *garamQueue) refuseClaims(n int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claimsToRefuse = n
}

// claimsRefused is how many polls the queue has failed.
func (q *garamQueue) claimsRefused() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claimsRefusedSeen
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

// completionCalls is how many times completion was called and admitted,
// refusals of the hold included. It is what tells an event that
// completed nothing from one the loop never acted on: a message the
// queue would refuse to complete still leaves a call here.
func (q *garamQueue) completionCalls() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.completions
}

// hangFirstCompletionCall parks the first completion until the test
// ends. hold calls complete inline, so before #747 that stopped every
// completion behind it.
func (q *garamQueue) hangFirstCompletionCall(t *testing.T) {
	t.Helper()
	stop := make(chan struct{})
	q.mu.Lock()
	q.hangFirstCompletion = stop
	q.mu.Unlock()
	t.Cleanup(func() { close(stop) })
}

func (q *garamQueue) renewalsAttempted() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.renewals
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
// before the adapter holds the message.
func (q *garamQueue) renewalsOf(message int) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.messages[message].renewals
}

// claimsAttempted is how many times the loop has asked for work. Claim,
// post and the handoff of the pairing are one sequence in one goroutine,
// so a claim beyond the messages already posted is the queue's own
// evidence that every pairing has reached hold — which the post is not,
// since the gateway records a message before the loop has its answer.
func (q *garamQueue) claimsAttempted() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claimCallsSeen
}

// deliveries is how many times the queue has handed one message out.
// Above one is a redelivery.
func (q *garamQueue) deliveries(message int) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.messages[message].deliveries
}

// agentGateway is the agent platform's gateway: it keeps every message
// under the caller's identifier for it, answers a replay of one it
// already completed as completed, refuses one reused with other
// content, and streams the agent's events. A turn is ended by the test
// rather than by a running agent, which is what makes the order of a
// post and a turn's end something a test decides.
type agentGateway struct {
	t      *testing.T
	server *httptest.Server

	mu sync.Mutex
	// taken is every post the gateway answered, oldest first, and
	// sources the messages it took as new, by identifier.
	taken     []posted
	sources   map[string]string
	order     []string
	completed map[string]bool
	// loseAnswers is how many accepted posts lose their answer.
	loseAnswers int
	status      map[string]any
	subscribers []chan string
	opened      int
	// closed refuses every new subscription while it is set.
	closed bool
}

// posted is one post the gateway took.
type posted struct {
	id      string
	content string
}

func newAgentGateway(t *testing.T) *agentGateway {
	t.Helper()
	g := &agentGateway{
		t: t, sources: map[string]string{}, completed: map[string]bool{},
		status: map[string]any{
			"contract_version": 1, "agent_id": agentGRN().String(), "assignment_epoch": "3",
			"generation": strings.Repeat("a", 32), "config_revision": "7", "state": "serving",
			"started_at": "2026-10-05T01:02:03Z",
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agents/{agent}/messages", g.post)
	mux.HandleFunc("GET /agents/{agent}/messages/{id}", g.lookup)
	mux.HandleFunc("GET /agents/{agent}/events", g.events)
	mux.HandleFunc("GET /agents/{agent}/status", g.statusOf)
	g.server = httptest.NewServer(mux)
	t.Cleanup(g.server.Close)
	return g
}

func (g *agentGateway) post(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("agent") != agentGRN().String() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown agent"})
		return
	}
	var submitted struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&submitted); err != nil || submitted.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid message body"})
		return
	}
	g.mu.Lock()
	held, known := g.sources[submitted.ID]
	switch {
	case known && held != submitted.Content:
		g.mu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "id reused with different content"})
		return
	case !known:
		g.sources[submitted.ID] = submitted.Content
		g.order = append(g.order, submitted.ID)
	}
	g.taken = append(g.taken, posted{id: submitted.ID, content: submitted.Content})
	done := g.completed[submitted.ID]
	lose := g.loseAnswers > 0
	if lose {
		g.loseAnswers--
	}
	g.mu.Unlock()
	if lose {
		// Taken, and the answer never reaches the caller.
		hijacker, ok := w.(http.Hijacker)
		require.True(g.t, ok)
		conn, _, err := hijacker.Hijack()
		require.NoError(g.t, err)
		_ = conn.Close()
		return
	}
	if done {
		writeJSON(w, http.StatusOK, map[string]string{"id": submitted.ID, "status": "completed"})
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"id": submitted.ID, "status": "queued"})
}

func (g *agentGateway) lookup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g.mu.Lock()
	defer g.mu.Unlock()
	status := "unknown"
	if _, known := g.sources[id]; known {
		status = "pending"
	}
	if g.completed[id] {
		status = "completed"
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "status": status})
}

func (g *agentGateway) statusOf(w http.ResponseWriter, _ *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	writeJSON(w, http.StatusOK, g.status)
}

// report sets one field of what the runtime's status answers.
func (g *agentGateway) report(field string, value any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.status[field] = value
}

func (g *agentGateway) events(w http.ResponseWriter, r *http.Request) {
	events := make(chan string, 32)
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "not serving"})
		return
	}
	g.subscribers = append(g.subscribers, events)
	g.opened++
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
		case event, live := <-events:
			if !live {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()
		}
	}
}

// endTurn emits one terminal event naming the messages that turn
// drained, as the platform stamps them on a terminal event and on
// nothing else. A turn that finished committed its messages first, so a
// replay of one answers completed; one that failed forgot them, so a
// replay is taken again.
func (g *agentGateway) endTurn(outcome delivery.TurnOutcome, messages ...string) {
	event, err := json.Marshal(map[string]any{
		"agent_id":    agentGRN().String(),
		"kind":        string(outcome),
		"turn":        1,
		"message_ids": messages,
	})
	if err != nil {
		panic(err)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, id := range messages {
		if outcome == delivery.TurnFinished {
			g.completed[id] = true
		} else {
			delete(g.sources, id)
		}
	}
	for _, subscriber := range g.subscribers {
		subscriber <- string(event)
	}
}

// dropStreams ends every open subscription the way a gateway that lost
// the connection does, leaving whoever was reading to open another.
func (g *agentGateway) dropStreams() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, subscriber := range g.subscribers {
		close(subscriber)
	}
	g.subscribers = nil
}

// closeStreams refuses new subscriptions until reopenStreams.
func (g *agentGateway) closeStreams(closed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = closed
}

// subscriptions is how many streams have been opened in all. A second
// one is the reader's answer to the first ending.
func (g *agentGateway) subscriptions() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.opened
}

// speak emits an event that is not a turn ending, which names nothing
// and completes nothing.
func (g *agentGateway) speak() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, subscriber := range g.subscribers {
		subscriber <- `{"kind":"tool_call","turn":1,"tool_name":"files"}`
	}
}

// posted is every post the gateway took, oldest first.
func (g *agentGateway) posted() []posted {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]posted(nil), g.taken...)
}

// sourceCount is how many distinct messages the runtime holds.
func (g *agentGateway) sourceCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.order)
}

// loseNextAnswers makes the next n accepted posts lose their answer.
func (g *agentGateway) loseNextAnswers(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.loseAnswers = n
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// mounted is what the deployment mounted: the placement token and the
// serial of the agent certificate, either replaceable mid-test.
type mounted struct {
	mu     sync.Mutex
	token  string
	serial string
}

func newMounted() *mounted { return &mounted{token: "placement-1", serial: "1001"} }

func (m *mounted) PlacementToken() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.token, nil
}

func (m *mounted) LeafSerial() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.serial, nil
}

func (m *mounted) replace(token, serial string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.token, m.serial = token, serial
}

// expected is the content garam-message.v1 gives a body the fake queue
// holds, written out rather than computed so the encoding is pinned
// here and not only where it is produced.
func expected(body string) string {
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return `{"contract":"garam-message.v1","sender":"` + senderGRN().String() + `","body":` + string(encoded) + `}`
}

// brief is a configuration a test can wait through. Nothing about the
// loop changes; only how long it waits before trying again.
func brief() delivery.Config {
	return delivery.Config{
		RetryDelay: 5 * time.Millisecond, MinRenewInterval: 5 * time.Millisecond,
		StatusInterval: 5 * time.Millisecond, OutboxInterval: 5 * time.Millisecond,
		MaxRetryDelay: 20 * time.Millisecond,
	}
}

// legacyEnds is the loop of an agent never cut over, against the two
// fakes, forwarding outbox when it is not nil.
func legacyEnds(t *testing.T, queue *garamQueue, gateway *agentGateway, outbox *delivery.Outbox) delivery.Ends {
	t.Helper()
	execution := delivery.NewLegacyExecution()
	return ends(t, queue, gateway, execution, nil, newMounted(), outbox)
}

// ends assembles the loop's ends over the fakes, through the HTTP
// clients the adapter really uses.
func ends(t *testing.T, queue *garamQueue, gateway *agentGateway, execution *delivery.Execution,
	authority delivery.Authority, credentials delivery.Credentials, outbox *delivery.Outbox,
) delivery.Ends {
	t.Helper()
	claims, err := delivery.NewHTTPQueue(queue.server.Client(), queue.server.URL, agentGRN(), execution)
	require.NoError(t, err)
	ingress, err := delivery.NewHTTPGateway(gateway.server.Client(), gateway.server.URL, agentGRN())
	require.NoError(t, err)
	return delivery.Ends{
		Agent: agentGRN(), Queue: claims, Gateway: ingress, Execution: execution,
		Credentials: credentials, Authority: authority, Outbox: outbox,
	}
}

// run starts the legacy loop against the two fakes and stops it when
// the test ends. It answers the stop, so a test can end one process and
// start another against the same two fakes.
func run(t *testing.T, queue *garamQueue, gateway *agentGateway) context.CancelFunc {
	t.Helper()
	return runWith(t, queue, gateway, brief())
}

// runWith is run with the config named, so a test can bound the calls
// more tightly than the defaults do.
func runWith(t *testing.T, queue *garamQueue, gateway *agentGateway, cfg delivery.Config) context.CancelFunc {
	t.Helper()
	return start(t, legacyEnds(t, queue, gateway, nil), cfg)
}

// start runs a loop over ends until the test ends or the answer is
// called.
func start(t *testing.T, e delivery.Ends, cfg delivery.Config) context.CancelFunc {
	t.Helper()
	loop, err := delivery.NewService(e, cfg)
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		assert.NoError(t, loop.Run(ctx), "a requested stop is not a failure")
	}()
	var once sync.Once
	halt := func() {
		once.Do(func() {
			stop()
			<-stopped
		})
	}
	t.Cleanup(halt)
	return halt
}

// eventually is how long a test waits for the loop to get somewhere.
// The bound is what the loop has to do rather than what the machine is
// doing: the longest wait here is a lease lapsing and the renewal
// inside it, no test waits on a lease longer than 200ms, and two
// seconds leaves an order of magnitude over that for a runner not
// giving this process the CPU. A condition that wants more is not slow
// but unreachable — #852 was one the loop would never reach, and no
// bound is long enough for that.
func eventually(t *testing.T, condition func() bool, message string) {
	t.Helper()
	require.Eventually(t, condition, 2*time.Second, 2*time.Millisecond, message)
}

// holding waits until the adapter is holding the pairing for a message
// — until the hold on it has been carried forward once more than it had
// been. A turn that ends before that completes nothing, because the
// pairing has not reached the loop yet; waiting on the renewal is
// waiting on the condition the completion depends on rather than on the
// post it followed.
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
	working := legacyEnds(t, queue, gateway, nil)

	_, err := delivery.NewService(working, delivery.Config{})
	require.NoError(t, err, "the defaults run")

	configs := map[string]delivery.Config{
		"a negative retry delay":      {RetryDelay: -time.Second},
		"a retry cap under the delay": {RetryDelay: time.Second, MaxRetryDelay: time.Millisecond},
		"a negative renew interval":   {MinRenewInterval: -time.Second},
		"a negative status interval":  {StatusInterval: -time.Second},
		"a negative outbox interval":  {OutboxInterval: -time.Second},
		"a negative call bound":       {CallTimeout: -time.Second},
		"a negative claim bound":      {ClaimTimeout: -time.Second},
	}
	for name, cfg := range configs {
		t.Run(name, func(t *testing.T) {
			_, err := delivery.NewService(working, cfg)
			require.Error(t, err)
		})
	}

	outbox, err := delivery.NewOutbox(t.TempDir())
	require.NoError(t, err)
	fenced := ends(t, queue, gateway, delivery.NewFencedExecution(), newControl(queue), newMounted(), outbox)
	_, err = delivery.NewService(fenced, delivery.Config{})
	require.NoError(t, err, "the fenced control: an authority and an outbox beside it")

	broken := map[string]func(e *delivery.Ends){
		"no queue":                             func(e *delivery.Ends) { e.Queue = nil },
		"no gateway":                           func(e *delivery.Ends) { e.Gateway = nil },
		"no credentials":                       func(e *delivery.Ends) { e.Credentials = nil },
		"no agent":                             func(e *delivery.Ends) { e.Agent = grn.GRN{} },
		"a fenced execution with no authority": func(e *delivery.Ends) { e.Authority = nil },
		"a fenced execution with no outbox":    func(e *delivery.Ends) { e.Outbox = nil },
		"a legacy execution with an authority": func(e *delivery.Ends) { e.Execution = delivery.NewLegacyExecution() },
	}
	for name, breaks := range broken {
		t.Run(name, func(t *testing.T) {
			e := fenced
			breaks(&e)
			_, err := delivery.NewService(e, delivery.Config{})
			require.Error(t, err)
		})
	}
}

// TestRun_CompletesTheClaimATurnSucceededOnAndNamedIt is the loop as
// docs/architecture/adapter.md states it: claimed, posted under garam's
// identifier as garam-message.v1, and completed by the turn that
// succeeded and named the message.
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
	assert.Equal(t, []posted{{id: "message-0", content: expected("what the sender said")}}, gateway.posted(),
		"garam's message identifier, never the claim's, and the sender garam verified")
	holding(t, queue, 0, 0)
	assert.Zero(t, queue.completionCalls(), "a message the gateway took is not a message a turn handled")

	// The events arrive in the order they are emitted, so the
	// completion below is proof that the one above was read and
	// completed nothing.
	gateway.speak()
	gateway.endTurn(delivery.TurnFinished, "message-0")
	eventually(t, func() bool { return queue.delivered() == 1 }, "the turn that named it completes it")
	assert.Equal(t, 1, queue.completionCalls(), "an event that is not a turn ending completes nothing")
	assert.Equal(t, 1, queue.deliveries(0), "a completed message is not handed out again")
	assert.Equal(t, []string{""}, queue.claimsPresenting()[:1], "an agent never cut over presents no token")
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
	gateway.endTurn(delivery.TurnFailed, "message-0")

	eventually(t, func() bool { return len(gateway.posted()) >= 2 }, "the message is delivered again")
	assert.GreaterOrEqual(t, queue.deliveries(0), 2, "the lease lapsed and the message was handed out again")

	held := queue.renewalsOf(0)
	holding(t, queue, 0, held)
	gateway.endTurn(delivery.TurnFinished, "message-0")
	eventually(t, func() bool { return queue.delivered() == 1 }, "the turn that succeeded completes it")
	assert.Equal(t, 1, queue.completionCalls(), "the turn that failed completed nothing")
}

// TestRun_KeepsPollingAfterAPollThatFailed covers the loop's answer to
// a garam that is not there. Nothing was claimed, so nothing is lost,
// and the loop waits and asks again. A loop that ended its polling on
// the failure leaves the adapter up and delivering nothing: the process
// stays healthy, the queue drains never, and no exit says why.
//
// The count of refused polls is the control — without it this passes on
// a queue that never failed one.
func TestRun_KeepsPollingAfterAPollThatFailed(t *testing.T) {
	queue := newGaramQueue(t, time.Second, "first")
	gateway := newAgentGateway(t)
	queue.refuseClaims(2)
	run(t, queue, gateway)

	eventually(t, func() bool { return len(gateway.posted()) == 1 },
		"the loop asked again past the polls that failed")
	assert.Equal(t, 2, queue.claimsRefused(), "and the polls it asked past did fail")
}

// TestRun_ResubscribesAfterTheStreamDrops covers the other loop's
// answer to the same thing: a gateway stream that ends under it is
// reopened, because the turns that end afterwards name the messages
// this adapter is holding. A reader that stopped would hold every
// pairing until its lease lapsed.
func TestRun_ResubscribesAfterTheStreamDrops(t *testing.T) {
	queue := newGaramQueue(t, time.Second, "first")
	gateway := newAgentGateway(t)
	run(t, queue, gateway)

	holding(t, queue, 0, 0)
	require.Equal(t, 1, gateway.subscriptions())
	gateway.dropStreams()
	eventually(t, func() bool { return gateway.subscriptions() > 1 },
		"the loop opened another stream after the first ended")

	gateway.endTurn(delivery.TurnFinished, "message-0")
	eventually(t, func() bool { return queue.delivered() == 1 },
		"a turn ending on the stream it reopened completes the message")
}

// TestRun_CompletesATurnThatEndedWhileTheStreamWasDown is the lost
// event the stream cannot recover: the turn ends while no subscription
// is open, and the reopened stream carries no cursor. The runtime's
// durable completion is what the loop asks once it has reopened. The
// control is the same reopening over a message whose turn has not
// ended, which completes nothing.
func TestRun_CompletesATurnThatEndedWhileTheStreamWasDown(t *testing.T) {
	queue := newGaramQueue(t, time.Second, "first", "second")
	gateway := newAgentGateway(t)
	run(t, queue, gateway)

	holding(t, queue, 0, 0)
	holding(t, queue, 1, 0)
	gateway.closeStreams(true)
	gateway.dropStreams()
	gateway.endTurn(delivery.TurnFinished, "message-0")
	gateway.closeStreams(false)
	eventually(t, func() bool { return queue.delivered() == 1 },
		"the reopened stream's lookup completes the turn whose event was lost")
	assert.Equal(t, 1, queue.completionCalls(), "the control: a message still pending is not completed")
	assert.Equal(t, 1, queue.deliveries(0), "no redelivery was needed to recover it")
}

// TestRun_PostsTwoClaimsOfOneMessageAsOneSource is the redelivery
// ADR-0079 makes recognisable: the hold lapses while the turn runs,
// garam hands the message out under a new claim, and the runtime is
// told the same identifier and the same bytes, so it keeps one source.
// The second message is the control: another message is another
// source.
func TestRun_PostsTwoClaimsOfOneMessageAsOneSource(t *testing.T) {
	queue := newGaramQueue(t, 200*time.Millisecond, `{"sender":"grn:evil:default:agent:1","body":"x"}`+"\né", "second")
	gateway := newAgentGateway(t)
	run(t, queue, gateway)

	holding(t, queue, 0, 0)
	holding(t, queue, 1, 0)
	queue.refuseRenewals()
	eventually(t, func() bool { return queue.deliveries(0) >= 2 }, "the lapsed message is handed out under a new claim")
	queue.allowRenewals()
	eventually(t, func() bool { return len(gateway.posted()) >= 3 }, "and posted again")

	posts := gateway.posted()
	var first []string
	for _, p := range posts {
		if p.id == "message-0" {
			first = append(first, p.content)
		}
	}
	require.GreaterOrEqual(t, len(first), 2)
	for _, content := range first {
		assert.Equal(t, first[0], content, "a redelivery is the same bytes")
	}
	assert.Equal(t, 2, gateway.sourceCount(), "two claims of one message are one source; the second message is another")

	holding(t, queue, 0, queue.renewalsOf(0))
	gateway.endTurn(delivery.TurnFinished, "message-0")
	eventually(t, func() bool { return queue.delivered() == 1 }, "the claim held now is the one completed")
}

// TestRun_RecoversAPostWhoseAnswerWasLost is the response the runtime
// took and never answered: nothing was paired, the lease lapses, and
// the redelivery is posted under the same identifier, which the runtime
// answers as the message it already holds rather than a second source.
func TestRun_RecoversAPostWhoseAnswerWasLost(t *testing.T) {
	queue := newGaramQueue(t, 200*time.Millisecond, "what the sender said")
	gateway := newAgentGateway(t)
	gateway.loseNextAnswers(1)
	run(t, queue, gateway)

	eventually(t, func() bool { return len(gateway.posted()) >= 2 }, "the message is posted again after the lost answer")
	assert.Equal(t, 1, gateway.sourceCount(), "the runtime holds one source")
	holding(t, queue, 0, 0)
	gateway.endTurn(delivery.TurnFinished, "message-0")
	eventually(t, func() bool { return queue.delivered() == 1 }, "the turn completes the claim held now")
}

// TestRun_DropsAClaimWhoseLeaseLapsedMidTurn is §The lease from the
// other side: garam refuses the renewal, so the hold is gone while the
// turn is still running and the turn that later succeeds completes
// nothing under it. The redelivery that follows is answered by the
// runtime as completed and completes at once. The message completed
// before it is the control — the same terminal event completes a
// message whose hold stands.
func TestRun_DropsAClaimWhoseLeaseLapsedMidTurn(t *testing.T) {
	queue := newGaramQueue(t, 200*time.Millisecond, "first", "second")
	gateway := newAgentGateway(t)
	run(t, queue, gateway)

	eventually(t, func() bool { return len(gateway.posted()) >= 2 }, "both messages reach the gateway")
	holding(t, queue, 0, 0)
	holding(t, queue, 1, 0)
	gateway.endTurn(delivery.TurnFinished, "message-0")
	eventually(t, func() bool { return queue.delivered() == 1 }, "a hold that stands completes")

	// The hold on the second message goes while its turn is still
	// running, which is the state a renewal garam refuses leaves.
	queue.refuseRenewals()
	eventually(t, func() bool { return queue.renewalsRefused() > 0 }, "the renewal garam refuses is made")
	gateway.endTurn(delivery.TurnFinished, "message-1")
	queue.allowRenewals()

	eventually(t, func() bool { return queue.delivered() == 2 }, "the redelivery the runtime answers as completed completes")
	assert.Equal(t, 2, queue.deliveries(1), "the lapsed message was handed out again")
	assert.Equal(t, 2, gateway.sourceCount(), "and ran no second turn")
	assert.Equal(t, 2, queue.completionCalls(), "the claim that lapsed was not completed")
}

// TestRun_CompletesAMessageTheRuntimeCompletedBeforeARestart is a
// process that dies between the turn and the completion: the pairing
// goes with it and garam hands the message to the process that follows,
// which the runtime answers as completed without running a second turn
// (ADR-0079). The second message, never completed, is the control: its
// redelivery is posted and waits for its turn.
func TestRun_CompletesAMessageTheRuntimeCompletedBeforeARestart(t *testing.T) {
	queue := newGaramQueue(t, 200*time.Millisecond, "what the sender said", "second")
	gateway := newAgentGateway(t)
	stop := run(t, queue, gateway)

	holding(t, queue, 0, 0)
	holding(t, queue, 1, 0)
	stop()
	// The turn the stopped process started ends, and nothing holds its
	// pairing any more.
	gateway.endTurn(delivery.TurnFinished, "message-0")

	run(t, queue, gateway)
	eventually(t, func() bool { return queue.delivered() == 1 }, "the replay of a completed message completes its claim")
	assert.Equal(t, 2, queue.deliveries(0), "the new process was handed the same message")
	assert.Equal(t, 2, gateway.sourceCount(), "no second turn ran for it")
	assert.Zero(t, queue.messagesCompleted(1), "the control: the uncompleted message waits for its turn")
}

// messagesCompleted reports whether one message is completed, as 0 or 1.
func (q *garamQueue) messagesCompleted(message int) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.messages[message].completed {
		return 1
	}
	return 0
}

// pollingQueue answers every claim with err and hands nothing out, so
// the loop against it does nothing but poll.
type pollingQueue struct {
	mu     sync.Mutex
	err    error
	claims int
}

func (q *pollingQueue) Claim(context.Context) (*delivery.Claim, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claims++
	return nil, q.err
}

func (q *pollingQueue) Renew(context.Context, delivery.ClaimID) (*delivery.Claim, error) {
	return nil, nil
}

func (q *pollingQueue) Complete(context.Context, delivery.ClaimID) error { return nil }

func (q *pollingQueue) Post(context.Context, delivery.Outbound) (*delivery.Stored, error) {
	return nil, errorx.New(errorx.KindUnavailable, "delivery: not here")
}

func (q *pollingQueue) polls() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claims
}

// heldQueue hands out one claim and holds every later poll open, so one
// pairing is in flight and the renewals are the only other traffic.
// Every renewal answers err.
type heldQueue struct {
	mu       sync.Mutex
	expires  time.Duration
	err      error
	handed   bool
	renewals int
}

func (q *heldQueue) Claim(ctx context.Context) (*delivery.Claim, error) {
	q.mu.Lock()
	first := !q.handed
	q.handed = true
	expires := q.expires
	q.mu.Unlock()
	if first {
		return &delivery.Claim{
			ID: "claim-1", ExpiresAt: time.Now().Add(expires), Message: "message-1", Sender: senderGRN(), Body: "hello",
		}, nil
	}
	<-ctx.Done()
	return nil, errorx.New(errorx.KindDeadlineExceeded, "delivery: poll cut")
}

func (q *heldQueue) Renew(context.Context, delivery.ClaimID) (*delivery.Claim, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.renewals++
	return nil, q.err
}

func (q *heldQueue) Complete(context.Context, delivery.ClaimID) error { return nil }

func (q *heldQueue) Post(context.Context, delivery.Outbound) (*delivery.Stored, error) {
	return nil, errorx.New(errorx.KindUnavailable, "delivery: not here")
}

func (q *heldQueue) renewed() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.renewals
}

// idleGateway takes every message and ends no turn: what the loop does
// with what it is holding is the subject, not what ends a hold.
type idleGateway struct{ done <-chan struct{} }

func (g idleGateway) Post(context.Context, delivery.MessageID, string) (delivery.Progress, error) {
	return delivery.ProgressPending, nil
}

func (g idleGateway) Lookup(context.Context, delivery.MessageID) (delivery.Progress, error) {
	return delivery.ProgressPending, nil
}

func (g idleGateway) Subscribe(context.Context) (delivery.TurnStream, error) {
	return idleStream(g), nil
}

func (g idleGateway) Status(context.Context) (*delivery.RuntimeStatus, error) {
	return nil, errorx.New(errorx.KindUnavailable, "delivery: no status here")
}

type idleStream struct{ done <-chan struct{} }

func (s idleStream) Next() (delivery.TurnEnd, error) {
	<-s.done
	return delivery.TurnEnd{}, errorx.New(errorx.KindUnavailable, "delivery: stream ended")
}

func (s idleStream) Close() error { return nil }

// idleEnds is a legacy loop over queue and an idle gateway.
func idleEnds(queue delivery.Queue, done <-chan struct{}) delivery.Ends {
	return delivery.Ends{
		Agent: agentGRN(), Queue: queue, Gateway: idleGateway{done: done},
		Execution: delivery.NewLegacyExecution(), Credentials: newMounted(),
	}
}

// TestService_ReadsAnEmptyPollFromTheKindGaramAnswers holds the claim
// loop against the shape garam's answer arrives in. `stack-go.md` §4
// does not admit errors.Is beside errorx, and the reason is here: a
// sentinel matched by identity stops matching the moment anything
// replaces it with a kind-carrying copy, and every empty poll would
// then be logged and waited on as the failure it is not.
//
// The control is a failure of another kind, which must be waited on:
// without it a loop that asked again at once whatever garam said would
// read exactly like this one.
func TestService_ReadsAnEmptyPollFromTheKindGaramAnswers(t *testing.T) {
	empty := runPolling(t, errorx.New(errorx.KindNotFound, "delivery: no message ready"))
	assert.Greater(t, empty, 1, "an empty poll is asked again at once")

	unreachable := runPolling(t, errorx.New(errorx.KindUnavailable, "delivery: garam answered 503"))
	assert.Equal(t, 1, unreachable, "the control: another kind waits the retry delay out")
}

// runPolling runs the loop for one window against a garam answering
// every poll with err, and returns how many polls it made. The retry
// delay is far longer than the window, so a poll waited on is a poll not
// repeated.
func runPolling(t *testing.T, answer error) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	queue := &pollingQueue{err: answer}
	s, err := delivery.NewService(idleEnds(queue, ctx.Done()), delivery.Config{
		RetryDelay:       10 * time.Second,
		MinRenewInterval: time.Second,
		ClaimTimeout:     10 * time.Second,
		CallTimeout:      time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, s.Run(ctx))
	return queue.polls()
}

// TestService_ReadsALapsedHoldFromTheKindGaramAnswers is the same claim
// for the renewal half: a hold garam says is gone is dropped, and every
// other refusal keeps it. Reading that by the sentinel's identity turns
// a lapsed hold into one renewed forever, against a message garam has
// already handed to somebody else.
//
// The control is a transient refusal, which must keep the hold:
// without it a loop that dropped every pairing it could not renew would
// read exactly like this one.
func TestService_ReadsALapsedHoldFromTheKindGaramAnswers(t *testing.T) {
	lapsed := runHolding(t, errorx.New(errorx.KindFailedPrecondition, "delivery: claim no longer held"))
	assert.Equal(t, 1, lapsed, "a hold garam says is gone is dropped rather than carried")

	fenced := runHolding(t, errorx.New(errorx.KindPermissionDenied, "delivery: garam refused the execution token"))
	assert.Equal(t, 1, fenced, "a hold the fence refuses is dropped too")

	unreachable := runHolding(t, errorx.New(errorx.KindUnavailable, "delivery: garam answered 503"))
	assert.Greater(t, unreachable, 1, "the control: another kind keeps the hold and tries again")
}

// runHolding runs the loop for one window against a garam that hands
// out one message and refuses every renewal of it with err, and returns
// how many renewals it made.
func runHolding(t *testing.T, answer error) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	// The lease divided by three is the renewal interval, and the floor
	// below is what a lease this short takes instead.
	queue := &heldQueue{expires: 30 * time.Millisecond, err: answer}
	s, err := delivery.NewService(idleEnds(queue, ctx.Done()), delivery.Config{
		RetryDelay:       20 * time.Millisecond,
		MinRenewInterval: 10 * time.Millisecond,
		ClaimTimeout:     10 * time.Second,
		CallTimeout:      time.Second,
	})
	require.NoError(t, err)
	require.NoError(t, s.Run(ctx))
	return queue.renewed()
}
