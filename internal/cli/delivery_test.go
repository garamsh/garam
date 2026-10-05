// Internal test package: what the adapter refuses to start without is
// unexported wiring, and no exported surface reaches it.
package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/machine"
)

// build builds the loop from one set of what the environment names the
// adapter.
func (s adapterSettings) build() error {
	_, err := adapterLoop(s)
	return err
}

// clientOf is the client garam is reached through, from the three files
// the deployment mounts for it.
func clientOf(certFile, keyFile, serverRootFile string) (*http.Client, error) {
	pair, err := adapterPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return adapterClient(pair, serverRootFile, string(envAdapterServerRootFile))
}

// adapterKeyMaterial writes what the adapter presents and the garam
// server root it verifies garam by (ADR-0040), which is a certificate
// of its own rather than the authority that signed the agent.
func adapterKeyMaterial(t *testing.T) (certFile, keyFile, serverRootFile string) {
	t.Helper()
	dir := t.TempDir()
	certFile, keyFile = writeTLSKeyPair(t, dir)
	return certFile, keyFile, writeServerRoot(t, dir)
}

// TestAdapterLoop_RefusesWhatItCannotRunOn covers what the process will
// not start without, in either mode ADR-0084 lets the deployment
// choose. The two working sets are the controls: each case below
// changes one value of one of them, so what refuses is that value
// rather than anything beside it.
func TestAdapterLoop_RefusesWhatItCannotRunOn(t *testing.T) {
	certFile, keyFile, serverRootFile := adapterKeyMaterial(t)
	legacy := adapterSettings{
		agent:          machineGRN("acme", "bot-1").String(),
		machineURL:     "https://garam.example:8443",
		gatewayURL:     defaultGatewayURL,
		certFile:       certFile,
		keyFile:        keyFile,
		serverRootFile: serverRootFile,
	}
	require.NoError(t, legacy.build(), "the legacy control: no control settings")
	withOutbox := legacy
	withOutbox.outboxDir = t.TempDir()
	require.NoError(t, withOutbox.build(), "a legacy adapter forwards an outbox where one is named")
	fenced := withOutbox
	fenced.controlURL = "https://control.example:8443"
	fenced.controlRoot = serverRootFile
	fenced.placementToken = filepath.Join(t.TempDir(), "token")
	require.NoError(t, fenced.build(), "the fenced control: all three control settings and an outbox")

	tests := map[string]adapterSettings{}
	change := func(name string, from adapterSettings, edit func(*adapterSettings)) {
		edit(&from)
		tests[name] = from
	}
	change("no agent", legacy, func(s *adapterSettings) { s.agent = "" })
	change("an agent that is not a GRN", legacy, func(s *adapterSettings) { s.agent = "bot-1" })
	change("no machine listener", legacy, func(s *adapterSettings) { s.machineURL = "" })
	change("a machine listener that is not a URL", legacy, func(s *adapterSettings) { s.machineURL = "garam.example" })
	change("no gateway address", legacy, func(s *adapterSettings) { s.gatewayURL = "" })
	change("no certificate", legacy, func(s *adapterSettings) { s.certFile = "" })
	change("no private key", legacy, func(s *adapterSettings) { s.keyFile = "" })
	change("no server root", legacy, func(s *adapterSettings) { s.serverRootFile = "" })
	change("a server root that is not one", legacy, func(s *adapterSettings) { s.serverRootFile = s.keyFile })
	change("a control address alone", legacy, func(s *adapterSettings) { s.controlURL = fenced.controlURL })
	change("no control address", fenced, func(s *adapterSettings) { s.controlURL = "" })
	change("no control root", fenced, func(s *adapterSettings) { s.controlRoot = "" })
	change("no placement token", fenced, func(s *adapterSettings) { s.placementToken = "" })
	change("no outbox beside the control settings", fenced, func(s *adapterSettings) { s.outboxDir = "" })
	change("a control address that is not https", fenced, func(s *adapterSettings) { s.controlURL = "http://control.example" })
	change("a control root that is not one", fenced, func(s *adapterSettings) { s.controlRoot = s.keyFile })
	for name, settings := range tests {
		t.Run(name, func(t *testing.T) {
			err := settings.build()
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "want an invalid-argument kind, got %v", err)
		})
	}
}

