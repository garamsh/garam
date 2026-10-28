package ca_test

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/ca"
	"github.com/garamsh/garam/platform/internal/errorx"
)

// keyIDPrefix is how many leading bytes of a sealed value name the
// key-encryption key that opens it (see [ca.Sealer]).
const keyIDPrefix = 8

// acmeRow is the row a sealed value belongs to, as [ca.Sealer] takes it.
var acmeRow = []byte("grn:root:default:org:acme")

func randomKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return key
}

// keyFile writes a key-encryption key file and chmods it to mode. The
// umask masks os.WriteFile's mode argument and not os.Chmod's, and the
// mode is read back so one that never reached the file fails the setup
// rather than the refusal the caller went on to assert.
func keyFile(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kek")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	require.NoError(t, os.Chmod(path, mode))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equalf(t, mode, info.Mode().Perm(),
		"asked for mode %04o, file has %04o", mode, info.Mode().Perm())
	return path
}

func TestAESGCMSealer_RoundTrip(t *testing.T) {
	ctx := context.Background()
	sealer, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)

	secret := []byte("a private key")
	sealed, err := sealer.Seal(ctx, acmeRow, secret)
	require.NoError(t, err)
	assert.NotContains(t, string(sealed), string(secret))

	opened, err := sealer.Open(ctx, acmeRow, sealed)
	require.NoError(t, err)
	assert.Equal(t, secret, opened)
}

func TestAESGCMSealer_SealsTheSameValueDifferentlyEachTime(t *testing.T) {
	ctx := context.Background()
	sealer, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)

	first, err := sealer.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)
	second, err := sealer.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
}

// TestNewAESGCMSealer_RejectsAKeyThatIsNotAES256 pins the size to this
// package rather than to crypto/aes: 16 and 24 bytes are key lengths
// crypto/aes accepts, so [ca.KeySize] is the only thing refusing them,
// and the 32-byte key beside them is what the refusal is measured
// against.
func TestNewAESGCMSealer_RejectsAKeyThatIsNotAES256(t *testing.T) {
	tests := map[string]struct {
		size     int
		accepted bool
	}{
		"an aes-128 key": {size: 16},
		"an aes-192 key": {size: 24},
		"an aes-256 key": {size: ca.KeySize, accepted: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			sealer, err := ca.NewAESGCMSealer(make([]byte, tc.size))
			if tc.accepted {
				require.NoError(t, err)
				assert.NotNil(t, sealer)
				return
			}
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

func TestAESGCMSealer_OpenRejectsAlteredBytes(t *testing.T) {
	ctx := context.Background()
	sealer, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)

	sealed, err := sealer.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)
	sealed[len(sealed)-1] ^= 0xff

	_, err = sealer.Open(ctx, acmeRow, sealed)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindDataLoss))
}

func TestAESGCMSealer_OpenRejectsTruncatedBytes(t *testing.T) {
	sealer, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)

	_, err = sealer.Open(context.Background(), acmeRow, []byte("short"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindDataLoss))
}

// TestAESGCMSealer_OpenNamesTheKeyItNeeds pins the property that makes
// a later re-wrap possible: the sealed bytes carry the identifier of
// the key that sealed them, so a process holding another key says which
// one is missing instead of reporting corruption.
func TestAESGCMSealer_OpenNamesTheKeyItNeeds(t *testing.T) {
	ctx := context.Background()
	sealer, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)
	other, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)

	sealed, err := sealer.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)

	_, err = other.Open(ctx, acmeRow, sealed)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))
	assert.Contains(t, err.Error(), hex.EncodeToString(sealed[:keyIDPrefix]))

	otherSealed, err := other.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)
	assert.NotEqual(t, sealed[:keyIDPrefix], otherSealed[:keyIDPrefix])
}

// TestAESGCMSealer_OpenReadsAHeaderThatIsAllThereIs pins where
// truncation stops. Bytes a byte short of the identifier and the nonce
// carry no identifier to read, so they are corruption; bytes exactly
// that long carry one, so what they get is the name of the key they
// need. A guard that refused the second as truncated would report a
// foreign key as corruption, and the two inputs differ by one byte.
func TestAESGCMSealer_OpenReadsAHeaderThatIsAllThereIs(t *testing.T) {
	ctx := context.Background()
	key := randomKey(t)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)
	header := keyIDPrefix + aead.NonceSize()

	sealer, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)
	other, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	sealed, err := other.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)
	require.Greater(t, len(sealed), header, "a sealed value is longer than its header")

	_, err = sealer.Open(ctx, acmeRow, sealed[:header-1])
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindDataLoss),
		"a byte short of a header is corruption, got %v", err)

	_, err = sealer.Open(ctx, acmeRow, sealed[:header])
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition),
		"a whole header names the key it needs, got %v", err)
	assert.Contains(t, err.Error(), hex.EncodeToString(sealed[:keyIDPrefix]))
}

