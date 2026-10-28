package delivery_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/delivery"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
)

// garamAnswering is garam's machine listener answering one way to
// everything, so a test can hold the client to what one answer means.
// The execution holds an activation already, so it presents that
// activation's token and is refused as garam's answers say.
func garamAnswering(t *testing.T, answer http.HandlerFunc) (*delivery.HTTPQueue, *delivery.Execution) {
	t.Helper()
	server := httptest.NewServer(answer)
	t.Cleanup(server.Close)
	execution := delivery.HeldExecution("held-token")
	queue, err := delivery.NewHTTPQueue(server.Client(), server.URL, agentGRN(), execution, agentCredential())
	require.NoError(t, err)
	return queue, execution
}

// TestHTTPQueue_ClaimReadsTheHoldGaramAnsweredWith covers the call as
// api/machine.yaml describes it — the route it goes to, and the hold
// the loop reads out of the answer: the claim's identifier, which the
// lease changes, apart from the message's and its verified sender,
// which it does not. The agent is a GRN and a GRN carries colons, so
// what the path segment survives as is part of it.
func TestHTTPQueue_ClaimReadsTheHoldGaramAnsweredWith(t *testing.T) {
	expires := time.Now().Add(30 * time.Second).UTC().Truncate(time.Second)
	var route, method, authorization, execution string
	queue, _ := garamAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		route, method, authorization = r.URL.Path, r.Method, r.Header.Get("Authorization")
		execution = r.Header.Get("Garam-Execution")
		writeJSON(w, http.StatusOK, machinegen.Claim{
			Id:        "claim-1",
			ExpiresAt: expires,
			Attempt:   1,
			Message: machinegen.Message{
				Id: "message-1", Sender: senderGRN().String(), Recipient: agentGRN().String(),
				Body: "what the sender said",
			},
		})
	})

	claim, err := queue.Claim(context.Background())
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "/agents/"+agentGRN().String()+"/claims", route)
	assert.Equal(t, "Garam-Token "+agentTokenOf("a"), authorization, "the claim carries the agent's token")
	assert.Equal(t, "held-token", execution, "and the activation's")
	assert.Equal(t, delivery.ClaimID("claim-1"), claim.ID)
	assert.Equal(t, delivery.MessageID("message-1"), claim.Message)
	assert.Equal(t, senderGRN(), claim.Sender)
	assert.Equal(t, expires, claim.ExpiresAt.UTC())
	assert.Equal(t, "what the sender said", claim.Body)

	unsent, _ := garamAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, machinegen.Claim{Id: "claim-1", Message: machinegen.Message{Id: "message-1"}})
	})
	_, err = unsent.Claim(context.Background())
	assert.Error(t, err, "a claim without a verified sender is not one the loop can post")
}

// TestHTTPQueue_ClaimAnswersNoMessageReadyOnAnEmptyPoll holds the 204
// apart from a refusal: a poll that waited its time and found nothing
// is the ordinary answer, and the loop asks again rather than treating
// it as a failure.
func TestHTTPQueue_ClaimAnswersNoMessageReadyOnAnEmptyPoll(t *testing.T) {
	queue, _ := garamAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	claim, err := queue.Claim(context.Background())
	assert.Nil(t, claim)
	assert.True(t, errors.Is(err, delivery.ErrNoMessageReady))
}

// TestHTTPQueue_RenewAndCompleteReportAHoldThatIsGone is the answer the
// loop drops a pairing on. The hold that stands is the control: the
// same two calls against a 200 and a 204 report nothing, so what is
// read is the conflict rather than the call.
func TestHTTPQueue_RenewAndCompleteReportAHoldThatIsGone(t *testing.T) {
	var routes []string
	standing, _ := garamAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		routes = append(routes, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/completion") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, machinegen.Claim{
			Id: "claim-1", ExpiresAt: time.Now().Add(time.Minute),
			Message: machinegen.Message{Id: "message-1", Sender: senderGRN().String()},
		})
	})

	renewed, err := standing.Renew(context.Background(), "claim-1")
	require.NoError(t, err)
	assert.Equal(t, delivery.ClaimID("claim-1"), renewed.ID)
	require.NoError(t, standing.Complete(context.Background(), "claim-1"))
	assert.Equal(t, []string{
		"/agents/" + agentGRN().String() + "/claims/claim-1/renewal",
		"/agents/" + agentGRN().String() + "/claims/claim-1/completion",
	}, routes)

	gone, execution := garamAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, machinegen.Error{Kind: "failed_precondition", Message: "message claim is stale"})
	})

	_, err = gone.Renew(context.Background(), "claim-1")
	assert.True(t, errors.Is(err, delivery.ErrClaimLapsed))
	assert.True(t, errors.Is(gone.Complete(context.Background(), "claim-1"), delivery.ErrClaimLapsed))
	_, err = execution.Present()
	assert.NoError(t, err, "a lapsed hold fences nothing")
}

