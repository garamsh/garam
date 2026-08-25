package e2e_test

import (
	"context"
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

// stubProvider drives /auth/login/{provider} without an issuer. The
// bound under test is the store's, and Login reaches a provider only
// for the name and the redirect URL, so a value satisfying
// authn.Provider is the whole dependency. The shipped providers do not
// serve here: constructing the Google one runs OIDC discovery against
// its issuer, which a test may not reach.
type stubProvider struct{}

func (stubProvider) Name() string { return "stub" }

func (stubProvider) AuthCodeURL(state, _ string) string {
	return "https://idp.example/auth?state=" + state
}

func (stubProvider) Exchange(context.Context, string, string) (*authn.Identity, error) {
	return &authn.Identity{Subject: "stub", Nickname: "Stub"}, nil
}

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

	// Login reaches only the state store and the provider; the
	// dependencies its sibling handlers use stay nil rather than
	// doubled.
	srv := api.NewServer(nil, nil, nil, nil, nil, nil, nil,
		authn.NewPostgresOAuthStateStore(p), nil, nil, []authn.Provider{stubProvider{}}, nil, nil,
		api.Config{SessionTTL: time.Hour})
	handler := apigen.Handler(srv)

	login := func() int {
		req := httptest.NewRequest(http.MethodGet, "/auth/login/stub", nil)
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