// TestAESGCMSealer_OpenRejectsAnotherBinding is what stops a sealed key
// being moved: the bytes open only where they were written, so copying
// an organization's row onto another organization yields nothing.
func TestAESGCMSealer_OpenRejectsAnotherBinding(t *testing.T) {
	ctx := context.Background()
	sealer, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)

	sealed, err := sealer.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)

	_, err = sealer.Open(ctx, []byte("grn:root:default:org:other"), sealed)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindDataLoss))

	opened, err := sealer.Open(ctx, acmeRow, sealed)
	require.NoError(t, err)
	assert.Equal(t, []byte("a private key"), opened)
}

// TestAESGCMSealer_OpenRejectsASplicedKeyIdentifier keeps the prefix
// from being rewritten: bytes this key sealed, wearing another key's
// identifier, are refused by the name of that key, while the same bytes
// untouched open.
func TestAESGCMSealer_OpenRejectsASplicedKeyIdentifier(t *testing.T) {
	ctx := context.Background()
	mine, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)
	theirs, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)

	mineSealed, err := mine.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)
	theirsSealed, err := theirs.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)

	// The identifier is the only thing that differs from mineSealed.
	spliced := append(append([]byte{}, theirsSealed[:keyIDPrefix]...), mineSealed[keyIDPrefix:]...)
	_, err = mine.Open(ctx, acmeRow, spliced)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))

	opened, err := mine.Open(ctx, acmeRow, mineSealed)
	require.NoError(t, err)
	assert.Equal(t, []byte("a private key"), opened)
}

// TestAESGCMSealer_OpenRejectsBytesSealedWithoutTheKeyIdentifier holds
// the identifier inside what the AEAD authenticates (ADR-0016). Both
// values below are AES-256-GCM under this sealer's own key, carrying its
// own identifier, and differ only in whether that identifier was
// authenticated with them — which is the one thing the prefix check
// cannot see.
func TestAESGCMSealer_OpenRejectsBytesSealedWithoutTheKeyIdentifier(t *testing.T) {
	ctx := context.Background()
	key := randomKey(t)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)

	secret := []byte("a private key")
	sealed, err := sealer.Seal(ctx, acmeRow, secret)
	require.NoError(t, err)
	keyID := sealed[:keyIDPrefix]

	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)
	// sealWith writes the sealed layout — identifier, nonce, ciphertext —
	// under this sealer's key, authenticating what it is given.
	sealWith := func(additionalData []byte) []byte {
		header := make([]byte, keyIDPrefix+aead.NonceSize())
		copy(header, keyID)
		nonce := header[keyIDPrefix:]
		_, err := rand.Read(nonce)
		require.NoError(t, err)
		return aead.Seal(header, nonce, secret, additionalData)
	}

	_, err = sealer.Open(ctx, acmeRow, sealWith(acmeRow))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindDataLoss))

	opened, err := sealer.Open(ctx, acmeRow, sealWith(append(append([]byte{}, keyID...), acmeRow...)))
	require.NoError(t, err)
	assert.Equal(t, secret, opened)
}

func TestNewSealerFromKeyFile_ReadsTheKey(t *testing.T) {
	ctx := context.Background()
	key := randomKey(t)
	sealer, err := ca.NewSealerFromKeyFile(keyFile(t, hex.EncodeToString(key)+"\n", 0o600))
	require.NoError(t, err)

	sealed, err := sealer.Seal(ctx, acmeRow, []byte("a private key"))
	require.NoError(t, err)

	// The same key from anywhere opens what it sealed.
	same, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	opened, err := same.Open(ctx, acmeRow, sealed)
	require.NoError(t, err)
	assert.Equal(t, []byte("a private key"), opened)
}

func TestNewSealerFromKeyFile_Refuses(t *testing.T) {
	key := hex.EncodeToString(randomKey(t))
	tests := map[string]string{
		"no path":            "",
		"missing file":       filepath.Join(t.TempDir(), "absent"),
		"group readable":     keyFile(t, key, 0o640),
		"world readable":     keyFile(t, key, 0o604),
		"not hex":            keyFile(t, "not a key", 0o600),
		"wrong key size":     keyFile(t, hex.EncodeToString([]byte("short")), 0o600),
		"empty":              keyFile(t, "", 0o600),
		"hex of empty bytes": keyFile(t, "\n", 0o600),
	}
	for name, path := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ca.NewSealerFromKeyFile(path)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}
