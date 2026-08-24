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
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
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
func writeServerRoot(t *testing.T, dir string) string {
	t.Helper()
	certPEM, _ := selfSigned(t, "garam server root")
	path := filepath.Join(dir, "server-root.pem")
	require.NoError(t, os.WriteFile(path, certPEM, 0o644))
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
