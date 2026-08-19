package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
)

// TestLogin_EndpointShedsPastTheLiveStateBound drives
// /auth/login/{provider} over HTTP against the real store, at the rate
// the code before the bound accepted: one unauthenticated request, one
// database write, nothing in the way. The requests up to
// MaxLiveOAuthStates redirect; the next one answers 429 and writes
// nothing, and the table holds exactly the bound. The accepted
// requests are the control — the refusal is the bound, not an endpoint
// that refuses logins.
func TestLogin_EndpointShedsPastTheLiveStateBound(t *testing.T) {
	p := pool(t)

	dev, err := authn.NewDevProvider(authn.DevConfig{RedirectURL: "http://web.test/api/auth/callback/dev"})
	require.NoError(t, err)
	// Login reaches only the state store and the provider; the
	// dependencies its sibling handlers use stay nil rather than
	// doubled.
	srv := api.NewServer(nil, nil, nil, nil, nil, nil,
		authn.NewPostgresOAuthStateStore(p), []authn.Provider{dev}, nil, nil,
		api.Config{SessionTTL: time.Hour})
	handler := apigen.Handler(srv)

	login := func() int {
		req := httptest.NewRequest(http.MethodGet, "/auth/login/dev", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	for range authn.MaxLiveOAuthStates {
		require.Equal(t, http.StatusFound, login())
	}
	assert.Equal(t, http.StatusTooManyRequests, login())
	assert.Equal(t, authn.MaxLiveOAuthStates, countRows(t, p,
		"SELECT count(*) FROM oauth_states"),
		"the refused request wrote nothing")
}
