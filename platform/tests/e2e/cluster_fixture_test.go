package e2e_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// buildClusterFixture builds tests/clusterfixture as the conformance
// suite's run builds it from a garam checkout.
func buildClusterFixture(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "clusterfixture")
	build := exec.Command("go", "build", "-o", bin, "./tests/clusterfixture")
	build.Dir = moduleRoot(t)
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build: %s", out)
	return bin
}

// clusterFixture runs one fixture subcommand under env, and answers its
// exit code and the one JSON object it printed.
func clusterFixture(t *testing.T, bin string, env []string, args ...string) (int, map[string]any) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else {
		require.NoError(t, err)
	}
	var answer map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &answer), "stdout %q, stderr %q", stdout.String(), stderr.String())
	return code, answer
}

// connector is the half of a connector this test plays: its key, and a
// client that verifies the machine listener against the server root.
type connector struct {
	t      *testing.T
	key    *rsa.PrivateKey
	kid    string
	base   string
	client *http.Client
}

func newConnector(t *testing.T, base, rootFile string) *connector {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	require.NoError(t, err)
	thumbprint, err := (&jose.JSONWebKey{Key: &key.PublicKey}).Thumbprint(crypto.SHA256)
	require.NoError(t, err)
	root, err := os.ReadFile(rootFile)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(root))
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}}}
	t.Cleanup(client.CloseIdleConnections)
	return &connector{t: t, key: key, kid: base64.RawURLEncoding.EncodeToString(thumbprint), base: base, client: client}
}

// register spends token with the connector's public key, answering the
// status and the decoded body.
func (c *connector) register(token string) (int, map[string]any) {
	c.t.Helper()
	jwk, err := json.Marshal(jose.JSONWebKey{Key: &c.key.PublicKey, Algorithm: "RS256", Use: "sig"})
	require.NoError(c.t, err)
	body, err := json.Marshal(map[string]any{"registrationToken": token, "jwk": json.RawMessage(jwk),
		"connector": map[string]string{"version": "e2e"}})
	require.NoError(c.t, err)
	return c.send(http.MethodPost, "/cluster-connection/v1alpha1/registrations", "", body)
}

// exchange presents a private_key_jwt assertion signed by the
// connector's key at tokenURL, as AUTH-1 says.
func (c *connector) exchange(clientID, tokenURL string) (string, error) {
	c.t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: c.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", c.kid))
	require.NoError(c.t, err)
	var jti [16]byte
	_, err = rand.Read(jti[:])
	require.NoError(c.t, err)
	now := time.Now()
	claims, err := json.Marshal(map[string]any{"iss": clientID, "sub": clientID, "aud": tokenURL,
		"iat": now.Unix(), "exp": now.Add(time.Minute).Unix(), "jti": base64.RawURLEncoding.EncodeToString(jti[:])})
	require.NoError(c.t, err)
	signed, err := signer.Sign(claims)
	require.NoError(c.t, err)
	assertion, err := signed.CompactSerialize()
	require.NoError(c.t, err)
	cfg := clientcredentials.Config{ClientID: clientID, TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInParams,
		EndpointParams: map[string][]string{"client_assertion_type": {jwtBearer}, "client_assertion": {assertion}}}
	token, err := cfg.Token(context.WithValue(context.Background(), oauth2.HTTPClient, c.client))
	if err != nil {
		return "", err
	}
	return token.AccessToken, nil
}

func (c *connector) send(method, path, token string, body []byte) (int, map[string]any) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, bytes.NewReader(body))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	require.NoError(c.t, err)
	defer func() { _ = resp.Body.Close() }()
	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	return resp.StatusCode, decoded
}

