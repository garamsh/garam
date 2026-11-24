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
	"fmt"
	"net/http"
	"net/url"
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
	bin, env, c, tokenURL, slug := startFixtureStack(t)

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
	require.Equal(t, http.StatusOK, status, "the control: the token authenticates before revoke-keys")
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

// startFixtureStack serves the built `serve api` and `serve machine` on
// one database, creates the organization the fixture acts in, and
// answers the fixture, its environment, a connector, the token URL and
// the organization's slug. The machine listener runs with
// GARAM_CLUSTER_REGISTRATION_LIMIT raised, as the suite's run will
// raise it.
func startFixtureStack(t *testing.T) (string, []string, *connector, string, string) {
	t.Helper()
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
	return bin, env, newConnector(t, "https://"+machineAddr, rootFile), tokenURL, slug
}

// poll is one watch of the declarations after cursor, held up to wait
// seconds, answered with its status, page and how long it was held.
func (c *connector) poll(token, cursor string, wait int) (int, map[string]any, time.Duration) {
	c.t.Helper()
	path := "/cluster-connection/v1alpha1/clusters/self/declarations?wait=" + fmt.Sprint(wait)
	if cursor != "" {
		path += "&after=" + url.QueryEscape(cursor)
	}
	start := time.Now()
	status, page := c.send(http.MethodGet, path, token, nil)
	return status, page, time.Since(start)
}

// changesOf names each change of a page as "op id generation".
func changesOf(page map[string]any) []string {
	var out []string
	changes, _ := page["changes"].([]any)
	for _, raw := range changes {
		ch, _ := raw.(map[string]any)
		out = append(out, fmt.Sprintf("%v %v %v", ch["op"], ch["id"], ch["generation"]))
	}
	return out
}

