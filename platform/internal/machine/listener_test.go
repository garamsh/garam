package machine_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/keyfile"
	"github.com/garamsh/garam/platform/internal/machine"
)

// identityHandler answers with the GRN the request authenticated as.
// What this listener serves is the surface api/machine.yaml describes;
// here any handler that reads the identity will do, and what these
// tests are about is that it reads one.
func identityHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := machine.IdentityOf(r)
		if !ok {
			http.Error(w, "no machine identity", http.StatusUnauthorized)
			return
		}
		// A failed write after the header cannot be reported to the
		// caller.
		_ = json.NewEncoder(w).Encode(struct {
			GRN string `json:"grn"`
		}{GRN: identity.GRN().String()})
	})
}

func certPath(dir string) string { return filepath.Join(dir, "tls.crt") }
func keyPath(dir string) string  { return filepath.Join(dir, "tls.key") }

func agentGRN(org, id string) grn.GRN      { return grn.New(org, grn.DefaultProject, "agent", id) }
func operatorGRNOf(org, id string) grn.GRN { return grn.New(org, grn.DefaultProject, "operator", id) }

// serverRoot is a garam server certificate authority as these tests
// hold it (ADR-0040): the root a machine identity pins, and the key
// that signs the leaves the listener presents under it.
type serverRoot struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
}

// newServerRoot mints one under the given name.
func newServerRoot(t *testing.T, commonName string) *serverRoot {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &serverRoot{certificate: certificate, key: key}
}

// pool returns what a client pinning this root verifies garam against.
func (r *serverRoot) pool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(r.certificate)
	return pool
}

// issue writes the pair the listener reads into dir, named commonName
// and signed by this root, and returns the leaf it wrote. Writing into
// a directory a listener is already serving from replaces the leaf and
// leaves the root that signs it standing, which is the rotation
// ADR-0040 §Decision 5 is written for.
func (r *serverRoot) issue(t *testing.T, dir, commonName string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}, r.certificate, &key.PublicKey, r.key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(certPath(dir),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyPath(dir),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))

	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return leaf
}

// serverPair writes the certificate the listener presents into dir,
// named commonName and signed by a garam server root minted for this
// call, and returns the pool that verifies it (ADR-0040). Writing into
// a directory a listener is already serving from replaces both the
// certificate and the root that verifies it. Where only the leaf is to
// change, one [serverRoot] issues both.
func serverPair(t *testing.T, dir, commonName string) *x509.CertPool {
	t.Helper()
	root := newServerRoot(t, "garam server root for "+commonName)
	root.issue(t, dir, commonName)
	return root.pool()
}

// presented is the certificate the listener served on this connection.
const presented = "the certificate garam started with"

// liveToken is the token the listeners here know, naming liveSubject.
const liveToken = "garam_at_live"

var liveSubject = agentGRN("acme", "bot-1")

// serve starts the machine listener on a loopback port, presenting the
// pair dir already holds and answering liveToken for liveSubject, and
// returns the base URL. It reads the files per handshake, so what dir
// holds later is what a later connection is served.
func serve(t *testing.T, dir string, next http.Handler) string {
	t.Helper()
	known := newTokens()
	known.set(liveToken, liveSubject)
	return serveTokens(t, dir, known, next)
}

// serveTokens is serve answering presented tokens from tokens.
func serveTokens(t *testing.T, dir string, tokens machine.Tokens, next http.Handler) string {
	t.Helper()
	return serveEvidence(t, dir, tokens, nil, next)
}

// serveEvidence is serveTokens noting each identity in evidence.
func serveEvidence(t *testing.T, dir string, tokens machine.Tokens, evidence *machine.Evidence, next http.Handler) string {
	t.Helper()
	pair, err := keyfile.NewPair(certPath(dir), keyPath(dir))
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := machine.NewServer(tokens, evidence, pair, next)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	return "https://" + lis.Addr().String()
}

// listener starts the machine listener on a loopback port, presenting
// the pair in a directory of its own, and returns the base URL, that
// directory, and the pool that verifies what it presents.
func listener(t *testing.T, next http.Handler) (string, string, *x509.CertPool) {
	t.Helper()
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	return serve(t, dir, next), dir, roots
}

// get performs one request presenting token, or none where it is
// empty. HTTP/2 is attempted, so the answer covers the protocol the
// listener negotiates rather than the one it falls back to.
func get(t *testing.T, url string, roots *x509.CertPool, token string) (*http.Response, string, error) {
	t.Helper()
	c := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13},
		ForceAttemptHTTP2: true,
	}}
	defer c.CloseIdleConnections()
	req, err := http.NewRequest(http.MethodGet, url+"/identity", nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", machine.TokenScheme+" "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(body), nil
}

// served returns the name on the certificate the listener presented,
// read off the connection the request was answered on. Each call is a
// client of its own, so it is a connection of its own.
func served(t *testing.T, url string, roots *x509.CertPool) string {
	t.Helper()
	resp, _, err := get(t, url, roots, liveToken)
	require.NoError(t, err)
	require.NotNil(t, resp.TLS)
	require.NotEmpty(t, resp.TLS.PeerCertificates)
	return resp.TLS.PeerCertificates[0].Subject.CommonName
}

