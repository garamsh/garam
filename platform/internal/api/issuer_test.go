package api_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/signer"
)

var issuerRoutes = []struct{ method, path string }{
	{http.MethodGet, "/.well-known/openid-configuration"},
	{http.MethodGet, "/oauth/jwks"},
	{http.MethodPost, "/oauth/token"},
}

// TestIssuer_IsOffWithoutAnIssuer: with no issuer configured none of
// the three operations answers anything but the 404 an unrouted path
// gets.
func TestIssuer_IsOffWithoutAnIssuer(t *testing.T) {
	f := newFixture(t)
	for _, route := range issuerRoutes {
		rec := f.do(t, route.method, route.path, "")
		assert.Equal(t, http.StatusNotFound, rec.Code, route.path)
		assert.Equal(t, "404 page not found\n", rec.Body.String(), route.path)
	}
}

// liveSessions answers one live CLI session, token "cli-access".
type liveSessions struct{}

func (liveSessions) Get(_ context.Context, token string) (*authn.Session, error) {
	if token != "cli-access" {
		return nil, authn.ErrSessionNotFound
	}
	return &authn.Session{User: grn.New(grn.RootOwner, "user", "ada"), ID: "sid", Kind: authn.SessionCLI,
		ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (liveSessions) GetByID(context.Context, string) (*authn.Session, error) {
	return nil, authn.ErrSessionNotFound
}

func issuerHandler(t *testing.T) http.Handler {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	key, err := signer.ParseKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)
	issuer, err := identity.NewIssuer("https://garam.test/api", key, liveSessions{}, nil)
	require.NoError(t, err)
	srv := api.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, api.Config{}).WithIdentity(issuer)
	return apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError})
}

func exchangeForm(extra url.Values) string {
	form := url.Values{
		"grant_type":         {identity.GrantTypeTokenExchange},
		"subject_token":      {"cli-access"},
		"subject_token_type": {identity.TokenTypeAccessToken},
		"audience":           {"policy"},
	}
	for name, values := range extra {
		form[name] = values
	}
	return form.Encode()
}

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestExchangeSessionToken_AnswersTheTokenResponse(t *testing.T) {
	rec := post(issuerHandler(t), exchangeForm(nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var got apigen.TokenExchangeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.NotEmpty(t, got.AccessToken)
	assert.Equal(t, "Bearer", got.TokenType)
	assert.EqualValues(t, identity.TokenTypeAccessToken, got.IssuedTokenType)
	assert.Equal(t, 300, got.ExpiresIn)
}

func TestExchangeSessionToken_RefusesWithTheCodeEachDefectEarns(t *testing.T) {
	h := issuerHandler(t)
	for name, c := range map[string]struct {
		body string
		code apigen.OAuthErrorError
	}{
		"another grant":       {exchangeForm(url.Values{"grant_type": {"password"}}), apigen.UnsupportedGrantType},
		"a scope":             {exchangeForm(url.Values{"scope": {"x"}}), apigen.InvalidRequest},
		"a resource":          {exchangeForm(url.Values{"resource": {"x"}}), apigen.InvalidRequest},
		"an actor token":      {exchangeForm(url.Values{"actor_token": {"x"}}), apigen.InvalidRequest},
		"a repeated audience": {exchangeForm(nil) + "&audience=codehub", apigen.InvalidRequest},
		"a subject token type": {exchangeForm(url.Values{
			"subject_token_type": {"urn:ietf:params:oauth:token-type:refresh_token"}}), apigen.InvalidRequest},
		"an unknown audience": {exchangeForm(url.Values{"audience": {"cowork"}}), apigen.InvalidTarget},
		"an unknown session":  {exchangeForm(url.Values{"subject_token": {"revoked"}}), apigen.InvalidGrant},
	} {
		assert.Equal(t, c.code, refusalOf(t, post(h, c.body)), name)
	}
}

func TestIssuer_ServesItsDocumentsAsJSON(t *testing.T) {
	h := issuerHandler(t)
	for _, path := range []string{"/.well-known/openid-configuration", "/oauth/jwks"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, rec.Code, path)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), path)
		assert.True(t, json.Valid(rec.Body.Bytes()), path)
	}
}
