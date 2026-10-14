package e2e_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// The client assertion type of RFC 7523, spelled here for the reason
// the probe paths are.
const jwtBearer = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

// issuedKey is the part of issueOperatorKey's answer a person places
// in the operator's cluster.
type issuedKey struct {
	Key struct {
		Kid string `json:"kid"`
	} `json:"key"`
	PrivateKeyPem string `json:"privateKeyPem"`
	ClientID      string `json:"clientId"`
	TokenURL      string `json:"tokenUrl"`
}

// signAssertion signs a client assertion with the PEM the issuance
// answered, through go-jose alone, as an operator outside garam does.
func signAssertion(t *testing.T, key issuedKey) string {
	t.Helper()
	block, _ := pem.Decode([]byte(key.PrivateKeyPem))
	require.NotNil(t, block)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	private, ok := parsed.(*rsa.PrivateKey)
	require.True(t, ok)
	jwk := jose.JSONWebKey{Key: &private.PublicKey}
	thumbprint, err := jwk.Thumbprint(crypto.SHA256)
	require.NoError(t, err)
	require.Equal(t, key.Key.Kid, base64.RawURLEncoding.EncodeToString(thumbprint),
		"the fingerprint answered is the PEM's own RFC 7638 thumbprint")

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", key.Key.Kid))
	require.NoError(t, err)
	var jti [16]byte
	_, err = rand.Read(jti[:])
	require.NoError(t, err)
	now := time.Now()
	claims, err := json.Marshal(map[string]any{
		"iss": key.ClientID, "sub": key.ClientID, "aud": key.TokenURL,
		"iat": now.Unix(), "exp": now.Add(time.Minute).Unix(),
		"jti": base64.RawURLEncoding.EncodeToString(jti[:]),
	})
	require.NoError(t, err)
	signed, err := signer.Sign(claims)
	require.NoError(t, err)
	compact, err := signed.CompactSerialize()
	require.NoError(t, err)
	return compact
}

// TestOperatorKey_TheBuiltBinariesIssueAKeyAndExchangeItsAssertion is
// ADR-0102 end to end on what the build produces: `serve api` issues
// a key under the console's route, answering the PEM, the operator's
// GRN and the configured token URL; x/oauth2's client credentials flow
// presents a go-jose assertion to `serve machine` over its verified
// server TLS and obtains a token; the token authenticates as the
// operator under Bearer; a replay of the assertion is invalid_client;
// and withdrawing the key refuses the token on the next request over
// the same connection. A second `serve api` with no token URL issues
// nothing and is the control on the configuration.
func TestOperatorKey_TheBuiltBinariesIssueAKeyAndExchangeItsAssertion(t *testing.T) {
	garam := buildGaram(t)
	p := prepareDatabase(t, buildPrincipalFixture(t))
	grpcAddr, metricsAddr := freeAddr(t), freeAddr(t)
	apiAddr, unconfiguredAddr := freeAddr(t), freeAddr(t)
	machineAddr, machineHealthAddr := freeAddr(t), freeAddr(t)
	tokenURL := "https://" + machineAddr + "/oauth/token"

	serve(t, garam, "authz", []string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL, "GARAM_GRPC_ADDR=" + grpcAddr, "GARAM_METRICS_ADDR=" + metricsAddr,
	}, "http://"+metricsAddr+readyPath)
	serve(t, garam, "api", []string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL, "GARAM_HTTP_ADDR=" + apiAddr, "GARAM_AUTHZ_ADDR=" + grpcAddr,
		"GARAM_MACHINE_TOKEN_URL=" + tokenURL,
	}, "http://"+apiAddr+readyPath)
	serve(t, garam, "api", []string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL, "GARAM_HTTP_ADDR=" + unconfiguredAddr, "GARAM_AUTHZ_ADDR=" + grpcAddr,
	}, "http://"+unconfiguredAddr+readyPath)
	certFile, keyFile, kekFile, rootFile := machineKeyMaterialFiles(t)
	serve(t, garam, "machine", []string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL, "GARAM_AUTHZ_ADDR=" + grpcAddr,
		"GARAM_MACHINE_ADDR=" + machineAddr, "GARAM_MACHINE_HEALTH_ADDR=" + machineHealthAddr,
		"GARAM_MACHINE_TLS_CERT_FILE=" + certFile, "GARAM_MACHINE_TLS_KEY_FILE=" + keyFile,
		"GARAM_CA_KEY_FILE=" + kekFile, "GARAM_MACHINE_SERVER_ROOT_FILE=" + rootFile,
		"GARAM_MACHINE_ISSUER=https://garam.test", "GARAM_MACHINE_TOKEN_URL=" + tokenURL,
	}, "http://"+machineHealthAddr+readyPath)

	c := consumerClient{t: t, base: "http://" + apiAddr}
	owner := p.Session.Token
	slug := c.createOrg(owner, "keyed")
	keys := "/orgs/" + slug + "/operators/controller/keys"

	status, body := consumerClient{t: t, base: "http://" + unconfiguredAddr}.do(http.MethodPost, keys, owner, `{"rotation":"planned"}`)
	assert.Equal(t, http.StatusServiceUnavailable, status, "no token URL issues no key: %s", body)
	assert.NotContains(t, body, "PRIVATE KEY")

	status, body = c.do(http.MethodPost, keys, owner, `{"rotation":"planned"}`)
	require.Equal(t, http.StatusCreated, status, body)
	var key issuedKey
	require.NoError(t, json.Unmarshal([]byte(body), &key))
	assert.Equal(t, "grn:"+slug+":default:operator:controller", key.ClientID)
	assert.Equal(t, tokenURL, key.TokenURL)

	// The listener is verified against the server root, as an operator
	// pins garam by it; no client certificate is presented.
	root, err := os.ReadFile(rootFile)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(root))
	var connections int
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: roots, MinVersion: tls.VersionTLS13,
		VerifyConnection: func(tls.ConnectionState) error { connections++; return nil },
	}}}
	t.Cleanup(client.CloseIdleConnections)
	exchange := func(signed string) (*oauth2.Token, error) {
		cfg := clientcredentials.Config{
			ClientID: key.ClientID, TokenURL: key.TokenURL, AuthStyle: oauth2.AuthStyleInParams,
			EndpointParams: map[string][]string{"client_assertion_type": {jwtBearer}, "client_assertion": {signed}},
		}
		return cfg.Token(context.WithValue(context.Background(), oauth2.HTTPClient, client))
	}
	identify := func(token string) (int, string) {
		req, err := http.NewRequest(http.MethodGet, "https://"+machineAddr+"/identity", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		var identity struct {
			GRN string `json:"grn"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&identity)
		return resp.StatusCode, identity.GRN
	}

	signed := signAssertion(t, key)
	token, err := exchange(signed)
	require.NoError(t, err)
	assert.Equal(t, "Bearer", token.TokenType)
	status, identity := identify(token.AccessToken)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, key.ClientID, identity, "the token authenticates as the operator the key proves")

	_, err = exchange(signed)
	var refused *oauth2.RetrieveError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "invalid_client", refused.ErrorCode, "a replayed assertion")

	status, body = c.do(http.MethodDelete, keys+"/"+key.Key.Kid, owner, "")
	require.Equal(t, http.StatusNoContent, status, body)
	status, _ = identify(token.AccessToken)
	assert.Equal(t, http.StatusUnauthorized, status, "the withdrawal refuses the token on the next request")
	_, err = exchange(signAssertion(t, key))
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "invalid_client", refused.ErrorCode, "the withdrawn key exchanges nothing")
	assert.Equal(t, 1, connections, "every machine request travelled over one connection")
}