// TestHTTPQueue_ReadsTheFenceFromItsAnswers holds the client to
// ADR-0084's reading of garam's refusals: a 403 on any call is the
// fence, and so is a 409 on a claim, where no hold exists to have
// lapsed, and a submission's 409 of kind failed_precondition, which
// ADR-0083 keeps apart from a conflicting key's already_exists. Each
// closes the execution, so nothing more reaches garam. A 503 on the
// same call is the control: it is undecided and closes
// nothing. A 401 is the agent token refused, which
// TestHTTPQueue_TokenModeReadsARefusedTokenAsUndecided holds.
func TestHTTPQueue_ReadsTheFenceFromItsAnswers(t *testing.T) {
	claim := func(q *delivery.HTTPQueue) error { _, err := q.Claim(context.Background()); return err }
	submit := func(q *delivery.HTTPQueue) error { _, err := q.Post(context.Background(), outbound()); return err }
	refusals := map[string]struct {
		status int
		kind   string
		call   func(*delivery.HTTPQueue) error
	}{
		"a claim's 403":                          {http.StatusForbidden, "permission_denied", claim},
		"a claim's 409":                          {http.StatusConflict, "failed_precondition", claim},
		"a renewal's 403":                        {http.StatusForbidden, "permission_denied", func(q *delivery.HTTPQueue) error { _, err := q.Renew(context.Background(), "c"); return err }},
		"a completion's 403":                     {http.StatusForbidden, "permission_denied", func(q *delivery.HTTPQueue) error { return q.Complete(context.Background(), "c") }},
		"a submission's 403":                     {http.StatusForbidden, "permission_denied", submit},
		"a submission's 409 failed_precondition": {http.StatusConflict, "failed_precondition", submit},
	}
	for name, refusal := range refusals {
		t.Run(name, func(t *testing.T) {
			calls := 0
			queue, execution := garamAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Garam-Contract-Version", delivery.TokenDeliveryContract)
				writeJSON(w, refusal.status, machinegen.Error{Kind: refusal.kind, Message: "execution token is not current"})
			})
			err := refusal.call(queue)
			assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "%v", err)
			assert.Contains(t, err.Error(), "execution token is not current", "garam's own words are kept")
			_, err = execution.Present()
			assert.True(t, errors.Is(err, delivery.ErrInactive), "the execution is closed")
			assert.Error(t, refusal.call(queue))
			assert.Equal(t, 1, calls, "nothing more reaches garam")
		})
	}

	undecided, execution := garamAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, machinegen.Error{Kind: "unavailable", Message: "internal error"})
	})
	_, err := undecided.Claim(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
	_, err = execution.Present()
	assert.NoError(t, err, "the control: an undecided answer closes nothing")
}

// outbound is one reply as the forwarder offers it.
func outbound() delivery.Outbound {
	return delivery.Outbound{
		Recipient: senderGRN(), IdempotencyKey: "entry-1", Body: "reply",
		CreatedAt: time.Date(2026, 10, 5, 1, 2, 3, 456789000, time.UTC),
	}
}

