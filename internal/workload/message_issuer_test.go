package workload_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/workload"
)

// runtimeConfigOf is the runtime's configuration as the runtime reads
// it: its revision and its issuers list.
func runtimeConfigOf(t *testing.T, c *stubContainer) (string, []map[string]string) {
	t.Helper()
	var config struct {
		Revision string              `yaml:"revision"`
		Issuers  []map[string]string `yaml:"issuers"`
	}
	require.NoError(t, yaml.Unmarshal(c.files["/etc/sherlock/config.yaml"].Data, &config))
	return config.Revision, config.Issuers
}

// TestRun_RendersGaramsIssuerIntoTheRuntime holds ADR-0101's step 4 on a
// standalone host: one issuers entry naming garam's issuer, the key
// set at the adapter's address for garam, and the garam server root
// that address is verified by, copied into the runtime for its owner
// alone.
func TestRun_RendersGaramsIssuerIntoTheRuntime(t *testing.T) {
	h := newHost(t)
	h.start()
	runtime, _, _ := h.serving()

	revision, issuers := runtimeConfigOf(t, runtime)
	assert.Equal(t, "r1", revision, "the host's own settings stay as they were")
	assert.Equal(t, []map[string]string{{
		"issuer":       testIssuer,
		"keys-url":     "https://garam.example/message-signing-keys",
		"keys-ca-file": "/etc/sherlock/garam-server-root.pem",
	}}, issuers)
	root := runtime.files["/etc/sherlock/garam-server-root.pem"]
	assert.Equal(t, h.root.pem, root.Data)
	assert.Equal(t, 0o600, int(root.Mode))
	assert.Equal(t, 65532, root.UID)
}

// TestRun_RendersNothingWithoutAnIssuer holds the host that names no
// issuer on today's footing: the runtime is handed its configuration
// byte for byte and no garam server root, so it trusts no garam
// signature until the host names garam's issuer. The rendering test
// above is the control.
func TestRun_RendersNothingWithoutAnIssuer(t *testing.T) {
	h := newHost(t)
	h.issuer = ""
	h.start()
	runtime, _, _ := h.serving()

	assert.Equal(t, "revision: r1\n", string(runtime.files["/etc/sherlock/config.yaml"].Data))
	assert.NotContains(t, runtime.files, "/etc/sherlock/garam-server-root.pem")
	_, _, adapter := h.serving()
	assert.NotContains(t, adapter.spec.Env, "GARAM_ADAPTER_SIGN_MESSAGES=true", "the adapter signs nothing")
}

// TestRun_SignsWhereTheRuntimeTrustsGaramsIssuer holds the one setting
// that turns signing on end to end (#1312): naming garam's issuer
// renders the runtime's entry and turns the adapter's signing on with
// it, so neither is on without the other. TestRun_RendersNothingWithout
// AnIssuer is the control, where both stay off.
func TestRun_SignsWhereTheRuntimeTrustsGaramsIssuer(t *testing.T) {
	h := newHost(t)
	h.start()
	runtime, _, adapter := h.serving()

	_, issuers := runtimeConfigOf(t, runtime)
	require.Len(t, issuers, 1, "the setup rendered the runtime's entry")
	assert.Contains(t, adapter.spec.Env, "GARAM_ADAPTER_SIGN_MESSAGES=true")
}

// TestRun_TakesTheKeySetFromTheAdaptersAddressAndTheIssuerFromGaram
// holds the two values apart: the issuer is garam's identity and stays
// what garam names, while the set is fetched where the runtime reaches
// garam, which is the adapter's address (ADR-0087).
func TestRun_TakesTheKeySetFromTheAdaptersAddressAndTheIssuerFromGaram(t *testing.T) {
	h := newHost(t)
	h.machineURL = "https://10.0.2.2:8443"
	h.start()
	runtime, _, _ := h.serving()

	_, issuers := runtimeConfigOf(t, runtime)
	require.Len(t, issuers, 1)
	assert.Equal(t, testIssuer, issuers[0]["issuer"])
	assert.Equal(t, "https://10.0.2.2:8443/message-signing-keys", issuers[0]["keys-url"])
}

