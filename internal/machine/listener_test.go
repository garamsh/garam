package machine_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
)

// loopback is the listener's own certificate and the pool a client
// trusts it through. What garam presents is not what is under test
// here, but a client has to verify something.
func loopback(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "garam machine listener"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

// listener starts the machine listener on a loopback port and returns
// the base URL and the pool that verifies it.
func listener(t *testing.T, svc machine.Service, next http.Handler) (string, *x509.CertPool) {
	t.Helper()
	server, roots := loopback(t)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := machine.NewServer(svc, server, next)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	return "https://" + lis.Addr().String(), roots
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

// TestNewServer_HandsTheHandlerTheIdentity is the listener end to end:
// a real handshake against a real authority, and the GRN the handler
// reads out of the request. The negotiated protocol is asserted with
// it, because the identity travels on the request rather than on the
// connection and HTTP/2 carries several requests over one.
func TestNewServer_HandsTheHandlerTheIdentity(t *testing.T) {
	issuer := newIssuer(t)
	subject := agentGRN("acme", "bot-1")
	url, roots := listener(t, issuer.service(), machine.IdentityHandler())

	resp, body, err := get(t, url, roots, issuer.issue(t, subject))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"grn":"`+subject.String()+`"}`, body)
	assert.Equal(t, "HTTP/2.0", resp.Proto)
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
	url, roots := listener(t, issuer.service(), machine.IdentityHandler())

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
