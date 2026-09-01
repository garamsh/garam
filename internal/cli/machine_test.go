// Internal test package: what the machine listener refuses to start
// without, and which authority it verifies a certificate against, are
// unexported wiring with no exported surface that reaches them.
package cli

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/health"
)

func machineGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, "agent", id)
}

func orgGRN(org string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", org)
}

// writeKeyEncryptionKey writes a key-encryption key the way ADR-0015
// has one reach a process: a file its owner alone can read.
func writeKeyEncryptionKey(t *testing.T, dir string) string {
	t.Helper()
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	path := filepath.Join(dir, "ca.key")
	require.NoError(t, os.WriteFile(path, []byte(hex.EncodeToString(key)), 0o600))
	return path
}

// selfSigned returns a certificate and its key in PEM form. What either
// carries is not what the assemblies below check, so one shape answers
// wherever they need a certificate.
func selfSigned(t *testing.T, commonName string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

// writeServerRoot writes the garam server root a machine identity pins
// garam by (ADR-0040). It is a certificate of its own rather than the
// listener's, because the two are what this change separates.
//
// Owner-only like the key material beside it, though the reader of this
// file reads its contents and not its mode: the umask subtracts from
// os.WriteFile's mode argument, so a mode reaching past the owner is one
// the environment decides rather than this helper.
func writeServerRoot(t *testing.T, dir string) string {
	t.Helper()
	certPEM, _ := selfSigned(t, "garam server root")
	path := filepath.Join(dir, "server-root.pem")
	require.NoError(t, os.WriteFile(path, certPEM, 0o600))
	return path
}

// writeTLSKeyPair writes the certificate the listener terminates TLS
// with, as key material arrives: the key readable by its owner alone.
func writeTLSKeyPair(t *testing.T, dir string) (string, string) {
	t.Helper()
	certPEM, keyPEM := selfSigned(t, "garam machine listener")
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))
	return certFile, keyFile
}