// TestAdapterClient_RefusesAGroupReadableKey holds the agent's own key
// to the rule ADR-0015 states for key material: the owner-only pair
// loads and the same pair with a group-readable key does not, so what
// refuses is the mode rather than anything around it.
func TestAdapterClient_RefusesAGroupReadableKey(t *testing.T) {
	certFile, keyFile, serverRootFile := adapterKeyMaterial(t)

	_, err := clientOf(certFile, keyFile, serverRootFile)
	require.NoError(t, err, "owner-only key")

	require.NoError(t, os.Chmod(keyFile, 0o640))
	_, err = clientOf(certFile, keyFile, serverRootFile)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// TestAdapterClient_RefusesKeyMaterialThatIsNotThere covers the file
// that was configured and never arrived, which a deployment gets wrong
// before it gets anything else wrong. The material that is all there is
// the control: each case below replaces one file of it with a path
// nothing wrote, so what refuses is that file rather than anything
// beside it.
func TestAdapterClient_RefusesKeyMaterialThatIsNotThere(t *testing.T) {
	certFile, keyFile, serverRootFile := adapterKeyMaterial(t)
	absent := filepath.Join(t.TempDir(), "absent.pem")

	_, err := clientOf(certFile, keyFile, serverRootFile)
	require.NoError(t, err, "every file where it was configured")

	for name, args := range map[string][3]string{
		"a certificate that is not there": {absent, keyFile, serverRootFile},
		"a key that is not there":         {certFile, absent, serverRootFile},
		"a server root that is not there": {certFile, keyFile, absent},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := clientOf(args[0], args[1], args[2])
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

// TestRootCommand_CarriesTheAdapter is the command tree as ADR-0003
// keeps it: one CLI, and the adapter is a subcommand of it rather than
// a binary of its own.
func TestRootCommand_CarriesTheAdapter(t *testing.T) {
	adapter, _, err := NewRootCommand().Find([]string{"adapter"})
	require.NoError(t, err)
	assert.Equal(t, "adapter", adapter.Name())
}

// anyMachineIdentity authenticates whatever a handshake presents. What
// this file measures is the protocol the connection ends up on, and a
// refusal here would end the handshake before there is one. Nothing
// here makes a decision that verifies a certificate again.
type anyMachineIdentity struct{ machine.Service }

func (anyMachineIdentity) Authenticate(context.Context, [][]byte) (machine.Identity, error) {
	return machine.Identity{}, nil
}

// heldCertificate is the pair the listener presents, held rather than
// reloaded — the seam ADR-0040 put there, in the form a test can hold.
type heldCertificate struct{ pair *tls.Certificate }

func (h heldCertificate) Get() (*tls.Certificate, error) { return h.pair, nil }

// TestAdapterClient_MeetsTheMachineListenerOverHTTP11 pins the protocol
// the shipped client and the shipped listener actually agree on. The
// listener offers h2 — machine.NewServer sets no NextProtos and
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
	srv := machine.NewServer(anyMachineIdentity{}, heldCertificate{pair: &pair},
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

	subject, err := clientOf(certFile, keyFile, rootFile)
	require.NoError(t, err)
	resp, err := subject.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, "HTTP/1.1", resp.Proto, "the shipped adapter client")

	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(certPEM))
	control := &http.Client{Transport: &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{
			Certificates: []tls.Certificate{pair},
			RootCAs:      roots,
			MinVersion:   tls.VersionTLS13,
		},
	}}
	controlResp, err := control.Get(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = controlResp.Body.Close() })
	assert.Equal(t, "HTTP/2.0", controlResp.Proto, "the control: the listener does offer h2")
}
