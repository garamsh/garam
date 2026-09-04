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

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/keyfile"
	"github.com/garamsh/garam/internal/machine"
)

// identityHandler answers with the GRN the connection authenticated
// as. What this listener serves is the surface api/machine.yaml
// describes; here any handler that reads the identity will do, and
// what these tests are about is that it reads one.
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

// serve starts the machine listener on a loopback port, presenting the
// pair dir already holds, and returns the base URL. It reads the files
// per handshake, so what dir holds later is what a later connection is
// served.
func serve(t *testing.T, dir string, svc machine.Service, next http.Handler) string {
	t.Helper()
	pair, err := keyfile.NewPair(certPath(dir), keyPath(dir))
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := machine.NewServer(svc, pair, next)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	return "https://" + lis.Addr().String()
}

// listener starts the machine listener on a loopback port, presenting
// the pair in a directory of its own, and returns the base URL, that
// directory, and the pool that verifies what it presents.
func listener(t *testing.T, svc machine.Service, next http.Handler) (string, string, *x509.CertPool) {
	t.Helper()
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	return serve(t, dir, svc, next), dir, roots
}

// get performs one request, presenting client as the certificate — or
// presenting none when it holds none. HTTP/2 is attempted, so the
// answer covers the protocol the listener negotiates rather than the
// one it falls back to.
func get(t *testing.T, url string, roots *x509.CertPool, client tls.Certificate) (*http.Response, string, error) {
	t.Helper()
	cfg := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
	if client.Certificate != nil {
		cfg.Certificates = []tls.Certificate{client}
	}
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, ForceAttemptHTTP2: true}}
	defer c.CloseIdleConnections()
	resp, err := c.Get(url + "/identity")
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
func served(t *testing.T, url string, roots *x509.CertPool, client tls.Certificate) string {
	t.Helper()
	resp, _, err := get(t, url, roots, client)
	require.NoError(t, err)
	require.NotNil(t, resp.TLS)
	require.NotEmpty(t, resp.TLS.PeerCertificates)
	return resp.TLS.PeerCertificates[0].Subject.CommonName
}

// TestNewServer_HandsTheHandlerTheIdentity is the listener end to end:
// a real handshake against a real authority, and the GRN the handler
// reads out of the request. The negotiated protocol is asserted with
// it, because the identity travels on the request rather than on the
// connection and HTTP/2 carries several requests over one.
func TestNewServer_HandsTheHandlerTheIdentity(t *testing.T) {
	issuer := newIssuer(t)
	subject := agentGRN("acme", "bot-1")
	url, _, roots := listener(t, issuer.service(), identityHandler())

	resp, body, err := get(t, url, roots, issuer.issue(t, subject))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"grn":"`+subject.String()+`"}`, body)
	assert.Equal(t, "HTTP/2.0", resp.Proto)
}

// TestNewServer_HandsTheHandlerTheIdentityOnAResumedConnection covers
// the connection a TLS 1.3 ticket resumes. A resumption restores the
// peer certificates without re-running the certificate callbacks, so a
// listener reading the identity off a per-connection cell has to write
// that cell on a callback resumption also runs — otherwise the second
// connection is anonymous and every request on it is refused.
//
// The first connection is the control and the source of the ticket:
// without it there is nothing to resume, and the second answering
// correctly would hold for a listener that never resumed at all. The
// assertion that the second connection did resume is what separates
// the two.
func TestNewServer_HandsTheHandlerTheIdentityOnAResumedConnection(t *testing.T) {
	issuer := newIssuer(t)
	subject := agentGRN("acme", "bot-1")
	url, _, roots := listener(t, issuer.service(), identityHandler())

	// One cache across both requests is what carries the ticket, and
	// closing idle connections between them is what stops the second
	// request reusing the first connection instead of resuming it.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:            roots,
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{issuer.issue(t, subject)},
		ClientSessionCache: tls.NewLRUClientSessionCache(4),
	}}}
	t.Cleanup(client.CloseIdleConnections)

	first := identityOver(t, client, url)
	require.Equal(t, http.StatusOK, first.StatusCode)
	assert.False(t, first.TLS.DidResume, "the connection that mints the ticket")

	client.CloseIdleConnections()
	second := identityOver(t, client, url)
	assert.True(t, second.TLS.DidResume, "the second connection resumed the first")
	assert.Equal(t, http.StatusOK, second.StatusCode,
		"a resumed connection carries the identity the resumed handshake proved")
}

// identityOver performs one identity request and returns the response
// with its body already drained, so the connection is free to be
// closed and resumed.
func identityOver(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	resp, err := client.Get(url + "/identity")
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp
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
	issuer := newIssuer(t)
	client := issuer.issue(t, agentGRN("acme", "bot-1"))
	url, dir, roots := listener(t, issuer.service(), identityHandler())
	require.Equal(t, presented, served(t, url, roots, client))

	const replacement = "the certificate garam was given afterwards"
	replaced := serverPair(t, dir, replacement)

	assert.Equal(t, replacement, served(t, url, replaced, client))
	// The old root no longer verifies what is served, which is what
	// makes the answer above the replacement rather than a second copy
	// of the certificate the process started with.
	_, _, err := get(t, url, roots, client)
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
	issuer := newIssuer(t)
	client := issuer.issue(t, agentGRN("acme", "bot-1"))
	root := newServerRoot(t, "the garam server root that signs both")
	dir := t.TempDir()
	first := root.issue(t, dir, presented)
	url := serve(t, dir, issuer.service(), identityHandler())

	pinnedRoot := root.pool()
	pinnedLeaf := x509.NewCertPool()
	pinnedLeaf.AddCert(first)
	require.Equal(t, presented, served(t, url, pinnedRoot, client))
	require.Equal(t, presented, served(t, url, pinnedLeaf, client))

	const rotated = "the certificate the same root signed afterwards"
	root.issue(t, dir, rotated)

	assert.Equal(t, rotated, served(t, url, pinnedRoot, client))
	_, _, err := get(t, url, pinnedLeaf, client)
	assert.ErrorContains(t, err, "certificate signed by unknown authority")
}

// TestNewServer_IsVerifiedAgainstTheGaramServerRoot covers both
// directions of what an agent pins (ADR-0040): the garam server root
// verifies the listener, and the organization authority that signed the
// agent's own certificate does not. One organization's authority
// failing to verify garam proves only that two keys differ, so the
// connection that succeeds is here beside it, made by the same client
// with the same certificate — what separates them is the root alone.
func TestNewServer_IsVerifiedAgainstTheGaramServerRoot(t *testing.T) {
	issuer := newIssuer(t)
	subject := agentGRN("acme", "bot-1")
	client := issuer.issue(t, subject)
	url, _, serverRoot := listener(t, issuer.service(), identityHandler())

	resp, body, err := get(t, url, serverRoot, client)
	require.NoError(t, err, "a client verifying garam against the garam server root")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{"grn":"`+subject.String()+`"}`, body)

	authority, _ := issuer.open(t, "acme")
	organizationRoot := x509.NewCertPool()
	organizationRoot.AddCert(authority)

	_, _, err = get(t, url, organizationRoot, client)
	assert.Error(t, err, "the organization's own authority does not verify garam")
}

