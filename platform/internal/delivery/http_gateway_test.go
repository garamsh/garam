package delivery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/delivery"
)

// gatewayAnswering is the platform's gateway answering one way to
// everything.
func gatewayAnswering(t *testing.T, answer http.HandlerFunc) *delivery.HTTPGateway {
	t.Helper()
	server := httptest.NewServer(answer)
	t.Cleanup(server.Close)
	gateway, err := delivery.NewHTTPGateway(server.Client(), server.URL, agentGRN())
	require.NoError(t, err)
	return gateway
}

// TestHTTPGateway_PostCarriesGaramsIdentifierAndReadsTheRuntimesProgress
// covers the ingress call: where it goes, what it carries, and what the
// answer says. The identifier is garam's for the message, so a replay
// of one the runtime completed answers completed rather than running
// again. The body carries the identifier and the content and nothing
// else: the gateway refuses a field it does not know.
func TestHTTPGateway_PostCarriesGaramsIdentifierAndReadsTheRuntimesProgress(t *testing.T) {
	var route string
	var sent map[string]any
	status, wire := http.StatusAccepted, "queued"
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		route = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		writeJSON(w, status, map[string]string{"id": "message-1", "agent_id": agentGRN().String(), "status": wire})
	})

	progress, err := gateway.Post(context.Background(), "message-1", `{"contract":"garam-message.v1"}`, "")
	require.NoError(t, err)
	assert.Equal(t, delivery.ProgressPending, progress)
	assert.Equal(t, "/agents/"+agentGRN().String()+"/messages", route)
	assert.Equal(t, map[string]any{"id": "message-1", "content": `{"contract":"garam-message.v1"}`}, sent)

	status, wire = http.StatusOK, "completed"
	progress, err = gateway.Post(context.Background(), "message-1", `{"contract":"garam-message.v1"}`, "")
	require.NoError(t, err)
	assert.Equal(t, delivery.ProgressCompleted, progress, "a replay of a completed message")
}

// TestHTTPGateway_PostReportsWhatTheGatewayRefusedWith is what happens
// to a message the gateway will not take — content it does not accept,
// or an identifier it holds with other content: nothing is paired, so
// the claim is left to lapse, and the line the loop logs carries the
// gateway's own words.
func TestHTTPGateway_PostReportsWhatTheGatewayRefusedWith(t *testing.T) {
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "id reused with different content"})
	})

	_, err := gateway.Post(context.Background(), "message-1", "content", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id reused with different content")
}

// TestHTTPGateway_PostCarriesTheSignatureInTheMemberSherlockNames is
// the ingress body Sherlock v0.4.0 takes (gateway.md, §Signed
// messages): the signature as the string member signature, beside the
// content it covers, both as they were handed in. The unsigned post in
// the test above is the control: it carries no such member at all.
func TestHTTPGateway_PostCarriesTheSignatureInTheMemberSherlockNames(t *testing.T) {
	var sent map[string]any
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		writeJSON(w, http.StatusAccepted, map[string]string{"id": "message-1", "status": "queued"})
	})
	content := `{"contract":"garam-message.v1","sender":"s","body":"caf\u00e9"}`

	_, err := gateway.Post(context.Background(), "message-1", content, "header.payload.signature")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"id": "message-1", "content": content, "signature": "header.payload.signature"}, sent)
}

// TestHTTPGateway_PostReadsEachSignatureRefusalAsUndelivered holds the
// refusals Sherlock's ingress answers about a signature to the answer
// the loop leaves to lapse: each is an unavailable error carrying the
// runtime's own words, never one that completes or pairs anything.
func TestHTTPGateway_PostReadsEachSignatureRefusalAsUndelivered(t *testing.T) {
	refusals := map[string]struct {
		status int
		error  string
	}{
		"rejected":         {http.StatusForbidden, "signature rejected: audience"},
		"required":         {http.StatusForbidden, "signature required"},
		"keys unavailable": {http.StatusServiceUnavailable, "issuer keys unavailable"},
	}
	for name, refused := range refusals {
		t.Run(name, func(t *testing.T) {
			gateway := gatewayAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, refused.status, map[string]string{"error": refused.error})
			})
			_, err := gateway.Post(context.Background(), "message-1", "content", "header.payload.signature")
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "left to lapse and redelivered: %v", err)
			assert.Contains(t, err.Error(), refused.error)
		})
	}
}

