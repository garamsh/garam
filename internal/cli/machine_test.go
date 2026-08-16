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

// writeTLSKeyPair writes the certificate the listener terminates TLS
// with. What it carries is not what the assembly checks, so it is
// self-signed.
func writeTLSKeyPair(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "garam machine listener"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certFile,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyFile,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))
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

	cert, sealer, err := machineKeyMaterial(certFile, keyFile, caKeyFile)
	require.NoError(t, err, "every file present")
	assert.NotNil(t, cert.PrivateKey)
	assert.NotNil(t, sealer, "the process keeps the key its authorities are sealed under")

	tests := map[string]struct{ certFile, keyFile, caKeyFile string }{
		"no key-encryption key":          {certFile, keyFile, ""},
		"a key-encryption key not there": {certFile, keyFile, filepath.Join(dir, "absent.key")},
		"no certificate":                 {"", keyFile, caKeyFile},
		"no private key":                 {certFile, "", caKeyFile},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			_, _, err := machineKeyMaterial(tt.certFile, tt.keyFile, tt.caKeyFile)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
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
