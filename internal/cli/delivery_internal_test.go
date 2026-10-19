package cli

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/machine"
)

// clientOf is the client garam is reached through, from the server
// root the deployment mounts for it.
func clientOf(serverRootFile string) (*http.Client, error) {
	return adapterClient(serverRootFile, string(envAdapterServerRootFile), "")
}

// heldCertificate is the pair the listener presents, held rather than
// reloaded — the seam ADR-0040 put there, in the form a test can hold.
type heldCertificate struct{ pair *tls.Certificate }

func (h heldCertificate) Get() (*tls.Certificate, error) { return h.pair, nil }

// TestAdapterClient_MeetsTheMachineListenerOverHTTP11 pins the protocol
// the shipped client and the shipped listener actually agree on. The
// listener offers h2 — machine.NewServer names it in NextProtos and
// ServeTLS configures HTTP/2 when TLSNextProto is nil — and the client
// declines it, because a Transport carrying a TLSClientConfig has
// HTTP/2 disabled unless ForceAttemptHTTP2 says otherwise, which
// adapterClient does not set.
//
// The control is the same connection made by a client differing in that
// one field: it negotiates HTTP/2, which is what says the listener
// offers it and that the subject's answer is the client's doing. It is
// also what fails if the listener ever stops offering h2, so this test
// cannot quietly become a tautology.
func TestAdapterClient_MeetsTheMachineListenerOverHTTP11(t *testing.T) {
	// Internal: the adapter command exposes no client to read.
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir)
	// The listener's certificate is self-signed, so it is also the root
	// the caller pins it by.
	certPEM, err := os.ReadFile(certFile)
	require.NoError(t, err)
	rootFile := filepath.Join(dir, "server-root.pem")
	require.NoError(t, os.WriteFile(rootFile, certPEM, 0o600))

	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	srv := machine.NewServer(identityTokens{operators: liveTokens{}}, nil, heldCertificate{pair: &pair},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	// The certificate names localhost, so that is the name to reach it
	// by; the port is whatever the listener took.
	addr, ok := ln.Addr().(*net.TCPAddr)
	require.True(t, ok, `net.Listen("tcp", ...) returns a *net.TCPAddr`)
	url := "https://localhost:" + strconv.Itoa(addr.Port)

	subject, err := clientOf(rootFile)
	require.NoError(t, err)
	resp, err := subject.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, "HTTP/1.1", resp.Proto, "the shipped adapter client")

	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(certPEM))
	control := &http.Client{Transport: &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13},
	}}
	controlResp, err := control.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = controlResp.Body.Close() })
	assert.Equal(t, "HTTP/2.0", controlResp.Proto, "the control: the listener does offer h2")
}

// TestAdapterClient_DialsTheControlSocket holds what a standalone
// supervisor's control is reached through (ADR-0085): a client given a
// socket reaches the listener on it under the URL's name, verified as
// that name. The control is the same client without the socket, which
// dials the name over TCP and finds nothing there, so what connects is
// the socket and not the address.
func TestAdapterClient_DialsTheControlSocket(t *testing.T) {
	// Internal: the adapter dials control only on a live activation.
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir)
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	require.NoError(t, err)
	socket := filepath.Join(dir, "control.sock")
	ln, err := net.Listen("unix", socket)
	require.NoError(t, err)
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13},
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	// The listener's certificate is self-signed for localhost, so it is
	// also the root the caller verifies it by.
	subject, err := adapterClient(certFile, "control root", socket)
	require.NoError(t, err)
	resp, err := subject.Get("https://localhost/v1/agents")
	require.NoError(t, err, "the socket answers under the name the certificate carries")
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	control, err := adapterClient(certFile, "control root", "")
	require.NoError(t, err)
	controlResp, err := control.Get("https://localhost/v1/agents")
	if controlResp != nil {
		_ = controlResp.Body.Close()
	}
	require.Error(t, err, "the control: without the socket the name is dialled and nothing answers")
}