// TestRun_KeepsTheHostsOwnIssuers holds garam as one issuer among any the
// runtime trusts: an issuer the host's configuration names stays, and
// garam's entry is added beside it.
func TestRun_KeepsTheHostsOwnIssuers(t *testing.T) {
	h := newHost(t)
	h.start(func(d *workload.Desired) {
		d.Config = []byte("revision: r1\nissuers:\n  - issuer: https://other.example\n    keys-url: https://other.example/keys\n")
	})
	runtime, _, _ := h.serving()

	_, issuers := runtimeConfigOf(t, runtime)
	require.Len(t, issuers, 2)
	assert.Equal(t, map[string]string{"issuer": "https://other.example", "keys-url": "https://other.example/keys"}, issuers[0])
	assert.Equal(t, testIssuer, issuers[1]["issuer"])
}

// TestNewSupervisor_RefusesAConfigurationNamingGaramsIssuer holds the
// one entry the host may not write itself: the runtime refuses two
// entries for one issuer at startup, and garam's is the supervisor's to
// render. An entry for another issuer is the control, and is taken.
func TestNewSupervisor_RefusesAConfigurationNamingGaramsIssuer(t *testing.T) {
	h := newHost(t)
	naming := func(issuer string) func(*workload.Desired) {
		return func(d *workload.Desired) {
			d.Config = []byte("revision: r1\nissuers:\n  - issuer: " + issuer + "\n    keys-url: https://elsewhere.example/keys\n")
		}
	}
	_, err := workload.NewSupervisor(h.engine, h.garam, h.config(naming(testIssuer)))
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)
	_, err = workload.NewSupervisor(h.engine, h.garam, h.config(naming("https://other.example")))
	assert.NoError(t, err)
}

// garamKeyring is garam's own message keyring over keys held in memory,
// with a sealer that keeps the private half it is handed: a garam Claim
// is signed only inside internal/message, so this is how the test signs
// under the key garam generated.
type garamKeyring struct {
	mu   sync.Mutex
	keys []*message.SigningKey
	// sealed guards private: rotation seals while holding mu.
	sealed  sync.Mutex
	private []byte
}

func (g *garamKeyring) SigningKeys(context.Context) ([]*message.SigningKey, time.Time, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]*message.SigningKey(nil), g.keys...), time.Now(), nil
}

func (g *garamKeyring) RotateSigningKeys(_ context.Context, prepare func([]*message.SigningKey, time.Time) (*message.NewSigningKey, error)) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	key, err := prepare(append([]*message.SigningKey(nil), g.keys...), now)
	if err != nil || key == nil {
		return err
	}
	g.keys = append(g.keys, &message.SigningKey{ID: key.ID, PublicKey: key.PublicKey, Held: key.Held, SignsFrom: now.Add(key.Lead)})
	return nil
}

func (g *garamKeyring) WithdrawSigningKey(context.Context, message.KeyID) error { return nil }

func (g *garamKeyring) Seal(_ context.Context, _, plaintext []byte) ([]byte, error) {
	g.sealed.Lock()
	defer g.sealed.Unlock()
	g.private = append([]byte(nil), plaintext...)
	return plaintext, nil
}

func (g *garamKeyring) Open(_ context.Context, _, sealed []byte) ([]byte, error) { return sealed, nil }

// stubGaramKeys serves keyring's published set as garam's route does,
// over TLS under the garam server root the host holds, and answers its
// origin.
func stubGaramKeys(t *testing.T, h *host, keyring message.Keyring) string {
	t.Helper()
	published, err := keyring.PublishedKeys(context.Background())
	require.NoError(t, err)
	var set jose.JSONWebKeySet
	for _, k := range published {
		public, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append([]byte{4}, append(k.X, k.Y...)...))
		require.NoError(t, err)
		set.Keys = append(set.Keys, jose.JSONWebKey{Key: public, KeyID: string(k.ID), Algorithm: "ES256", Use: "sig"})
	}
	body, err := json.Marshal(set)
	require.NoError(t, err)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(7), IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, h.root.cert, &key.PublicKey, h.root.key)
	require.NoError(t, err)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/message-signing-keys" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/jwk-set+json")
		_, _ = w.Write(body)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.URL
}

