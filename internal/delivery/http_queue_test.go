package delivery_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/grn"
)

// garamAnswering is garam's machine listener answering one way to
// everything, so a test can hold the client to what one answer means.
func garamAnswering(t *testing.T, answer http.HandlerFunc) *delivery.HTTPQueue {
	t.Helper()
	server := httptest.NewServer(answer)
	t.Cleanup(server.Close)
	queue, err := delivery.NewHTTPQueue(server.Client(), server.URL, agentGRN())
	require.NoError(t, err)
	return queue
}

// TestHTTPQueue_ClaimReadsTheHoldGaramAnsweredWith covers the call as
// api/machine.yaml describes it — the route it goes to, and the hold
// the loop reads out of the answer. The agent is a GRN and a GRN
// carries colons, so what the path segment survives as is part of it.
func TestHTTPQueue_ClaimReadsTheHoldGaramAnsweredWith(t *testing.T) {
	expires := time.Now().Add(30 * time.Second).UTC().Truncate(time.Second)
	var route, method string
	queue := garamAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		route, method = r.URL.Path, r.Method
		writeJSON(w, http.StatusOK, machinegen.Claim{
			Id:        "claim-1",
			ExpiresAt: expires,
			Attempt:   1,
			Message:   machinegen.Message{Id: "message-1", Body: "what the sender said"},
		})
	})

	claim, err := queue.Claim(context.Background())
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "/agents/"+agentGRN().String()+"/claims", route)
	assert.Equal(t, delivery.ClaimID("claim-1"), claim.ID)
	assert.Equal(t, expires, claim.ExpiresAt.UTC())
	assert.Equal(t, "what the sender said", claim.Body)
}

// TestHTTPQueue_ClaimAnswersNoMessageReadyOnAnEmptyPoll holds the 204
// apart from a refusal: a poll that waited its time and found nothing
// is the ordinary answer, and the loop asks again rather than treating
// it as a failure.
func TestHTTPQueue_ClaimAnswersNoMessageReadyOnAnEmptyPoll(t *testing.T) {
	queue := garamAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
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
	standing := garamAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		routes = append(routes, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/completion") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, http.StatusOK, machinegen.Claim{Id: "claim-1", ExpiresAt: time.Now().Add(time.Minute)})
	})

	renewed, err := standing.Renew(context.Background(), "claim-1")
	require.NoError(t, err)
	assert.Equal(t, delivery.ClaimID("claim-1"), renewed.ID)
	require.NoError(t, standing.Complete(context.Background(), "claim-1"))
	assert.Equal(t, []string{
		"/agents/" + agentGRN().String() + "/claims/claim-1/renewal",
		"/agents/" + agentGRN().String() + "/claims/claim-1/completion",
	}, routes)

	gone := garamAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusConflict, machinegen.Error{Kind: "failed_precondition", Message: "message claim is stale"})
	})

	_, err = gone.Renew(context.Background(), "claim-1")
	assert.True(t, errors.Is(err, delivery.ErrClaimLapsed))
	assert.True(t, errors.Is(gone.Complete(context.Background(), "claim-1"), delivery.ErrClaimLapsed))
}

// TestHTTPQueue_ReportsWhatGaramRefusedWith covers every other refusal:
// the loop retries them all, so what matters is that the line it logs
// carries garam's own words rather than a status alone.
func TestHTTPQueue_ReportsWhatGaramRefusedWith(t *testing.T) {
	queue := garamAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, machinegen.Error{Kind: "permission_denied", Message: "caller is not the recipient"})
	})

	_, err := queue.Claim(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission_denied")
	assert.Contains(t, err.Error(), "caller is not the recipient")
	assert.False(t, errors.Is(err, delivery.ErrClaimLapsed), "only a conflict says the hold is gone")
}

// TestNewHTTPQueue_RefusesWhatItCannotCall keeps a misconfiguration at
// the start rather than in the first poll. The working arguments are
// the control: each case below changes one of them.
func TestNewHTTPQueue_RefusesWhatItCannotCall(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)

	_, err := delivery.NewHTTPQueue(server.Client(), server.URL, agentGRN())
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
			_, err := delivery.NewHTTPQueue(tt.client, tt.base, tt.agent)
			require.Error(t, err)
		})
	}
}
