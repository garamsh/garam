package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/errorx"
)

// tokenEndpoint is a token endpoint that answers each poll with the
// next of answers and records when each poll arrived.
type tokenEndpoint struct {
	mu      sync.Mutex
	answers []any
	at      []time.Time
}

func (e *tokenEndpoint) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.at = append(e.at, time.Now())
	answer := e.answers[0]
	if len(e.answers) > 1 {
		e.answers = e.answers[1:]
	}
	w.Header().Set("Content-Type", "application/json")
	if _, ok := answer.(apigen.OAuthError); ok {
		w.WriteHeader(http.StatusBadRequest)
	}
	_ = json.NewEncoder(w).Encode(answer)
}

func pollAgainst(t *testing.T, answers ...any) (*tokenEndpoint, *apigen.SessionToken, error) {
	t.Helper()
	endpoint := &tokenEndpoint{answers: answers}
	srv := httptest.NewServer(endpoint)
	t.Cleanup(srv.Close)
	c := &userCommand{endpoint: srv.URL, http: srv.Client(), slowDown: 50 * time.Millisecond}
	client, err := apigen.NewClientWithResponses(srv.URL, apigen.WithHTTPClient(srv.Client()))
	require.NoError(t, err)
	tokens, err := c.poll(context.Background(), client, &apigen.DeviceCode{DeviceCode: "d", ExpiresIn: 600})
	return endpoint, tokens, err
}

func refusal(code apigen.OAuthErrorError) apigen.OAuthError { return apigen.OAuthError{Error: code} }

// TestPoll_SlowsDownWhenToldAndStopsOnTheToken: a slow_down moves the
// interval later before the next poll; the polls before it, at the
// code's own interval of zero, are the control.
func TestPoll_SlowsDownWhenToldAndStopsOnTheToken(t *testing.T) {
	endpoint, tokens, err := pollAgainst(t,
		refusal(apigen.AuthorizationPending), refusal(apigen.SlowDown),
		apigen.SessionToken{AccessToken: "a", TokenType: apigen.Bearer})
	require.NoError(t, err)
	assert.Equal(t, "a", tokens.AccessToken)
	require.Len(t, endpoint.at, 3)
	assert.Less(t, endpoint.at[1].Sub(endpoint.at[0]), 50*time.Millisecond)
	assert.GreaterOrEqual(t, endpoint.at[2].Sub(endpoint.at[1]), 50*time.Millisecond)
}

// TestPoll_EndsOnADenialOrALapse, each with its own kind.
func TestPoll_EndsOnADenialOrALapse(t *testing.T) {
	_, _, err := pollAgainst(t, refusal(apigen.AuthorizationPending), refusal(apigen.AccessDenied))
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "%v", err)
	_, _, err = pollAgainst(t, refusal(apigen.ExpiredToken))
	assert.True(t, errorx.IsKind(err, errorx.KindDeadlineExceeded), "%v", err)
}