// TestNewServer_CarriesNoIdentityForAConnectionItCannotAuthenticate
// covers what a connection whose certificate does not authenticate
// reaches, each case beside the one that succeeds. The connection is
// made either way — that is what leaves room for the enrollment route
// (ADR-0064) — and what separates them is whether an identity reaches
// the handler. Without the accepted case a refusal proves nothing: a
// handler answers 401 as readily for a listener that authenticates
// nobody.
func TestNewServer_CarriesNoIdentityForAConnectionItCannotAuthenticate(t *testing.T) {
	issuer := newIssuer(t)
	subject := agentGRN("acme", "bot-1")
	authenticated := issuer.issue(t, subject)
	issuer.authority(t, "other")
	url, _, roots := listener(t, issuer.service(), identityHandler())

	resp, body, err := get(t, url, roots, authenticated)
	require.NoError(t, err, "the listener answers a certificate its organization signed")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.JSONEq(t, `{"grn":"`+subject.String()+`"}`, body)

	tests := map[string]tls.Certificate{
		"no certificate":                   {},
		"signed by another organization's": issuer.sign(t, "other", leaf{uris: []string{subject.String()}}),
		// A self-signed certificate naming the identity it wants is
		// what an attacker would present, and it is what an enrolling
		// operator's own key would look like. It reaches the listener
		// and carries no identity: what is read is the chain the
		// handshake verified and never the SAN on its own.
		"self-signed, naming the identity it wants": issuer.selfSigned(t, subject),
	}
	for name, client := range tests {
		t.Run(name, func(t *testing.T) {
			resp, body, err := get(t, url, roots, client)
			require.NoError(t, err, "the connection is made")
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			assert.Contains(t, body, "no machine identity")
		})
	}
}

// TestIdentityOf_ReadsNothingOffARequestServedAnyOtherWay keeps the
// accessor from answering where no handshake authenticated anything.
func TestIdentityOf_ReadsNothingOffARequestServedAnyOtherWay(t *testing.T) {
	r, err := http.NewRequest(http.MethodGet, "https://garam.example/identity", nil)
	require.NoError(t, err)

	identity, ok := machine.IdentityOf(r)
	assert.False(t, ok)
	assert.Equal(t, grn.GRN{}, identity.GRN())
}
