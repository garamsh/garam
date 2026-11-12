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

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/api"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/identity"
	identitypg "github.com/garamsh/garam/platform/internal/identity/postgres"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/signer"
)

const issuerURL = "https://garam.test/api"

// issuerStack is the device stack with garam's OIDC issuer on its
// browser listener and a machine listener beside it that authenticates
// service principals, as `serve api` and `serve machine` wire them
// (ADR-0141, ADR-0129). The keys are generated here. The organization
// "globex" holds a principal of cowork and of codehub, which are
// first-party, and of policy, an audience no first-party service is
// yet, built through the org store's seam so that introspection has a
// caller to be tested with.
type issuerStack struct {
	*deviceStack
	key       *signer.Key
	issuer    *identity.Issuer
	keys      identity.ServiceKeys
	machine   string
	policy    *rsa.PrivateKey
	cowork    *rsa.PrivateKey
	codehub   *rsa.PrivateKey
	globex    string
	policyID  grn.GRN
	coworkID  grn.GRN
	codehubID grn.GRN
}

func newIssuerStack(t *testing.T) *issuerStack {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	key, err := signer.ParseKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)

	s := &issuerStack{key: key, globex: "globex"}
	s.deviceStack = newDeviceStack(t, func(p *pgxpool.Pool, srv *api.Server) {
		var err error
		s.issuer, err = identity.NewIssuer(issuerURL, key, authnpg.NewSessionStore(p), nil)
		require.NoError(t, err)
		srv.WithIdentity(s.issuer)
	})
	require.NoError(t, orgpg.New(s.pool).WithServices(identity.AudiencePolicy, identity.ServiceCowork, identity.ServiceCodehub).
		CreateOrg(t.Context(), org.NewOrganization(orgRootGRN(s.globex), s.globex)))
	s.policyID = identity.PrincipalGRN(s.globex, identity.AudiencePolicy)
	s.coworkID = identity.PrincipalGRN(s.globex, identity.ServiceCowork)
	s.codehubID = identity.PrincipalGRN(s.globex, identity.ServiceCodehub)

	s.policy, s.cowork, s.codehub = serviceKey(t), serviceKey(t), serviceKey(t)
	s.keys = identity.ServiceKeys{
		identity.AudiencePolicy: {publicKey(t, s.policy)},
		identity.ServiceCowork:  {publicKey(t, s.cowork)},
		identity.ServiceCodehub: {publicKey(t, s.codehub)},
	}
	s.machine = s.machineListener(t, s.issuer)
	return s
}

func serviceKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, operator.MinKeyBits)
	require.NoError(t, err)
	return private
}

func publicKey(t *testing.T, private *rsa.PrivateKey) operator.Key {
	t.Helper()
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	return operator.Key{KID: kid, Algorithm: operator.KeyAlgorithm, Public: &private.PublicKey}
}

// machineListener serves another machine listener over the stack's
// database, the issuer given and the stack's keys, as a second replica
// of `serve machine` does, and answers its URL. A nil issuer is the
// issuer off.
func (s *issuerStack) machineListener(t *testing.T, issuer *identity.Issuer) string {
	t.Helper()
	var handler http.Handler
	listener := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r) }))
	t.Cleanup(listener.Close)
	principals := identity.NewPrincipals(identity.PrincipalsConfig{
		Keys:               identity.FixedKeys(s.keys),
		ExchangeEndpoint:   listener.URL + "/oauth/token",
		IntrospectEndpoint: listener.URL + "/oauth/introspect",
		Issuer:             issuerURL,
		Admitter:           identitypg.New(s.pool),
	})
	handler = machinegen.HandlerWithOptions(
		machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).WithIdentity(issuer, principals),
		machinegen.StdHTTPServerOptions{ErrorHandlerFunc: machineapi.BindingError})
	return listener.URL
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

// principalAssertion signs a client assertion as principal, addressed to the
// URL named by to, with the jti given or a fresh one.
func principalAssertion(t *testing.T, private *rsa.PrivateKey, principal grn.GRN, to, jti string) string {
	t.Helper()
	return principalAssertionAs(t, private, principal, "JWT", to, jti)
}

// principalAssertionAs signs the assertion under header typ, naming aud
// as given: a string, or a list for an assertion naming two audiences.
func principalAssertionAs(t *testing.T, private *rsa.PrivateKey, principal grn.GRN, typ string, aud any, jti string) string {
	t.Helper()
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	signing, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private},
		(&jose.SignerOptions{}).WithType(jose.ContentType(typ)).WithHeader("kid", kid))
	require.NoError(t, err)
	if jti == "" {
		raw := make([]byte, 32)
		_, err = rand.Read(raw)
		require.NoError(t, err)
		jti = base64.RawURLEncoding.EncodeToString(raw)
	}
	client := principal.String()
	payload, err := json.Marshal(map[string]any{
		"iss": client, "sub": client, "aud": aud,
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(), "jti": jti,
	})
	require.NoError(t, err)
	object, err := signing.Sign(payload)
	require.NoError(t, err)
	compact, err := object.CompactSerialize()
	require.NoError(t, err)
	return compact
}

// post sends a form to the machine listener at url and answers the
// status and body.
func post(t *testing.T, url string, form url.Values) (int, string) {
	t.Helper()
	resp, err := http.PostForm(url, form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

// introspectAs asks the machine listener at listener about token as
// principal, signing with private under the jti given or a fresh one.
func (s *issuerStack) introspectAs(t *testing.T, listener string, private *rsa.PrivateKey, principal grn.GRN, jti, token string) (int, string) {
	t.Helper()
	return post(t, listener+"/oauth/introspect", url.Values{
		"token": {token}, "client_id": {principal.String()},
		"client_assertion":      {principalAssertion(t, private, principal, listener+"/oauth/introspect", jti)},
		"client_assertion_type": {operator.ClientAssertionTypeJWT},
	})
}

// introspect asks as the principal of policy, which is the audience
// the tokens of the issuer's exchange are for.
func (s *issuerStack) introspect(t *testing.T, token string) (int, string) {
	t.Helper()
	return s.introspectAs(t, s.machine, s.policy, s.policyID, "", token)
}

// exchangeAs asks the machine listener at listener for a token as
// principal, naming audience.
func (s *issuerStack) exchangeAs(t *testing.T, listener string, private *rsa.PrivateKey, principal grn.GRN, jti, audience string) (int, string) {
	t.Helper()
	form := url.Values{
		"grant_type": {operator.GrantTypeClientCredentials}, "client_id": {principal.String()},
		"client_assertion":      {principalAssertion(t, private, principal, listener+"/oauth/token", jti)},
		"client_assertion_type": {operator.ClientAssertionTypeJWT},
	}
	if audience != "" {
		form.Set("audience", audience)
	}
	return post(t, listener+"/oauth/token", form)
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