// TestNewServer_HandsTheHandlerTheIdentity is the listener end to end:
// a real handshake, a token, and the GRN the handler reads out of the
// request. The negotiated protocol is asserted with it, because the
// identity travels on the request and HTTP/2 carries several requests
// over one connection. A request presenting no token is the control:
// the same connection carries no identity without one.
func TestNewServer_HandsTheHandlerTheIdentity(t *testing.T) {
	url, _, roots := listener(t, identityHandler())

	resp, body, err := get(t, url, roots, liveToken)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"grn":"`+liveSubject.String()+`"}`, body)
	assert.Equal(t, "HTTP/2.0", resp.Proto)

	resp, body, err = get(t, url, roots, "")
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, body, "no machine identity")
}

// TestNewServer_AsksNoClientCertificate holds that the listener reads
// no client certificate (ADR-0118): its handshake requests none, so a
// client holding one is never asked for it, and the request carries
// the identity its token proved. The token is the control: without it
// the same client carries no identity, so the certificate it holds is
// not what authenticates it.
func TestNewServer_AsksNoClientCertificate(t *testing.T) {
	url, _, roots := listener(t, identityHandler())
	asked := 0
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: roots, MinVersion: tls.VersionTLS13,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			asked++
			return &tls.Certificate{}, nil
		},
	}}}
	t.Cleanup(client.CloseIdleConnections)

	status, body, _ := presenting(t, client, url, liveToken)
	require.Equal(t, http.StatusOK, status, body)
	status, _, _ = presenting(t, client, url, "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Zero(t, asked, "the handshake requested a client certificate")
}

// TestNewServer_ServesAReplacedCertificateWithoutARestart is the
// rotation ADR-0040 decides: the certificate is read per handshake, so
// replacing the files the running listener was started from replaces
// what the next connection is served. Nothing here restarts the server
// — the same *http.Server serves every request below, on the same
// listening socket.
//
// The first connection is the control. Without it, a second answering
// with the replacement would hold for a listener that had been serving
// it all along, and a certificate simply being served is what this
// change does not need to prove.
//
// What this covers is the read per handshake, not the pin: serverPair
// mints a root per call, so the rotation below replaces the root and
// the leaf together and the closing assertion holds because the root
// changed. The rotation that keeps the root is
// TestNewServer_ServesARotatedLeafToAClientThatPinnedTheRoot.
func TestNewServer_ServesAReplacedCertificateWithoutARestart(t *testing.T) {
	url, dir, roots := listener(t, identityHandler())
	require.Equal(t, presented, served(t, url, roots))

	const replacement = "the certificate garam was given afterwards"
	replaced := serverPair(t, dir, replacement)

	assert.Equal(t, replacement, served(t, url, replaced))
	// The old root no longer verifies what is served, which is what
	// makes the answer above the replacement rather than a second copy
	// of the certificate the process started with.
	_, _, err := get(t, url, roots, liveToken)
	assert.Error(t, err)
}

// TestNewServer_ServesARotatedLeafToAClientThatPinnedTheRoot is the
// property ADR-0040 rests on: a machine identity is given one garam
// server root (§Decision 4), the leaf that root signed is replaced
// underneath it (§Decision 5), and the identity reaches garam across
// the rotation holding nothing new. One root signs both leaves here,
// and the same pool verifies both connections.
//
// The client pinning the first leaf itself is the control. It verifies
// that leaf before the rotation and stops verifying after it, so the
// success above is attributable to the root outliving what it signed
// rather than to the leaf never having changed — a client verifying
// nothing at all reads the same without it (testing.md §Failing).
func TestNewServer_ServesARotatedLeafToAClientThatPinnedTheRoot(t *testing.T) {
	root := newServerRoot(t, "the garam server root that signs both")
	dir := t.TempDir()
	first := root.issue(t, dir, presented)
	url := serve(t, dir, identityHandler())

	pinnedRoot := root.pool()
	pinnedLeaf := x509.NewCertPool()
	pinnedLeaf.AddCert(first)
	require.Equal(t, presented, served(t, url, pinnedRoot))
	require.Equal(t, presented, served(t, url, pinnedLeaf))

	const rotated = "the certificate the same root signed afterwards"
	root.issue(t, dir, rotated)

	assert.Equal(t, rotated, served(t, url, pinnedRoot))
	_, _, err := get(t, url, pinnedLeaf, liveToken)
	assert.ErrorContains(t, err, "certificate signed by unknown authority")
}

// TestNewServer_IsVerifiedAgainstTheGaramServerRoot holds that a
// machine identity verifies the listener by the garam server root it
// pins (ADR-0040). Another root failing to verify garam proves only
// that two keys differ, so the connection that succeeds is here beside
// it, made by the same client with the same token — what separates
// them is the root alone.
func TestNewServer_IsVerifiedAgainstTheGaramServerRoot(t *testing.T) {
	url, _, serverRoot := listener(t, identityHandler())

	resp, body, err := get(t, url, serverRoot, liveToken)
	require.NoError(t, err, "a client verifying garam against the garam server root")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{"grn":"`+liveSubject.String()+`"}`, body)

	_, _, err = get(t, url, newServerRoot(t, "another root").pool(), liveToken)
	assert.Error(t, err, "a root that did not sign the listener's certificate does not verify garam")
}

// TestIdentityOf_ReadsNothingOffARequestServedAnyOtherWay keeps the
// accessor from answering where no listener authenticated anything.
func TestIdentityOf_ReadsNothingOffARequestServedAnyOtherWay(t *testing.T) {
	r, err := http.NewRequest(http.MethodGet, "https://garam.example/identity", nil)
	require.NoError(t, err)

	identity, ok := machine.IdentityOf(r)
	assert.False(t, ok)
	assert.Equal(t, grn.GRN{}, identity.GRN())
}
