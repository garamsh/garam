package integration_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
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

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/message/transit"
)

// openBao is the dev-mode OpenBao `make db-up` starts beside Postgres
// (deploy/local/docker-compose.yml), pinned by digest. Its address is
// GARAM_TEST_OPENBAO_ADDR and its container
// GARAM_TEST_OPENBAO_CONTAINER, both of which `make integration` sets;
// the suite fails without them rather than skipping. The root token
// and CA the dev server generated are read out of the container here
// and held in memory, so neither is set in the environment and neither
// is printed.
type openBao struct {
	addr   string
	caFile string
	root   string
	client *http.Client
}

func newOpenBao(t *testing.T) *openBao {
	t.Helper()
	addr := os.Getenv("GARAM_TEST_OPENBAO_ADDR")
	container := os.Getenv("GARAM_TEST_OPENBAO_CONTAINER")
	if addr == "" || container == "" {
		t.Fatal("GARAM_TEST_OPENBAO_ADDR and GARAM_TEST_OPENBAO_CONTAINER are not set; run `make db-up integration`")
	}
	ca := inContainer(t, container, "/openbao/file/vault-ca.pem")
	caFile := filepath.Join(t.TempDir(), "openbao-ca.pem")
	require.NoError(t, os.WriteFile(caFile, ca, 0o600))
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(ca), "the dev server's CA is a certificate")
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
	}}
	t.Cleanup(client.CloseIdleConnections)
	return &openBao{
		addr:   addr,
		caFile: caFile,
		root:   strings.TrimSpace(string(inContainer(t, container, "/home/openbao/.vault-token"))),
		client: client,
	}
}

// inContainer reads one file out of the container, through the daemon
// DOCKER_HOST names. Its contents never reach a failure message.
func inContainer(t *testing.T, container, path string) []byte {
	t.Helper()
	var stderr bytes.Buffer
	cmd := exec.Command("docker", "exec", container, "cat", path)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, "docker exec %s cat %s: %s", container, path, stderr.String())
	return out
}

// call sends one request under the root token and answers its data.
func (b *openBao) call(t *testing.T, token, method, path string, body any) map[string]any {
	t.Helper()
	var payload io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, b.addr+"/v1/"+path, payload)
	require.NoError(t, err)
	req.Header.Set("X-Vault-Token", token)
	resp, err := b.client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Less(t, resp.StatusCode, 300, "%s %s answered %d: %s", method, path, resp.StatusCode, raw)
	var answer struct {
		Data map[string]any `json:"data"`
		Auth map[string]any `json:"auth"`
	}
	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &answer))
	}
	if answer.Auth != nil {
		return answer.Auth
	}
	return answer.Data
}

// signingKey is a Transit mount of the test's own holding one
// ecdsa-p256 key, and the token garam is given for it: one carrying
// only the three paths ADR-0104 names a deployment owes garam.
type signingKey struct {
	mount     string
	tokenFile string
	bao       *openBao
}