// TestHTTPGateway_PostRefusesAnAnswerAboutAnotherMessage covers the
// answers that would leave the loop holding a message the runtime did
// not say it took: one naming nothing, one naming another message and
// one in a status the gateway does not define. The answer naming the
// message is the control.
func TestHTTPGateway_PostRefusesAnAnswerAboutAnotherMessage(t *testing.T) {
	answers := map[string]map[string]string{
		"naming nothing":         {"status": "queued"},
		"naming another message": {"id": "message-2", "status": "queued"},
		"in an unknown status":   {"id": "message-1", "status": "lost"},
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			gateway := gatewayAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusAccepted, answer)
			})
			_, err := gateway.Post(context.Background(), "message-1", "content", "")
			require.Error(t, err)
		})
	}
	control := gatewayAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusAccepted, map[string]string{"id": "message-1", "status": "queued"})
	})
	_, err := control.Post(context.Background(), "message-1", "content", "")
	require.NoError(t, err, "the control: an answer about the message")
}

// TestHTTPGateway_LookupReadsWhetherTheRuntimeCompletedAMessage is the
// durable completion lookup a lost event is recovered by.
func TestHTTPGateway_LookupReadsWhetherTheRuntimeCompletedAMessage(t *testing.T) {
	var route string
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		route = r.URL.Path
		writeJSON(w, http.StatusOK, map[string]string{"id": "message-1", "status": "completed"})
	})
	progress, err := gateway.Lookup(context.Background(), "message-1")
	require.NoError(t, err)
	assert.Equal(t, delivery.ProgressCompleted, progress)
	assert.Equal(t, "/agents/"+agentGRN().String()+"/messages/message-1", route)
}

// runtimeStatus is the status body the runtime serves at v0.1.0.
func runtimeStatus() map[string]any {
	return map[string]any{
		"contract_version": 1, "agent_id": agentGRN().String(), "assignment_epoch": "3",
		"generation": strings.Repeat("a", 32), "config_revision": "7", "state": "serving",
		"started_at": "2026-10-05T01:02:03Z",
	}
}