// TestClusterFixture_DeclarationsReachTheConnectorsLongPoll holds
// declare, tombstone and expire-cursors (#2048) by what a connector's
// long poll on `serve machine` observes of what the fixture wrote
// through `serve api`:
//
//   - a poll held on the record's cursor answers the declaration
//     declare placed, before its wait is out;
//   - tombstone's delete follows it at the next generation;
//   - expire-cursors leaves every cursor answered so far 410, and the
//     full set read next answers a cursor that is live.
func TestClusterFixture_DeclarationsReachTheConnectorsLongPoll(t *testing.T) {
	bin, env, c, tokenURL, _ := startFixtureStack(t)
	code, minted := clusterFixture(t, bin, env, "mint-token")
	require.Equal(t, 0, code, "%v", minted)
	record := minted["cluster"].(string)
	status, registered := c.register(minted["registrationToken"].(string))
	require.Equal(t, http.StatusCreated, status, "%v", registered)
	token, err := c.exchange(record, tokenURL)
	require.NoError(t, err)

	status, full, _ := c.poll(token, "", 0)
	require.Equal(t, http.StatusOK, status, "%v", full)
	assert.Equal(t, true, full["full"])
	assert.Empty(t, changesOf(full), "a record with nothing declared answers an empty full set")
	cursor := full["cursor"].(string)

	file := filepath.Join(t.TempDir(), "declare.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"cluster":"`+record+`","id":"e2e-bot","object":
		{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot","metadata":{"namespace":"team-a","name":"reviewer"},
		 "spec":{"persona":{"role":"Reviewer"},"agent":{"harness":"claude-code"},"permissions":{"secrets":["bot-token/value"]}}}}`), 0o600))
	held := make(chan struct{})
	type answer struct {
		status int
		page   map[string]any
		took   time.Duration
	}
	polled := make(chan answer, 1)
	go func() {
		close(held)
		status, page, took := c.poll(token, cursor, 50)
		polled <- answer{status, page, took}
	}()
	<-held
	code, placed := clusterFixture(t, bin, env, "declare", file)
	require.Equal(t, 0, code, "%v", placed)
	assert.Equal(t, map[string]any{"id": "e2e-bot", "generation": float64(1)}, placed)
	got := <-polled
	require.Equal(t, http.StatusOK, got.status, "%v", got.page)
	assert.Equal(t, []string{"apply e2e-bot 1"}, changesOf(got.page))
	assert.Less(t, got.took, 45*time.Second, "the write ended the hold, not the wait")
	cursor = got.page["cursor"].(string)

	code, tombstoned := clusterFixture(t, bin, env, "tombstone", "e2e-bot")
	require.Equal(t, 0, code, "%v", tombstoned)
	assert.Equal(t, map[string]any{"id": "e2e-bot", "generation": float64(2)}, tombstoned)
	status, after, _ := c.poll(token, cursor, 0)
	require.Equal(t, http.StatusOK, status, "%v", after)
	assert.Equal(t, []string{"delete e2e-bot 2"}, changesOf(after))
	answered := after["cursor"].(string)

	code, expiredCursors := clusterFixture(t, bin, env, "expire-cursors", record)
	require.Equal(t, 0, code, "%v", expiredCursors)
	assert.Equal(t, map[string]any{"cluster": record}, expiredCursors)
	for _, old := range []string{cursor, answered} {
		status, refusal, _ := c.poll(token, old, 0)
		assert.Equal(t, http.StatusGone, status, old)
		assert.Equal(t, "cursor_expired", refusal["error"], old)
	}
	status, refetched, _ := c.poll(token, "", 0)
	require.Equal(t, http.StatusOK, status, "%v", refetched)
	assert.Empty(t, changesOf(refetched), "the tombstoned declaration is not in the full set")
	status, live, _ := c.poll(token, refetched["cursor"].(string), 0)
	assert.Equal(t, http.StatusOK, status, "the control: the cursor the full set answered is live: %v", live)

	bad := filepath.Join(t.TempDir(), "image.json")
	require.NoError(t, os.WriteFile(bad, []byte(`{"cluster":"`+record+`","object":
		{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot","metadata":{"namespace":"team-a","name":"x"},
		 "spec":{"persona":{"role":"Reviewer"},"agent":{"harness":"claude-code"},"image":"evil:latest"}}}`), 0o600))
	code, refused := clusterFixture(t, bin, env, "declare", bad)
	assert.Equal(t, 1, code)
	assert.Equal(t, "invalid_request", refused["error"], "DECL-9 is refused at garam, before any connector reads it")
	assert.Contains(t, refused["message"], "spec.image", "the refusal names the field")
}

// TestClusterFixture_ReportsReachLatestReport holds the report route
// (#2050) through the built servers, by what latest-report reads back
// through `serve api` of what a connector sent `serve machine`:
//
//   - a report is answered 204 and kept, without the outcome and the
//     object naming another record's declaration (REP-8), and the
//     request's time is the record's lastContactAt (REP-5);
//   - an older report is answered 204 and changes nothing;
//   - a Secret is 400 invalid_request (REP-7), and a body over 4 MiB is
//     413 report_too_large (REP-6), past the listener's own bound;
//   - a declaration of a name the report shows cluster-owned is
//     refused (gbo ADR 0089 decision 4).
func TestClusterFixture_ReportsReachLatestReport(t *testing.T) {
	bin, env, c, tokenURL, _ := startFixtureStack(t)
	code, minted := clusterFixture(t, bin, env, "mint-token")
	require.Equal(t, 0, code, "%v", minted)
	record := minted["cluster"].(string)
	status, registered := c.register(minted["registrationToken"].(string))
	require.Equal(t, http.StatusCreated, status, "%v", registered)
	token, err := c.exchange(record, tokenURL)
	require.NoError(t, err)
	code, other := clusterFixture(t, bin, env, "mint-token")
	require.Equal(t, 0, code, "%v", other)

	code, before := clusterFixture(t, bin, env, "latest-report", record)
	require.Equal(t, 0, code, "%v", before)
	assert.Equal(t, map[string]any{"cluster": record, "lastContactAt": nil, "report": nil}, before)

	bot := `{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot","metadata":{"namespace":"team-a","name":"%s"},
		"spec":{"persona":{"role":"Reviewer"},"agent":{"harness":"claude-code"}}}`
	for _, d := range []struct{ on, id, name string }{{record, "e2e-ours", "reviewer"}, {other["cluster"].(string), "e2e-theirs", "theirs"}} {
		file := filepath.Join(t.TempDir(), "declare.json")
		require.NoError(t, os.WriteFile(file, []byte(`{"cluster":"`+d.on+`","id":"`+d.id+`","object":`+
			fmt.Sprintf(bot, d.name)+`}`), 0o600))
		code, placed := clusterFixture(t, bin, env, "declare", file)
		require.Equal(t, 0, code, "%v", placed)
	}

	reportPath := "/cluster-connection/v1alpha1/clusters/self/reports"
	object := func(owner, name, extra string) string {
		return `{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot","namespace":"team-a","name":"` + name +
			`","uid":"u-` + name + `","owner":"` + owner + `","generation":1,"status":{"phase":"Ready"}` + extra + `}`
	}
	report := func(observedAt, objects string) []byte {
		return []byte(`{"observedAt":"` + observedAt + `","connector":{"version":"0.1.0","mode":"apply","namespaces":["team-a"]},
			"declarations":[{"id":"e2e-ours","generation":1,"outcome":"applied"},
				{"id":"e2e-theirs","generation":1,"outcome":"applied"}],
			"objects":[` + objects + `]}`)
	}
	sent := report("2026-11-24T12:00:00Z", strings.Join([]string{
		object("garam", "reviewer", `,"cluster":"`+record+`","declarationId":"e2e-ours","orphaned":false`),
		object("garam", "theirs", `,"cluster":"`+record+`","declarationId":"e2e-theirs","orphaned":false`),
		object("cluster", "mira", `,"spec":{"persona":{"role":"Lead"}}`),
	}, ","))
	status, answered := c.send(http.MethodPost, reportPath, token, sent)
	require.Equal(t, http.StatusNoContent, status, "%v", answered)

	code, kept := clusterFixture(t, bin, env, "latest-report", record)
	require.Equal(t, 0, code, "%v", kept)
	assert.NotNil(t, kept["lastContactAt"], "the report's request is the record's contact")
	keptReport, _ := kept["report"].(map[string]any)
	require.NotNil(t, keptReport, "%v", kept)
	var outcomes, objects []string
	for _, o := range keptReport["declarations"].([]any) {
		outcomes = append(outcomes, o.(map[string]any)["id"].(string))
	}
	for _, o := range keptReport["objects"].([]any) {
		objects = append(objects, o.(map[string]any)["name"].(string))
	}
	assert.Equal(t, []string{"e2e-ours"}, outcomes, "another record's declaration is not recorded")
	assert.Equal(t, []string{"reviewer", "mira"}, objects, "nor an object naming it")

	status, answered = c.send(http.MethodPost, reportPath, token, report("2026-11-24T11:59:59Z", object("cluster", "older", "")))
	require.Equal(t, http.StatusNoContent, status, "%v", answered)
	code, again := clusterFixture(t, bin, env, "latest-report", record)
	require.Equal(t, 0, code, "%v", again)
	assert.Equal(t, kept["report"], again["report"], "an older report changes nothing")

	secret := strings.Replace(object("cluster", "token", ""), `"kind":"Bot"`, `"kind":"Secret"`, 1)
	status, refused := c.send(http.MethodPost, reportPath, token, report("2026-11-24T12:00:01Z", secret))
	assert.Equal(t, http.StatusBadRequest, status, "%v", refused)
	assert.Equal(t, "invalid_request", refused["error"])
	big := object("cluster", "big", `,"spec":{"note":"`+strings.Repeat("x", 4<<20)+`"}`)
	status, refused = c.send(http.MethodPost, reportPath, token, report("2026-11-24T12:00:01Z", big))
	assert.Equal(t, http.StatusRequestEntityTooLarge, status, "%v", refused)
	assert.Equal(t, "report_too_large", refused["error"])
	twoMiB := object("cluster", "large", `,"spec":{"note":"`+strings.Repeat("x", 2<<20)+`"}`)
	status, answered = c.send(http.MethodPost, reportPath, token, report("2026-11-24T12:00:02Z", twoMiB+","+
		object("cluster", "mira", "")))
	assert.Equal(t, http.StatusNoContent, status, "the control: a report past the listener's 1 MiB is read: %v", answered)

	file := filepath.Join(t.TempDir(), "held.json")
	require.NoError(t, os.WriteFile(file, []byte(`{"cluster":"`+record+`","object":`+fmt.Sprintf(bot, "mira")+`}`), 0o600))
	code, held := clusterFixture(t, bin, env, "declare", file)
	assert.Equal(t, 1, code, "%v", held)
	assert.Contains(t, held["message"], "cluster-owned", "the latest report shows mira held by the cluster")
}
