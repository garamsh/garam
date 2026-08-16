package ca_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
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

// keyFile writes a key-encryption key file with the given mode.
func keyFile(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kek")
	require.NoError(t, os.WriteFile(path, []byte(contents), mode))
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

func TestAESGCMSealer_RejectsWrongKeySize(t *testing.T) {
	_, err := ca.NewAESGCMSealer(make([]byte, ca.KeySize-1))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
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