// TestHTTPGateway_StatusReadsOnlyARuntimeItCanActivateFor holds the
// status read to what an activation can be asked for: another status
// version, another agent, no generation and an undefined state are each
// refused. The status as the runtime serves it is the control.
func TestHTTPGateway_StatusReadsOnlyARuntimeItCanActivateFor(t *testing.T) {
	var route string
	gateway := gatewayAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		route = r.URL.Path
		writeJSON(w, http.StatusOK, runtimeStatus())
	})
	status, err := gateway.Status(context.Background())
	require.NoError(t, err, "the control: the status the runtime serves")
	assert.Equal(t, "/agents/"+agentGRN().String()+"/status", route)
	assert.Equal(t, delivery.RuntimeStatus{
		Agent: agentGRN(), Epoch: "3", Generation: strings.Repeat("a", 32), Revision: "7",
		State: delivery.RuntimeServing, StartedAt: time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC),
	}, *status)

	changes := map[string]func(m map[string]any){
		"another status version": func(m map[string]any) { m["contract_version"] = 2 },
		"another agent":          func(m map[string]any) { m["agent_id"] = senderGRN().String() },
		"no generation":          func(m map[string]any) { m["generation"] = "" },
		"an undefined state":     func(m map[string]any) { m["state"] = "napping" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			body := runtimeStatus()
			change(body)
			gateway := gatewayAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusOK, body)
			})
			_, err := gateway.Status(context.Background())
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition), "a definite refusal, not an outage: %v", err)
		})
	}
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
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("the test server's writer does not flush, so the stream never reaches the subscriber")
			return
		}
		flusher.Flush()
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
		`{"kind":"turn_finished","turn":1,"message_ids":["message-0","message-1"],"response":"done"}`,
		`{"kind":"turn_started"`,
		`{"kind":"turn_failed","turn":2,"message_ids":["message-2"],"error":"provider unreachable"}`,
	)

	stream, err := gateway.Subscribe(context.Background())
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	finished, err := stream.Next()
	require.NoError(t, err)
	assert.Equal(t, delivery.TurnEnd{
		Outcome:  delivery.TurnFinished,
		Messages: []delivery.MessageID{"message-0", "message-1"},
	}, finished)
	assert.True(t, finished.Completes())

	failed, err := stream.Next()
	require.NoError(t, err)
	assert.Equal(t, delivery.TurnEnd{
		Outcome:  delivery.TurnFailed,
		Messages: []delivery.MessageID{"message-2"},
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
		`{"kind":"turn_finished","turn":1,"message_ids":["message-0"]}`,
	)

	stream, err := gateway.Subscribe(context.Background())
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	turn, err := stream.Next()
	require.NoError(t, err)
	assert.Equal(t, []delivery.MessageID{"message-0"}, turn.Messages)
}

// TestHTTPGateway_SubscribeSkipsAnEventJustPastTheBound is the boundary
// the test above does not reach. Its event is twice the bound, which
// says an event far past it is dropped and nothing about where the cut
// falls; this one is a hundred bytes past, and the turn ending after it
// is again what says the subscription carried on.
//
// A hundred and not more: the reader accumulates a line in
// bufio.Reader-sized pieces, so an event more than one piece past the
// bound is truncated wherever the cut is, and only an event inside that
// piece says where it is. What holds this to a hundred rather than to
// the piece is that bufio's size is not this package's to choose.
func TestHTTPGateway_SubscribeSkipsAnEventJustPastTheBound(t *testing.T) {
	// The oversized event is itself a turn ending, so one the reader kept
	// where it should have dropped it names messages and is answered
	// here rather than skipped for being something else.
	const dataLine = 1<<20 + 100
	const shape = `{"kind":"turn_finished","turn":1,"message_ids":["msg-past-the-bound"],"pad":"%s"}`
	oversized := fmt.Sprintf(shape, strings.Repeat("x", dataLine-len("data: ")-len(shape)+len("%s")))
	require.Len(t, "data: "+oversized, dataLine, "the line is the length this test is about")

	gateway := sendingEvents(t,
		oversized,
		`{"kind":"turn_finished","turn":2,"message_ids":["message-0"]}`,
	)

	stream, err := gateway.Subscribe(context.Background())
	require.NoError(t, err)
	defer func() { _ = stream.Close() }()

	turn, err := stream.Next()
	require.NoError(t, err)
	assert.Equal(t, []delivery.MessageID{"message-0"}, turn.Messages,
		"the event past the bound was carried rather than dropped")
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
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("the test server's writer does not flush, so the stream never reaches the subscriber")
			return
		}
		flusher.Flush()
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

	_, err := delivery.NewHTTPGateway(server.Client(), server.URL, agentGRN())
	require.NoError(t, err, "an address and an agent that work")

	tests := map[string]struct {
		client *http.Client
		base   string
		agent  grn.GRN
	}{
		"no client":                  {nil, server.URL, agentGRN()},
		"a relative address":         {server.Client(), "/agents", agentGRN()},
		"an address that is not one": {server.Client(), "://", agentGRN()},
		"an agent that is not a GRN": {server.Client(), server.URL, grn.GRN{}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := delivery.NewHTTPGateway(tt.client, tt.base, tt.agent)
			require.Error(t, err)
		})
	}
}