func newSigningKey(t *testing.T, b *openBao) signingKey {
	t.Helper()
	suffix := make([]byte, 6)
	_, err := rand.Read(suffix)
	require.NoError(t, err)
	mount := "garam-" + hex.EncodeToString(suffix)
	b.call(t, b.root, http.MethodPost, "sys/mounts/"+mount, map[string]string{"type": "transit"})
	t.Cleanup(func() {
		b.call(t, b.root, http.MethodDelete, "sys/mounts/"+mount, nil)
		b.call(t, b.root, http.MethodDelete, "sys/policies/acl/"+mount, nil)
	})
	b.call(t, b.root, http.MethodPost, mount+"/keys/messages", map[string]string{"type": "ecdsa-p256"})
	policy := `path "` + mount + `/keys/messages" { capabilities = ["read"] }
path "` + mount + `/keys/messages/rotate" { capabilities = ["update"] }
path "` + mount + `/sign/messages" { capabilities = ["update"] }`
	b.call(t, b.root, http.MethodPut, "sys/policies/acl/"+mount, map[string]string{"policy": policy})
	auth := b.call(t, b.root, http.MethodPost, "auth/token/create", map[string]any{
		"policies": []string{mount}, "no_default_policy": true, "ttl": "15m",
	})
	token, ok := auth["client_token"].(string)
	require.True(t, ok, "the token was created")
	t.Cleanup(func() { b.call(t, b.root, http.MethodPost, "auth/token/revoke", map[string]string{"token": token}) })
	tokenFile := filepath.Join(t.TempDir(), "openbao-token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(token), 0o600))
	return signingKey{mount: mount, tokenFile: tokenFile, bao: b}
}

// custodian answers the Transit custodian garam runs for this key at
// address.
func (k signingKey) custodian(t *testing.T, address string) message.Custodian {
	t.Helper()
	c, err := transit.New(transit.Settings{
		Address: address, CAFile: k.bao.caFile, TokenFile: k.tokenFile, Mount: k.mount, Key: "messages",
	})
	require.NoError(t, err)
	return c
}

// thumbprint is the RFC 7638 thumbprint go-jose computes for a version
// of the Transit key, read back from OpenBao under the root token.
func (k signingKey) thumbprint(t *testing.T, version string) string {
	t.Helper()
	data := k.bao.call(t, k.bao.root, http.MethodGet, k.mount+"/keys/messages", nil)
	keys, ok := data["keys"].(map[string]any)
	require.True(t, ok)
	v, ok := keys[version].(map[string]any)
	require.True(t, ok, "Transit holds version %s", version)
	block, _ := pem.Decode([]byte(v["public_key"].(string)))
	require.NotNil(t, block)
	public, err := x509.ParsePKIXPublicKey(block.Bytes)
	require.NoError(t, err)
	tp, err := (&jose.JSONWebKey{Key: public}).Thumbprint(crypto.SHA256)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(tp)
}

// kidOf reads the key a signature names.
func kidOf(t *testing.T, signature string) string {
	t.Helper()
	signed, err := jose.ParseSigned(signature, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	return signed.Signatures[0].Header.KeyID
}

// listenerHolding serves the stack's database on a second listener
// whose message signing keys are created in signing.
func (s signingStack) listenerHolding(t *testing.T, signing message.Custodian) string {
	t.Helper()
	return machineListenerHolding(t, s.pool, s.sealer, s.engine, signing)
}

// TestTransitSigning_AHandOutVerifiesUnderThePublishedSetAndNoPrivateKeyIsStored
// holds ADR-0104's point: with the provider configured, a hand-out is
// signed by Transit, a stock JOSE library verifies it under the set
// garam publishes, its kid is the RFC 7638 thumbprint of the Transit
// version that signed it, and the row carries no private material.
func TestTransitSigning_AHandOutVerifiesUnderThePublishedSetAndNoPrivateKeyIsStored(t *testing.T) {
	key := newSigningKey(t, newOpenBao(t))
	s := newSigningStackHolding(t, key.custodian(t, key.bao.addr))
	posted := s.post(t, "hello")
	claimed := s.claim(t)
	set := keySet(t, s.url)

	claims, err := verify(set, string(testIssuer), s.recipientGRN.String(), claimed.Message.Id, *claimed.Content, *claimed.Signature)
	require.NoError(t, err)
	assert.Equal(t, posted.Id, claims.MessageID)

	var sealed, mount, version any
	require.NoError(t, s.pool.QueryRow(context.Background(),
		`SELECT sealed_private_key, transit_mount, transit_version FROM message_signing_keys`).Scan(&sealed, &mount, &version))
	assert.Nil(t, sealed, "no private material is stored")
	assert.Equal(t, key.mount, mount)
	// Version 1 is the one the deployment's create made; garam signs
	// with the version its own rotate made, and never with one it holds
	// no row for.
	assert.EqualValues(t, 2, version)
	assert.Equal(t, key.thumbprint(t, "2"), kidOf(t, *claimed.Signature), "the kid is the thumbprint of the version that signed")
}

// TestTransitSigning_MovingOntoTransitKeepsTheSealedKeySigningForTheLead
// holds the move onto the provider (ADR-0104): the sealed key goes on
// signing, the Transit key is published two hours before it signs, and
// once that lead has passed a hand-out is signed by Transit while the
// sealed key stays published for what it signed.
func TestTransitSigning_MovingOntoTransitKeepsTheSealedKeySigningForTheLead(t *testing.T) {
	ctx := context.Background()
	key := newSigningKey(t, newOpenBao(t))
	s := newSigningStack(t)
	s.post(t, "before")
	sealedKid := kidOf(t, *s.claim(t).Signature)

	moved := s.listenerHolding(t, key.custodian(t, key.bao.addr))
	set := keySet(t, moved)
	require.Len(t, set.Keys, 2, "the Transit key is announced beside the sealed one")
	var lead float64
	require.NoError(t, s.pool.QueryRow(ctx, `SELECT extract(epoch FROM signs_from - created_at)::float8
		FROM message_signing_keys WHERE transit_version IS NOT NULL`).Scan(&lead))
	assert.InDelta(t, message.SigningKeyLead.Seconds(), lead, 5, "announced with the full lead")

	status, answer := machineSend(t, s.sender, http.MethodPost, moved+"/messages",
		`{"recipient":"`+s.recipientGRN.String()+`","idempotencyKey":"key-2","body":"during"}`)
	require.Equal(t, http.StatusCreated, status, string(answer))
	during := claimOn(t, s, moved)
	assert.Equal(t, sealedKid, kidOf(t, *during.Signature), "the sealed key signs until the Transit key begins")

	// The lead passing, as the database's clock would see it: the
	// Transit key begins now, after the sealed key began.
	_, err := s.pool.Exec(ctx, `UPDATE message_signing_keys SET signs_from = clock_timestamp()
		WHERE transit_version IS NOT NULL`)
	require.NoError(t, err)
	status, answer = machineSend(t, s.sender, http.MethodPost, moved+"/messages",
		`{"recipient":"`+s.recipientGRN.String()+`","idempotencyKey":"key-3","body":"after"}`)
	require.Equal(t, http.StatusCreated, status, string(answer))
	after := claimOn(t, s, moved)
	assert.Equal(t, key.thumbprint(t, "2"), kidOf(t, *after.Signature), "Transit signs once its key has begun")
	set = keySet(t, moved)
	_, err = verify(set, string(testIssuer), s.recipientGRN.String(), after.Message.Id, *after.Content, *after.Signature)
	require.NoError(t, err)
	assert.Len(t, set.Keys, 2, "the sealed key stays published while what it signed can be valid")
}

// TestTransitSigning_RotationMakesTheNextVersionAndRetirementWithdrawsIt
// holds rotation and retirement under Transit (ADR-0104): a key due for
// replacement is announced as the next version Transit's rotate makes,
// and retiring the signing key at once leaves the set and is replaced
// by a version that signs at once, while Transit keeps every version.
func TestTransitSigning_RotationMakesTheNextVersionAndRetirementWithdrawsIt(t *testing.T) {
	ctx := context.Background()
	key := newSigningKey(t, newOpenBao(t))
	s := newSigningStackHolding(t, key.custodian(t, key.bao.addr))
	s.post(t, "one")
	first := kidOf(t, *s.claim(t).Signature)
	require.Equal(t, key.thumbprint(t, "2"), first)

	// The current key having signed for its lifetime less the lead.
	_, err := s.pool.Exec(ctx, `UPDATE message_signing_keys SET signs_from = signs_from - make_interval(secs => $1)`,
		(message.SigningKeyLifetime - message.SigningKeyLead).Seconds())
	require.NoError(t, err)
	set := keySet(t, s.url)
	require.Len(t, set.Keys, 2, "the next key is announced")
	assert.Contains(t, kidsOf(set), key.thumbprint(t, "3"), "as the version rotate made")

	require.NoError(t, message.NewPostgresStore(s.pool).WithdrawSigningKey(ctx, message.KeyID(first)))
	status, answer := machineSend(t, s.sender, http.MethodPost, s.url+"/messages",
		`{"recipient":"`+s.recipientGRN.String()+`","idempotencyKey":"key-2","body":"two"}`)
	require.Equal(t, http.StatusCreated, status, string(answer))
	replaced := claimOn(t, s, s.url)
	assert.Equal(t, key.thumbprint(t, "4"), kidOf(t, *replaced.Signature), "the replacement is a new version that signs at once")
	set = keySet(t, s.url)
	assert.NotContains(t, kidsOf(set), first, "the retired key left the set")
	_, err = verify(set, string(testIssuer), s.recipientGRN.String(), replaced.Message.Id, *replaced.Content, *replaced.Signature)
	require.NoError(t, err)
	assert.NotEmpty(t, key.thumbprint(t, "2"), "Transit keeps the retired version; garam no longer signs with it")
}

// TestTransitSigning_AClaimOpenBaoCannotSignSpendsNoAttempt holds the
// unavailable case (ADR-0104): a listener whose Transit cannot be
// reached refuses the claim as a server error with the message's
// attempt unspent, and still serves the published set. The same claim
// on a listener that reaches Transit is the control: it hands the
// message out as its first attempt.
func TestTransitSigning_AClaimOpenBaoCannotSignSpendsNoAttempt(t *testing.T) {
	ctx := context.Background()
	key := newSigningKey(t, newOpenBao(t))
	s := newSigningStackHolding(t, key.custodian(t, key.bao.addr))
	require.Len(t, keySet(t, s.url).Keys, 1, "the setup created the Transit key")
	posted := s.post(t, "hello")

	closed, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	nowhere := "https://" + closed.Addr().String()
	require.NoError(t, closed.Close())
	unreachable := s.listenerHolding(t, key.custodian(t, nowhere))

	status, answer := machineSend(t, s.recipient, http.MethodPost, unreachable+"/agents/"+s.recipientGRN.String()+"/claims", "")
	assert.GreaterOrEqual(t, status, http.StatusInternalServerError, string(answer))
	var attempts int
	require.NoError(t, s.pool.QueryRow(ctx, `SELECT attempts FROM messages WHERE id = $1`, posted.Id).Scan(&attempts))
	assert.Equal(t, 0, attempts, "the refused claim spent no attempt")
	assert.Len(t, keySet(t, unreachable).Keys, 1, "the set is still served")

	claimed := claimOn(t, s, s.url)
	assert.Equal(t, 1, claimed.Attempt, "the control: the claim Transit signs is the message's first attempt")
}

// claimOn hands the recipient its next message from the listener at
// url.
func claimOn(t *testing.T, s signingStack, url string) machinegen.Claim {
	t.Helper()
	status, answer := machineSend(t, s.recipient, http.MethodPost, url+"/agents/"+s.recipientGRN.String()+"/claims", "")
	require.Equal(t, http.StatusOK, status, string(answer))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(answer, &claimed))
	require.NotNil(t, claimed.Signature, "the claim carried no signature")
	return claimed
}

func kidsOf(set jose.JSONWebKeySet) []string {
	kids := make([]string, 0, len(set.Keys))
	for _, k := range set.Keys {
		kids = append(kids, k.KeyID)
	}
	return kids
}
