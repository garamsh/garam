package delivery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/delivery"
)

// gatewayAnswering is the platform's gateway answering one way to
// everything.
func gatewayAnswering(t *testing.T, answer http.HandlerFunc) *delivery.HTTPGateway {
	t.Helper()
	server := httptest.NewServer(answer)
	t.Cleanup(server.Close)
	gateway, err := delivery.NewHTTPGateway(server.Client(), server.URL, gatewayAgentID)
	require.NoError(t, err)
	return gateway
}

// TestHTTPGateway_PostReturnsTheIdentifierTheGatewayMinted covers the
// ingress call: where it goes, what it carries, and the identifier that
// comes back — which is the only thing a turn's event can be paired
// back to, because a caller cannot supply one.
//
// The body carries the content and nothing else. The gateway refuses a
// field it does not know, so a field added here would be rejected
// there rather than ignored.
func TestHTTPGateway_PostReturnsTheIdentifierTheGatewayMinted(t *testing.T) {
	var route string
	var sent map[string]any
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		route = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		writeJSON(w, http.StatusAccepted, map[string]string{
			"id": "msg-9f2c", "agent_id": gatewayAgentID, "status": "queued",
		})
	})

	id, err := gateway.Post(context.Background(), "what the sender said")
	require.NoError(t, err)
	assert.Equal(t, delivery.GatewayID("msg-9f2c"), id)
	assert.Equal(t, "/agents/"+gatewayAgentID+"/messages", route)
	assert.Equal(t, map[string]any{"content": "what the sender said"}, sent)
}

// TestHTTPGateway_PostReportsWhatTheGatewayRefusedWith is what happens
// to a message the gateway will not take: nothing is paired, so the
// claim is left to lapse, and the line the loop logs carries the
// gateway's own words.
func TestHTTPGateway_PostReportsWhatTheGatewayRefusedWith(t *testing.T) {
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
	})

	_, err := gateway.Post(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "content is required")
}

// TestHTTPGateway_PostReportsAnAcceptanceThatNamesNothing covers the
// answer that would leave the loop holding a pairing to an empty
// identifier: it is an error rather than a pairing under "".
func TestHTTPGateway_PostReportsAnAcceptanceThatNamesNothing(t *testing.T) {
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusAccepted, map[string]string{"agent_id": gatewayAgentID, "status": "queued"})
	})

	_, err := gateway.Post(context.Background(), "what the sender said")
	require.Error(t, err)
}

// sendingEvents is a gateway whose event stream carries what the test
// wrote, in order, and then ends.
func sendingEvents(t *testing.T, events ...string) *delivery.HTTPGateway {
	t.Helper()
	return gatewayAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, event := range events {
			fmt.Fprintf(w, "data: %s\n\n", event)
		}
		w.(http.Flusher).Flush()
	})
}

// TestHTTPGateway_SubscribeReadsTurnEndingsAndSkipsEverythingElse is
// what the loop reads the stream for. Only a terminal event names the
// messages a turn drained, so the tool call and the event nothing can
// parse are skipped rather than ending the subscription — and the two
// terminal events around them are the control, because a reader that
// stopped at either would never reach them.
func TestHTTPGateway_SubscribeReadsTurnEndingsAndSkipsEverythingElse(t *testing.T) {
	gateway := sendingEvents(t,
		`{"kind":"turn_started","turn":1}`,
		`{"kind":"tool_call","turn":1,"tool_name":"files"}`,
		`{"kind":"turn_finished","turn":1,"message_ids":["msg-0","msg-1"],"response":"done"}`,
		`{"kind":"turn_started"`,
		`{"kind":"turn_failed","turn":2,"message_ids":["msg-2"],"error":"provider unreachable"}`,
	)

	stream, err := gateway.Subscribe(context.Background())
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	finished, err := stream.Next()
	require.NoError(t, err)
	assert.Equal(t, delivery.TurnEnd{
		Outcome:  delivery.TurnFinished,
		Messages: []delivery.GatewayID{"msg-0", "msg-1"},
	}, finished)
	assert.True(t, finished.Completes())

	failed, err := stream.Next()
	require.NoError(t, err)
	assert.Equal(t, delivery.TurnEnd{
		Outcome:  delivery.TurnFailed,
		Messages: []delivery.GatewayID{"msg-2"},
	}, failed)
	assert.False(t, failed.Completes(), "a turn that failed is an attempt rather than a completion")

	_, err = stream.Next()
	require.Error(t, err, "the stream ends when the response does")
}

// TestHTTPGateway_SubscribeSkipsAnEventLargerThanItReads holds the
// bound this reader keeps: an event past it is dropped and the
// subscription carries on, because losing it would cost the
// completions behind it. The turn ending after it is what shows the
// stream survived.
func TestHTTPGateway_SubscribeSkipsAnEventLargerThanItReads(t *testing.T) {
	gateway := sendingEvents(t,
		`{"kind":"tool_call","tool_result":"`+strings.Repeat("x", 2<<20)+`"}`,
		`{"kind":"turn_finished","turn":1,"message_ids":["msg-0"]}`,
	)

	stream, err := gateway.Subscribe(context.Background())
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	turn, err := stream.Next()
	require.NoError(t, err)
	assert.Equal(t, []delivery.GatewayID{"msg-0"}, turn.Messages)
}

// TestHTTPGateway_SubscribeReportsARefusedStream keeps a stream that
// was never opened apart from one that carried nothing: the loop waits
// and subscribes again rather than reading an error body as events.
func TestHTTPGateway_SubscribeReportsARefusedStream(t *testing.T) {
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown agent other"})
	})

	_, err := gateway.Subscribe(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent other")
}

// TestHTTPGateway_SubscribeEndsWithTheContextThatOpenedIt is how the
// loop stops reading: the stream is bound by the context the
// subscription was opened with, so a stopped process leaves nothing
// blocked on a read.
func TestHTTPGateway_SubscribeEndsWithTheContextThatOpenedIt(t *testing.T) {
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})

	ctx, stop := context.WithCancel(context.Background())
	stream, err := gateway.Subscribe(ctx)
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	read := make(chan error, 1)
	go func() {
		_, err := stream.Next()
		read <- err
	}()
	stop()
	require.Error(t, <-read)
}

// TestNewHTTPGateway_RefusesWhatItCannotCall keeps a misconfiguration
// at the start. The working arguments are the control: each case below
// changes one of them.
func TestNewHTTPGateway_RefusesWhatItCannotCall(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	_, err := delivery.NewHTTPGateway(server.Client(), server.URL, gatewayAgentID)
	require.NoError(t, err, "an address and an agent that work")

	tests := map[string]struct {
		client *http.Client
		base   string
		agent  string
	}{
		"no client":                  {nil, server.URL, gatewayAgentID},
		"a relative address":         {server.Client(), "/agents", gatewayAgentID},
		"an address that is not one": {server.Client(), "://", gatewayAgentID},
		"no agent identifier":        {server.Client(), server.URL, ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := delivery.NewHTTPGateway(tt.client, tt.base, tt.agent)
			require.Error(t, err)
		})
	}
}
