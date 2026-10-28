package delivery_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/delivery"
)

// stalled is the peer every test here is about: it accepts the
// connection, reads the request, and never answers. A dropped conntrack
// entry and a load balancer that reaps without an RST look like this
// from the adapter.
func stalled(t *testing.T) string {
	t.Helper()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	// Cleanup runs last-registered first, so the handlers are released
	// before Close waits on them; the other order deadlocks the test.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	return server.URL
}

// TestBounds_StalledPeerIsRefusedWithinItsBound is #747's first probe as
// a regression: with no bound each of these parks forever. Claim, Renew,
// Complete, Post and the calls beside them take the caller's deadline;
// Subscribe takes the client's header bound instead, because a deadline
// on the call would also end the stream it opens.
func TestBounds_StalledPeerIsRefusedWithinItsBound(t *testing.T) {
	const bound = 200 * time.Millisecond
	base := stalled(t)

	q, err := delivery.NewHTTPQueue(&http.Client{}, base, agentGRN(), delivery.HeldExecution("held-token"), agentCredential())
	require.NoError(t, err)
	streamBounded := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: bound}}
	g, err := delivery.NewHTTPGateway(streamBounded, base, agentGRN())
	require.NoError(t, err)

	deadlined := map[string]func(context.Context) error{
		"Claim":    func(c context.Context) error { _, e := q.Claim(c); return e },
		"Renew":    func(c context.Context) error { _, e := q.Renew(c, "id"); return e },
		"Complete": func(c context.Context) error { return q.Complete(c, "id") },
		"Post":     func(c context.Context) error { _, e := g.Post(c, "message-1", "hi", ""); return e },
		"Lookup":   func(c context.Context) error { _, e := g.Lookup(c, "message-1"); return e },
		"Status":   func(c context.Context) error { _, e := g.Status(c); return e },
		"Submit":   func(c context.Context) error { _, e := q.Post(c, outbound()); return e },
	}
	for name, call := range deadlined {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), bound)
			defer cancel()
			require.Error(t, answerWithin(t, 20*bound, func() error { return call(ctx) }),
				"%s answered without an error against a peer that never answers", name)
		})
	}
	t.Run("Subscribe", func(t *testing.T) {
		// No deadline on the call: the bound is the client's, and it
		// stops at the headers.
		require.Error(t, answerWithin(t, 20*bound, func() error { _, e := g.Subscribe(context.Background()); return e }),
			"Subscribe answered without an error against a peer that never answers")
	})
}

// answerWithin returns the call's error, or fails the test if the call
// is still parked after limit — which is the defect, not a slow run.
func answerWithin(t *testing.T, limit time.Duration, call func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return err
	case <-time.After(limit):
		t.Fatalf("still parked after %s: no bound on the call", limit)
		return nil
	}
}

// TestBounds_ClaimIsNotCutShortByTheOrdinaryBound is the control on the
// bound itself: a claim is a long poll and its bound is its own, so a
// wait longer than every other call's still answers 204 rather than
// failing. Without two values this is what a single timeout breaks.
func TestBounds_ClaimIsNotCutShortByTheOrdinaryBound(t *testing.T) {
	const poll = 300 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(poll):
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)

	q, err := delivery.NewHTTPQueue(server.Client(), server.URL, agentGRN(), delivery.HeldExecution("held-token"), agentCredential())
	require.NoError(t, err)

	// The ordinary bound is under the poll; the claim's is over it.
	ctx, cancel := context.WithTimeout(context.Background(), poll/3)
	defer cancel()
	_, err = q.Claim(ctx)
	require.Error(t, err, "a bound under the wait would have to cut the poll short")
	assert.False(t, errors.Is(err, delivery.ErrNoMessageReady), "a cut poll must not read as an empty one")

	claimCtx, cancelClaim := context.WithTimeout(context.Background(), 10*poll)
	defer cancelClaim()
	_, err = q.Claim(claimCtx)
	assert.ErrorIs(t, err, delivery.ErrNoMessageReady, "the poll waited its time and answered empty")
}

// TestBounds_SubscribeOutlivesTheHeaderBound holds a stream open well
// past the client's header bound and requires an event emitted after it
// to still arrive. The bound is on opening a response, not on reading
// one, which is what lets the stream run for the agent's life.
func TestBounds_SubscribeOutlivesTheHeaderBound(t *testing.T) {
	const bound = 200 * time.Millisecond
	gateway := newAgentGateway(t)
	client := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: bound}}
	g, err := delivery.NewHTTPGateway(client, gateway.server.URL, agentGRN())
	require.NoError(t, err)

	stream, err := g.Subscribe(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = stream.Close() })

	// Past the bound before anything is emitted, so nothing below can
	// have been in flight when it expired.
	time.Sleep(4 * bound)
	gateway.endTurn(delivery.TurnFinished, "message-0")

	turn, err := stream.Next()
	require.NoError(t, err, "the stream died at the header bound; it is held open for the agent's life")
	assert.Equal(t, []delivery.MessageID{"message-0"}, turn.Messages)
}

// TestBounds_AHungCompletionDoesNotStopTheNextOne is #747's second
// probe as a regression. hold is one goroutine: it calls complete
// inline, so a completion that never answers used to stop every
// completion behind it. Bounded, the first fails and the second still
// arrives — the message the first named is left to be redelivered,
// which is what every lost completion here means.
func TestBounds_AHungCompletionDoesNotStopTheNextOne(t *testing.T) {
	queue, gateway := twoInFlight(t, true)
	gateway.endTurn(delivery.TurnFinished, "message-0", "message-1")
	eventually(t, func() bool { return queue.delivered() == 1 },
		"the completion behind a hung one still reaches garam")
}

// TestBounds_ControlBothCompleteWhenNothingHangs is what makes the count
// above mean something: one delivered proves the second call got
// through only if two arrive when nothing is parked.
func TestBounds_ControlBothCompleteWhenNothingHangs(t *testing.T) {
	queue, gateway := twoInFlight(t, false)
	gateway.endTurn(delivery.TurnFinished, "message-0", "message-1")
	eventually(t, func() bool { return queue.delivered() == 2 },
		"control: the same turn end completes both when nothing hangs")
}

// twoInFlight runs the loop until it holds the pairing for both
// messages, optionally against a queue whose first completion never
// answers.
//
// Waiting on the second post is not enough, and the lease here is too
// long for holding to read a renewal: the gateway records a message
// before the loop has the identifier back, so a turn ending in that
// window finds nothing to complete. The pairing lands a moment later and
// is renewed for as long as the loop runs, so the claim never lapses and
// that turn's end never comes again — which is why the window is shut
// here rather than waited out (#852). The third claim is what shuts it:
// the loop asks for work again only once hold has taken the pairing for
// what it last posted.
func twoInFlight(t *testing.T, hang bool) (*garamQueue, *agentGateway) {
	t.Helper()
	queue := newGaramQueue(t, 10*time.Second, "one", "two")
	gateway := newAgentGateway(t)
	if hang {
		queue.hangFirstCompletionCall(t)
	}
	cfg := brief()
	cfg.ClaimTimeout = 2 * time.Second
	cfg.CallTimeout = 150 * time.Millisecond
	runWith(t, queue, gateway, cfg)
	eventually(t, func() bool { return len(gateway.posted()) == 2 && queue.claimsAttempted() > 2 },
		"both messages reach the gateway and the loop holds both pairings")
	return queue, gateway
}
