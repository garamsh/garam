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

// serverPair writes the certificate the listener presents into dir,
// named commonName and signed by a garam server root minted for this
// call, and returns the pool that verifies it (ADR-0040). Writing into
// a directory a listener is already serving from replaces both the
// certificate and the root that verifies it, which is what rotation
// does to a running process.
func serverPair(t *testing.T, dir, commonName string) *x509.CertPool {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "garam server root for " + commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	require.NoError(t, err)
	root, err := x509.ParseCertificate(rootDER)
	require.NoError(t, err)

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
	}, root, &key.PublicKey, rootKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(certPath(dir),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyPath(dir),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))

	roots := x509.NewCertPool()
	roots.AddCert(root)
	return roots
}

// presented is the certificate the listener served on this connection.
const presented = "the certificate garam started with"

// listener starts the machine listener on a loopback port, presenting
// the pair in a directory of its own, and returns the base URL, that
// directory, and the pool that verifies what it presents.
func listener(t *testing.T, svc machine.Service, next http.Handler) (string, string, *x509.CertPool) {
	t.Helper()
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	pair, err := keyfile.NewPair(certPath(dir), keyPath(dir))
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := machine.NewServer(svc, pair, next)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	return "https://" + lis.Addr().String(), dir, roots
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

// TestNewServer_RefusesAConnectionItCannotAuthenticate covers the two
// refusals the listener itself makes, each beside the connection that
// succeeds. Without the accepted case a refusal proves nothing: a
// handshake fails as readily for a certificate the listener never
// looked at as for one it rejected, and both refusals here are
// indistinguishable from a listener that is simply unreachable.
func TestNewServer_RefusesAConnectionItCannotAuthenticate(t *testing.T) {
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
	}
	for name, client := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := get(t, url, roots, client)
			assert.Error(t, err)
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