// TestHTTPQueue_PostOffersAReplyUnderDurableDelivery covers the
// submission as durable-delivery.v1 pins it and the answers it sorts by
// status and kind, never by text: a 201 under the contract is what was
// stored, and a 404, an answer outside the contract, an undecided 5xx,
// a future creation instant and every other refusal each read as their
// own kind.
func TestHTTPQueue_PostOffersAReplyUnderDurableDelivery(t *testing.T) {
	var got map[string]any
	var version, route string
	queue, _ := garamAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		route, version = r.URL.Path, r.Header.Get("Garam-Contract-Version")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.Header().Set("Garam-Contract-Version", delivery.TokenDeliveryContract)
		writeJSON(w, http.StatusCreated, machinegen.Message{
			Id: "stored-1", Sender: agentGRN().String(), Recipient: senderGRN().String(), IdempotencyKey: "entry-1",
		})
	})
	stored, err := queue.Post(context.Background(), outbound())
	require.NoError(t, err)
	assert.Equal(t, "/outbox-messages", route)
	assert.Equal(t, delivery.TokenDeliveryContract, version)
	assert.Equal(t, map[string]any{
		"recipient": senderGRN().String(), "idempotencyKey": "entry-1", "body": "reply",
		"createdAt": "2026-10-05T01:02:03.456789Z",
	}, got)
	assert.True(t, stored.Acknowledges(agentGRN(), outbound()))

	answers := map[string]struct {
		status  int
		version string
		kind    string
		want    errorx.Kind
	}{
		"an old server's 404":            {http.StatusNotFound, "", "not_found", errorx.KindUnimplemented},
		"an answer outside the contract": {http.StatusCreated, "", "", errorx.KindUnimplemented},
		"an undecided 503":               {http.StatusServiceUnavailable, "", "unavailable", errorx.KindUnavailable},
		"a future creation instant":      {http.StatusBadRequest, delivery.TokenDeliveryContract, "out_of_range", errorx.KindOutOfRange},
		"an invalid submission":          {http.StatusBadRequest, delivery.TokenDeliveryContract, "invalid_argument", errorx.KindFailedPrecondition},
		"an expired entry":               {http.StatusGone, delivery.TokenDeliveryContract, "deadline_exceeded", errorx.KindFailedPrecondition},
		"a conflicting key":              {http.StatusConflict, delivery.TokenDeliveryContract, "already_exists", errorx.KindFailedPrecondition},
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			queue, execution := garamAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
				if answer.version != "" {
					w.Header().Set("Garam-Contract-Version", answer.version)
				}
				writeJSON(w, answer.status, machinegen.Error{Kind: answer.kind, Message: "refused"})
			})
			_, err := queue.Post(context.Background(), outbound())
			assert.True(t, errorx.IsKind(err, answer.want), "%v", err)
			_, err = execution.Present()
			assert.NoError(t, err, "no submission refusal but the fence's closes the execution")
		})
	}
}

// TestStored_AcknowledgesOnlyTheReplyItWasOfferedAs holds the check
// #1169's 17:16 decision requires before an entry goes: a non-empty
// identifier, this agent as sender, the entry's target as recipient and
// its identifier as key. The stored message that matches all four is
// the control.
func TestStored_AcknowledgesOnlyTheReplyItWasOfferedAs(t *testing.T) {
	o := outbound()
	match := delivery.Stored{ID: "stored-1", Sender: agentGRN().String(), Recipient: senderGRN().String(), IdempotencyKey: "entry-1"}
	require.True(t, match.Acknowledges(agentGRN(), o), "the control: the stored reply acknowledges it")

	mismatches := map[string]func(s *delivery.Stored){
		"no identifier":  func(s *delivery.Stored) { s.ID = "" },
		"another sender": func(s *delivery.Stored) { s.Sender = senderGRN().String() },
		"another target": func(s *delivery.Stored) { s.Recipient = agentGRN().String() },
		"another key":    func(s *delivery.Stored) { s.IdempotencyKey = "refusal:entry-1" },
	}
	for name, mismatch := range mismatches {
		t.Run(name, func(t *testing.T) {
			s := match
			mismatch(&s)
			assert.False(t, s.Acknowledges(agentGRN(), o))
		})
	}
}

// TestNewHTTPQueue_RefusesWhatItCannotCall keeps a misconfiguration at
// the start rather than in the first poll. The working arguments are
// the control: each case below changes one of them.
func TestNewHTTPQueue_RefusesWhatItCannotCall(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	execution := delivery.HeldExecution("held-token")

	_, err := delivery.NewHTTPQueue(server.Client(), server.URL, agentGRN(), execution, agentCredential())
	require.NoError(t, err, "an address and an agent that work")

	tests := map[string]struct {
		client    *http.Client
		base      string
		agent     grn.GRN
		execution *delivery.Execution
	}{
		"no client":                  {nil, server.URL, agentGRN(), execution},
		"no execution":               {server.Client(), server.URL, agentGRN(), nil},
		"a relative address":         {server.Client(), "/agents", agentGRN(), execution},
		"an address that is not one": {server.Client(), "://", agentGRN(), execution},
		"an agent that is not a GRN": {server.Client(), server.URL, grn.GRN{}, execution},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := delivery.NewHTTPQueue(tt.client, tt.base, tt.agent, tt.execution, agentCredential())
			require.Error(t, err)
		})
	}
}