// TestClusterFixture_DrivesTheBuiltServersThroughTheSuitesActions runs
// the conformance fixture against the built `serve api` and `serve
// machine`, as the suite will (#2047). Each subcommand is held by what
// a connector then observes on the cluster-connection routes:
//
//   - mint-token's token registers, and the record it names is the one
//     the registration answers;
//   - --expired's token is refused as REG-4 refuses, and a token
//     reissued for the same record without it is the control;
//   - revoke-keys refuses the token exchanged under the key on the next
//     request, which answered before it;
//   - delete-cluster leaves nothing to mint for.
//
// The machine listener runs with GARAM_CLUSTER_REGISTRATION_LIMIT
// raised, as the suite's run will raise it.
func TestClusterFixture_DrivesTheBuiltServersThroughTheSuitesActions(t *testing.T) {
	garam, bin := buildGaram(t), buildClusterFixture(t)
	p := prepareDatabase(t, buildPrincipalFixture(t))
	startPolicyFeed(t, garam, p.DatabaseURL)
	apiAddr, machineAddr, machineHealthAddr := freeAddr(t), freeAddr(t), freeAddr(t)
	tokenURL := "https://" + machineAddr + "/oauth/token"
	serve(t, garam, "api", append([]string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL, "GARAM_HTTP_ADDR=" + apiAddr, "GARAM_MACHINE_TOKEN_URL=" + tokenURL,
	}, apiPolicyEnv(t)...), "http://"+apiAddr+readyPath)
	certFile, keyFile, kekFile, rootFile := machineKeyMaterialFiles(t)
	serve(t, garam, "machine", append([]string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL,
		"GARAM_MACHINE_ADDR=" + machineAddr, "GARAM_MACHINE_HEALTH_ADDR=" + machineHealthAddr,
		"GARAM_MACHINE_TLS_CERT_FILE=" + certFile, "GARAM_MACHINE_TLS_KEY_FILE=" + keyFile,
		"GARAM_CA_KEY_FILE=" + kekFile, "GARAM_MACHINE_SERVER_ROOT_FILE=" + rootFile,
		"GARAM_MACHINE_ISSUER=https://garam.test", "GARAM_MACHINE_TOKEN_URL=" + tokenURL,
		"GARAM_CLUSTER_REGISTRATION_LIMIT=1000",
	}, policyEnv(t, rootFile)...), "http://"+machineHealthAddr+readyPath)

	slug := consumerClient{t: t, base: "http://" + apiAddr}.createOrg(p.Session.Token, "connected")
	awaitDelivery(t, p.DatabaseURL)
	env := []string{"GARAM_FIXTURE_API_URL=http://" + apiAddr, "GARAM_FIXTURE_SESSION_TOKEN=" + p.Session.Token,
		"GARAM_FIXTURE_ORG=" + slug, "GARAM_FIXTURE_DATABASE_URL=" + p.DatabaseURL}
	c := newConnector(t, "https://"+machineAddr, rootFile)

	code, minted := clusterFixture(t, bin, env, "mint-token")
	require.Equal(t, 0, code, "%v", minted)
	record, _ := minted["cluster"].(string)
	require.True(t, strings.HasPrefix(record, "grn:"+slug+"/iam/cluster/"), record)
	status, registered := c.register(minted["registrationToken"].(string))
	require.Equal(t, http.StatusCreated, status, "%v", registered)
	assert.Equal(t, record, registered["cluster"], "the record mint-token named is the one that registered")
	assert.Equal(t, tokenURL, registered["tokenUrl"])

	code, expired := clusterFixture(t, bin, env, "mint-token", "--cluster", record, "--expired")
	require.Equal(t, 0, code, "%v", expired)
	assert.Equal(t, record, expired["cluster"])
	status, refused := c.register(expired["registrationToken"].(string))
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "registration_refused", refused["error"], "an expired token is refused as REG-4 refuses")
	code, reissued := clusterFixture(t, bin, env, "mint-token", "--cluster", record)
	require.Equal(t, 0, code, "%v", reissued)
	status, _ = c.register(reissued["registrationToken"].(string))
	require.Equal(t, http.StatusCreated, status, "the control: the same record's token without --expired registers")

	token, err := c.exchange(record, tokenURL)
	require.NoError(t, err)
	declarations := "/cluster-connection/v1alpha1/clusters/self/declarations?wait=0"
	status, _ = c.send(http.MethodGet, declarations, token, nil)
	require.NotEqual(t, http.StatusUnauthorized, status, "the control: the token authenticates before revoke-keys")
	code, revoked := clusterFixture(t, bin, env, "revoke-keys", record)
	require.Equal(t, 0, code, "%v", revoked)
	assert.Equal(t, map[string]any{"cluster": record}, revoked)
	status, body := c.send(http.MethodGet, declarations, token, nil)
	assert.Equal(t, http.StatusUnauthorized, status, "revoke-keys withdrew the key the token rests on: %v", body)

	code, deleted := clusterFixture(t, bin, env, "delete-cluster", record)
	require.Equal(t, 0, code, "%v", deleted)
	assert.Equal(t, map[string]any{"cluster": record}, deleted)
	code, gone := clusterFixture(t, bin, env, "mint-token", "--cluster", record)
	assert.Equal(t, 1, code)
	assert.Equal(t, "not_found", gone["error"], "a deleted record has nothing to mint for")
}