// TestMachineKeyMaterial_RefusesWhenAFileIsMissing covers what the
// process will not run without. Each refusal sits beside the same call
// with the file it names present, so what fails is the file that is
// missing rather than the two beside it.
func TestMachineKeyMaterial_RefusesWhenAFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir)
	caKeyFile := writeKeyEncryptionKey(t, dir)
	rootFile := writeServerRoot(t, dir)

	pair, sealer, serverRoot, err := machineKeyMaterial(certFile, keyFile, caKeyFile, rootFile)
	require.NoError(t, err, "every file present")
	presented, err := pair.Get()
	require.NoError(t, err)
	assert.NotNil(t, presented.PrivateKey)
	assert.NotNil(t, sealer, "the process keeps the key its authorities are sealed under")
	assert.NotEmpty(t, serverRoot, "the process keeps the root every agent pins it by")

	tests := map[string]struct{ certFile, keyFile, caKeyFile, rootFile string }{
		"no key-encryption key":          {certFile, keyFile, "", rootFile},
		"a key-encryption key not there": {certFile, keyFile, filepath.Join(dir, "absent.key"), rootFile},
		"no certificate":                 {"", keyFile, caKeyFile, rootFile},
		"no private key":                 {certFile, "", caKeyFile, rootFile},
		"no server root":                 {certFile, keyFile, caKeyFile, ""},
		"a server root not there":        {certFile, keyFile, caKeyFile, filepath.Join(dir, "absent.pem")},
		"a server root that is a key":    {certFile, keyFile, caKeyFile, keyFile},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := machineKeyMaterial(tt.certFile, tt.keyFile, tt.caKeyFile, tt.rootFile)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

// TestMachineKeyMaterial_RefusesAGroupReadableKey holds the listener's
// own key to the rule ADR-0015 states for key material, through
// keyfile: the owner-only pair loads, and the same pair with a
// group-readable key does not — so what refuses is the mode rather
// than anything around it.
func TestMachineKeyMaterial_RefusesAGroupReadableKey(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir)
	caKeyFile := writeKeyEncryptionKey(t, dir)
	rootFile := writeServerRoot(t, dir)

	_, _, _, err := machineKeyMaterial(certFile, keyFile, caKeyFile, rootFile)
	require.NoError(t, err, "owner-only key")

	require.NoError(t, os.Chmod(keyFile, 0o640))
	_, _, _, err = machineKeyMaterial(certFile, keyFile, caKeyFile, rootFile)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// authorities is a ca.Store in memory: the adapter needs a real
// authority per organization and nothing else a store does.
type authorities map[grn.GRN]*ca.Authority

func (a authorities) Create(_ context.Context, authority *ca.Authority) error {
	if _, held := a[authority.Organization()]; held {
		return ca.ErrAuthorityAlreadyExists
	}
	a[authority.Organization()] = authority
	return nil
}

func (a authorities) Get(_ context.Context, org grn.GRN) (*ca.Authority, error) {
	authority, held := a[org]
	if !held {
		return nil, ca.ErrAuthorityNotFound
	}
	return authority, nil
}

// TestCertificateAuthorities_ReadsTheOrganizationsOwnAuthority is the
// tenancy rule at the composition root: which authority a certificate
// has to chain to is read off the organization segment of the identity
// it names. One organization answering with a certificate would hold
// for an adapter that returns whichever authority it finds first, so
// two are here, and a subject of one that holds no certificate of its
// own still reads its organization's.
func TestCertificateAuthorities_ReadsTheOrganizationsOwnAuthority(t *testing.T) {
	ctx := context.Background()
	store := authorities{}
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	issuer, err := ca.NewService(store, sealer, ca.Config{})
	require.NoError(t, err)
	for _, subject := range []grn.GRN{machineGRN("acme", "bot-1"), machineGRN("other", "bot-1")} {
		_, err := issuer.Issue(ctx, subject)
		require.NoError(t, err)
	}

	roots := certificateAuthorities{store: store}
	acme, err := roots.Get(ctx, machineGRN("acme", "bot-2"))
	require.NoError(t, err)
	other, err := roots.Get(ctx, machineGRN("other", "bot-1"))
	require.NoError(t, err)

	assert.Equal(t, store[orgGRN("acme")].Certificate(), acme)
	assert.Equal(t, store[orgGRN("other")].Certificate(), other)
	assert.NotEqual(t, acme, other, "two organizations share one authority")

	_, err = roots.Get(ctx, machineGRN("nobody", "bot-1"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
}

// TestMachineServer_AnswersAPanicOutsideTheSurfacesOwnMiddleware is
// `serve machine`'s wiring. The generated middleware slice reaches the
// spec operations alone, so a panic raised outside them — the identity
// check machine.NewServer puts around the handler it is given, or a
// handler reached any other way — meets the recovery machineServer puts
// outermost or none at all.
//
// Without it the connection is dropped rather than answered and the Get
// below fails outright. The plain-text body is what says this recovery
// answered: the surface's own writes the specification's Error as JSON.
// The line names no route, for the reason apiHandler's test gives.
func TestMachineServer_AnswersAPanicOutsideTheSurfacesOwnMiddleware(t *testing.T) {
	subject := machineGRN("acme", "bot-1")

	var served lines
	answering, client := machineListenerAround(t, recordingObservability(&served), subject,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	resp, err := client.Get(answering + "/identity")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	// The control: the same listener carries a request to a handler that
	// does not panic, so what the 500 below reports is the panic rather
	// than the certificate or the identity check refusing.
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	var out lines
	url, client := machineListenerAround(t, recordingObservability(&out), subject,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("machine listener defect") }))

	resp, err = client.Get(url + "/identity")
	require.NoError(t, err, "the client read no answer at all")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "internal error\n", string(body), "no internals reach the body")

	line := decodeLine(t, &out)
	assert.Equal(t, "http panic", line["msg"])
	assert.Equal(t, float64(http.StatusInternalServerError), line["status"])
	assert.Contains(t, line["error"], "panic: machine listener defect",
		"the line is the only record this panic leaves")
}

// TestMachineProbeHandler_AnswersAPanicOnTheListenerBesideTheTLSOne is
// `serve machine`'s wiring for the port a probe reaches without a
// certificate. Nothing on that port is a spec operation, so the
// recovery this assembly puts outermost is the only one a panic here
// meets.
func TestMachineProbeHandler_AnswersAPanicOnTheListenerBesideTheTLSOne(t *testing.T) {
	var served lines
	status, _ := readiness(t, recordingObservability(&served).machineProbeHandler(presentProbe{}))
	require.Equal(t, http.StatusOK, status, "the probe this handler mounts is reachable")

	var out lines
	status, body := readiness(t, recordingObservability(&out).machineProbeHandler(panickingProbe{}))

	require.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, "internal error\n", body, "no internals reach the body")

	line := decodeLine(t, &out)
	assert.Equal(t, "http panic", line["msg"])
	assert.Equal(t, "GET "+health.ReadyPath, line["operation"])
	assert.Contains(t, line["error"], "panic: readiness defect",
		"the line is the only record this panic leaves")
}