// verifyAsTheRuntime is what the runtime does with garam's entry
// (Sherlock's gateway.md at v0.3.0, plus keys-ca-file from
// garamsh/sherlock#1008): it fetches the set at keys-url trusting the
// keys-ca-file bundle and nothing else, checks iss against the entry's
// issuer, and verifies the signature under the key the header names.
func verifyAsTheRuntime(entry map[string]string, caPEM []byte, signature string) error {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return errorx.New(errorx.KindInvalidArgument, "the keys-ca-file holds no certificate")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}}}
	resp, err := client.Get(entry["keys-url"])
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var set jose.JSONWebKeySet
	if err := json.Unmarshal(raw, &set); err != nil {
		return err
	}
	signed, err := jose.ParseSigned(signature, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return err
	}
	keys := set.Key(signed.Signatures[0].Protected.KeyID)
	if len(keys) != 1 {
		return errorx.New(errorx.KindPermissionDenied, "signature rejected: key")
	}
	payload, err := signed.Verify(keys[0])
	if err != nil {
		return err
	}
	var claims struct {
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return err
	}
	if claims.Iss != entry["issuer"] {
		return errorx.New(errorx.KindPermissionDenied, "signature rejected: issuer")
	}
	return nil
}

// garamSignature is a signature over garam's content for a message to
// the test agent, under key and named kid, with ADR-0101's claims.
func garamSignature(t *testing.T, key *ecdsa.PrivateKey, kid string) string {
	t.Helper()
	sender, err := grn.Parse("grn:acme:default:agent:9f8e7d6c5b4a3021")
	require.NoError(t, err)
	m, err := message.New(testAgent, sender, "key-1", "hello")
	require.NoError(t, err)
	content, err := message.Content(m)
	require.NoError(t, err)
	digest := sha256.Sum256([]byte(content))
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"iss": testIssuer, "sub": sender.String(), "aud": testAgent.String(), "org": testAgent.Organization,
		"message_id": string(m.ID), "content_sha256": base64.RawURLEncoding.EncodeToString(digest[:]),
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
	})
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: jose.JSONWebKey{Key: key, KeyID: kid}},
		(&jose.SignerOptions{}).WithType("agent-message+jwt"))
	require.NoError(t, err)
	object, err := signer.Sign(payload)
	require.NoError(t, err)
	jws, err := object.CompactSerialize()
	require.NoError(t, err)
	return jws
}

// TestRun_ARuntimeVerifiesGaramsSignatureByTheRenderedEntry holds what
// the entry is for, against a stub garam serving garam's own published
// set over the private garam server root: what the runtime is handed
// fetches that set and verifies a message signed under garam's key.
// Two controls sit beside it. Fetched trusting the system roots instead
// of the rendered bundle, the set is unreachable, so the bundle is what
// reaches it; and a signature under a key garam never published is
// refused, so the set is what decides. The Sherlock release with
// keys-ca-file is untagged, so the runtime's own verification is stood
// in for here (#1269).
func TestRun_ARuntimeVerifiesGaramsSignatureByTheRenderedEntry(t *testing.T) {
	h := newHost(t)
	keyring := &garamKeyring{}
	keys, err := message.NewKeyring(keyring, testIssuer, message.Config{}, message.NewFileCustodian(keyring))
	require.NoError(t, err)
	h.machineURL = stubGaramKeys(t, h, keys)
	h.start()
	runtime, _, _ := h.serving()
	_, issuers := runtimeConfigOf(t, runtime)
	require.Len(t, issuers, 1)
	entry, caPEM := issuers[0], runtime.files["/etc/sherlock/garam-server-root.pem"].Data

	require.Len(t, keyring.keys, 1)
	garamKey, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), keyring.private)
	require.NoError(t, err)
	kid := string(keyring.keys[0].ID)
	require.NoError(t, verifyAsTheRuntime(entry, caPEM, garamSignature(t, garamKey, kid)),
		"a message garam signed verifies by what the runtime was handed")

	resp, err := http.Get(entry["keys-url"])
	if err == nil {
		_ = resp.Body.Close()
	}
	assert.Error(t, err, "the control: the system roots do not reach garam's set")
	stranger, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	assert.Error(t, verifyAsTheRuntime(entry, caPEM, garamSignature(t, stranger, kid)),
		"the control: a key garam did not publish is refused")
}
