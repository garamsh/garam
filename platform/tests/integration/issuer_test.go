package integration_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/api"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/signer"
)

const issuerURL = "https://garam.test/api"

// issuerStack is the device stack with garam's OIDC issuer on its
// browser listener and introspection on a machine listener, as `serve
// api` and `serve machine` wire them (ADR-0141). The key is generated
// here; policy holds the other half of the service key pair.
type issuerStack struct {
	*deviceStack
	key     *signer.Key
	machine string
	policy  *rsa.PrivateKey
}

func newIssuerStack(t *testing.T) *issuerStack {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	key, err := signer.ParseKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)

	var issuer *identity.Issuer
	s := &issuerStack{key: key}
	s.deviceStack = newDeviceStack(t, func(p *pgxpool.Pool, srv *api.Server) {
		var err error
		issuer, err = identity.NewIssuer(issuerURL, key, authnpg.NewSessionStore(p), nil)
		require.NoError(t, err)
		srv.WithIdentity(issuer)
	})

	s.policy, err = rsa.GenerateKey(rand.Reader, operator.MinKeyBits)
	require.NoError(t, err)
	keys, err := json.Marshal(map[string]jose.JSONWebKeySet{
		"policy": {Keys: []jose.JSONWebKey{{Key: &s.policy.PublicKey, Algorithm: "RS256", Use: "sig"}}},
	})
	require.NoError(t, err)

	var handler http.Handler
	machine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(machine.Close)
	callers, err := identity.ParseCallers(keys, machine.URL+"/oauth/introspect", nil)
	require.NoError(t, err)
	handler = machinegen.HandlerWithOptions(
		machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).WithIdentity(issuer, callers),
		machinegen.StdHTTPServerOptions{ErrorHandlerFunc: machineapi.BindingError})
	s.machine = machine.URL
	return s
}

// serve makes one request and answers the response exactly as served:
// its status line, headers and body.
func serve(t *testing.T, req *http.Request) (*http.Response, string) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	dump, err := httputil.DumpResponse(resp, true)
	require.NoError(t, err)
	return resp, string(dump)
}

func (s *issuerStack) get(t *testing.T, path string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.url+"/api"+path, nil)
	require.NoError(t, err)
	resp, dump := serve(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode, dump)
	return dump
}

func (s *issuerStack) exchange(t *testing.T, subject, audience string) (*http.Response, string) {
	t.Helper()
	body := url.Values{
		"grant_type":         {identity.GrantTypeTokenExchange},
		"subject_token":      {subject},
		"subject_token_type": {identity.TokenTypeAccessToken},
		"audience":           {audience},
	}.Encode()
	req, err := http.NewRequest(http.MethodPost, s.url+"/api/oauth/token", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return serve(t, req)
}

func bodyOf(dump string) string {
	_, body, _ := strings.Cut(dump, "\r\n\r\n")
	return body
}

// introspect asks the machine listener about token as policy, with a
// fresh assertion each time.
func (s *issuerStack) introspect(t *testing.T, token string) (int, string) {
	t.Helper()
	kid, err := operator.Thumbprint(&s.policy.PublicKey)
	require.NoError(t, err)
	signing, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: s.policy},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	require.NoError(t, err)
	client := identity.CallerGRN("policy").String()
	jti := make([]byte, 32)
	_, err = rand.Read(jti)
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{
		"iss": client, "sub": client, "aud": s.machine + "/oauth/introspect",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
		"jti": base64.RawURLEncoding.EncodeToString(jti),
	})
	require.NoError(t, err)
	object, err := signing.Sign(payload)
	require.NoError(t, err)
	assertion, err := object.CompactSerialize()
	require.NoError(t, err)

	form := url.Values{
		"token": {token}, "client_id": {client}, "client_assertion": {assertion},
		"client_assertion_type": {operator.ClientAssertionTypeJWT},
	}
	resp, err := http.PostForm(s.machine+"/oauth/introspect", form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

// TestIssuer_ExchangesASessionAndAnswersForItWhileItsRowLives runs the
// issuer end to end over Postgres: a CLI session from the device grant
// is exchanged, the token verifies against the served key set, the
// machine listener introspects it as active, and once the session row
// is gone the exchange is refused and introspection answers inactive.
// It logs the discovery document, the key set and the exchange's
// response exactly as served.
func TestIssuer_ExchangesASessionAndAnswersForItWhileItsRowLives(t *testing.T) {
	s := newIssuerStack(t)

	t.Log("GET /api/.well-known/openid-configuration\n" + s.get(t, "/.well-known/openid-configuration"))
	t.Log("GET /api/oauth/jwks\n" + s.get(t, "/oauth/jwks"))

	tokens := s.approvedTokens(t)
	resp, dump := s.exchange(t, tokens.AccessToken, "policy")
	t.Log("POST /api/oauth/token\n" + dump)
	require.Equal(t, http.StatusOK, resp.StatusCode, dump)

	var granted apigen.TokenExchangeResponse
	require.NoError(t, json.Unmarshal([]byte(bodyOf(dump)), &granted))
	claims, err := s.key.Verify(granted.AccessToken, time.Now())
	require.NoError(t, err)
	assert.Equal(t, issuerURL, claims.Issuer)
	assert.Equal(t, s.adaGRN.String(), claims.Subject)
	assert.Equal(t, "policy", claims.Audience)
	assert.Equal(t, 300*time.Second, claims.Lifetime)
	assert.Equal(t, 1, countRows(t, s.pool, "SELECT count(*) FROM sessions WHERE id::text = $1", claims.SessionID),
		"sid is the session's row")

	status, body := s.introspect(t, granted.AccessToken)
	require.Equal(t, http.StatusOK, status, body)
	var active machinegen.Introspection
	require.NoError(t, json.Unmarshal([]byte(body), &active))
	assert.True(t, active.Active)
	assert.Equal(t, claims.SessionID, *active.Sid)

	// A refresh token, and an access token read as the wrong audience,
	// buy nothing.
	resp, dump = s.exchange(t, tokens.RefreshToken, "policy")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "a refresh token is no subject token: %s", dump)
	resp, dump = s.exchange(t, tokens.AccessToken, "cowork")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "an unknown audience: %s", dump)

	_, err = s.pool.Exec(t.Context(), "DELETE FROM sessions WHERE kind = 'cli'")
	require.NoError(t, err)

	resp, dump = s.exchange(t, tokens.AccessToken, "policy")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, dump)
	assert.Equal(t, apigen.InvalidGrant, oauthError(t, bodyOf(dump)).Error, "a revoked session")

	status, body = s.introspect(t, granted.AccessToken)
	require.Equal(t, http.StatusOK, status, body)
	assert.JSONEq(t, `{"active":false}`, body, "the row is gone, the token still unexpired")
}
