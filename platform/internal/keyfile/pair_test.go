package keyfile_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/keyfile"
)

// writePair writes a self-signed certificate and its key into dir under
// the names a pair arrives as, naming the certificate commonName so a
// test can tell one from another. What the certificate carries is not
// what this package checks.
func writePair(t *testing.T, dir, commonName string) (certFile, keyFile string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
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

	certFile = filepath.Join(dir, "tls.crt")
	keyFile = filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certFile,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(keyFile,
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600))
	return certFile, keyFile
}

// commonName is what the pair a Get answered with names, which is how
// these tests tell one certificate from another.
func commonName(t *testing.T, pair *keyfile.Pair) string {
	t.Helper()
	got, err := pair.Get()
	require.NoError(t, err)
	require.NotEmpty(t, got.Certificate)
	leaf, err := x509.ParseCertificate(got.Certificate[0])
	require.NoError(t, err)
	return leaf.Subject.CommonName
}

// TestPair_GetAnswersWithTheFilesAsTheyAreNow is the reload this type
// exists for: the same Pair answers with what the files hold at the
// moment it is asked, so replacing them replaces what a later caller
// gets. The first Get is the control — without it a second answer
// naming the replacement would hold for a Pair that never read the
// first one.
func TestPair_GetAnswersWithTheFilesAsTheyAreNow(t *testing.T) {
	dir := t.TempDir()
	writePair(t, dir, "first")
	pair, err := keyfile.NewPair(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"))
	require.NoError(t, err)
	require.Equal(t, "first", commonName(t, pair))

	writePair(t, dir, "second")

	assert.Equal(t, "second", commonName(t, pair))
}

// TestNewPair_RefusesWhatItCannotRead covers the reads that stop a
// process at startup. The pair that loads is the control: each case
// changes one file of it, so what refuses is that file rather than
// anything beside it.
func TestNewPair_RefusesWhatItCannotRead(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writePair(t, dir, "loads")
	absent := filepath.Join(dir, "absent.pem")
	_, err := keyfile.NewPair(certFile, keyFile)
	require.NoError(t, err, "a certificate and the key it was issued with")

	tests := map[string][2]string{
		"a certificate that is not there": {absent, keyFile},
		"a key that is not there":         {certFile, absent},
		"a certificate that is a key":     {keyFile, keyFile},
		"a key that is a certificate":     {certFile, certFile},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := keyfile.NewPair(files[0], files[1])
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

// TestNewPair_RefusesAKeyReadableBeyondItsOwner holds the key half of a
// pair to the rule this package states, which the certificate beside it
// is not held to: the same files load with an owner-only key and do not
// with a group-readable one.
func TestNewPair_RefusesAKeyReadableBeyondItsOwner(t *testing.T) {
	certFile, keyFile := writePair(t, t.TempDir(), "loads")
	_, err := keyfile.NewPair(certFile, keyFile)
	require.NoError(t, err, "owner-only key")

	require.NoError(t, os.Chmod(certFile, 0o644))
	_, err = keyfile.NewPair(certFile, keyFile)
	require.NoError(t, err, "a certificate is public")

	require.NoError(t, os.Chmod(keyFile, 0o640))
	_, err = keyfile.NewPair(certFile, keyFile)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}
