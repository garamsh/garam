package cli_test

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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
)

// selfSigned returns a certificate and its key in PEM form. What either
// carries is not what the commands below check, so one shape answers
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
// garam by (ADR-0040), owner-only like the key material beside it.
func writeServerRoot(t *testing.T, dir string) string {
	t.Helper()
	certPEM, _ := selfSigned(t, "garam server root")
	path := filepath.Join(dir, "server-root.pem")
	require.NoError(t, os.WriteFile(path, certPEM, 0o600))
	return path
}

// writeTLSKeyPair writes a certificate and its key as key material
// arrives: the key readable by its owner alone.
func writeTLSKeyPair(t *testing.T, dir string) (string, string) {
	t.Helper()
	certPEM, keyPEM := selfSigned(t, "garam machine listener")
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))
	return certFile, keyFile
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

// machineStart runs `serve machine` over the four key-material files
// and answers the error it stopped with. GARAM_MACHINE_ISSUER is unset,
// which refuses the start on the variable read after the key material,
// so a start that got past the files names that variable.
func machineStart(t *testing.T, certFile, keyFile, caKeyFile, rootFile string) error {
	t.Helper()
	for name, value := range map[string]string{
		"GARAM_DATABASE_URL":             unreachableDatabase,
		"GARAM_MACHINE_TLS_CERT_FILE":    certFile,
		"GARAM_MACHINE_TLS_KEY_FILE":     keyFile,
		"GARAM_CA_KEY_FILE":              caKeyFile,
		"GARAM_MACHINE_SERVER_ROOT_FILE": rootFile,
		"GARAM_AGENT_TOKEN_LIFETIME":     "",
		"GARAM_MACHINE_ISSUER":           "",
	} {
		t.Setenv(name, value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := runRootContext(ctx, t, "serve", "machine")
	require.Error(t, err)
	return err
}

// pastTheKeyMaterial reports a start refused on the variable read
// after the key material, which only a start that read all four files
// reaches.
func pastTheKeyMaterial(err error) bool {
	return err != nil && strings.Contains(err.Error(), "GARAM_MACHINE_ISSUER")
}

// TestMachineKeyMaterial_RefusesWhenAFileIsMissing covers what the
// process will not run without. Each refusal sits beside the same start
// with the file it names present, so what fails is the file that is
// missing rather than the two beside it.
func TestMachineKeyMaterial_RefusesWhenAFileIsMissing(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir)
	caKeyFile := writeKeyEncryptionKey(t, dir)
	rootFile := writeServerRoot(t, dir)

	err := machineStart(t, certFile, keyFile, caKeyFile, rootFile)
	require.True(t, pastTheKeyMaterial(err), "every file present: %v", err)

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
			err := machineStart(t, tt.certFile, tt.keyFile, tt.caKeyFile, tt.rootFile)
			assert.False(t, pastTheKeyMaterial(err), "refused at the key material: %v", err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
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

	err := machineStart(t, certFile, keyFile, caKeyFile, rootFile)
	require.True(t, pastTheKeyMaterial(err), "owner-only key: %v", err)

	require.NoError(t, os.Chmod(keyFile, 0o640))
	err = machineStart(t, certFile, keyFile, caKeyFile, rootFile)
	assert.False(t, pastTheKeyMaterial(err), "refused at the key material: %v", err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
}
