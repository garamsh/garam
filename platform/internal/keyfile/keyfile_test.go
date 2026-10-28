package keyfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/keyfile"
)

// writeFile writes a key file and chmods it to mode. The umask masks
// os.WriteFile's mode argument and not os.Chmod's, and the mode is
// read back so one that never reached the file fails the setup rather
// than whatever the caller went on to assert.
func writeFile(t *testing.T, contents string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	require.NoError(t, os.Chmod(path, mode))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equalf(t, mode, info.Mode().Perm(),
		"asked for mode %04o, file has %04o", mode, info.Mode().Perm())
	return path
}

// TestRead_RefusesAFileReadableBeyondItsOwner asserts each refusal
// beside the accepted case, so what fails is the mode rather than
// anything around it (testing.md §Failing).
func TestRead_RefusesAFileReadableBeyondItsOwner(t *testing.T) {
	read, err := keyfile.Read(writeFile(t, "a key", 0o600))
	require.NoError(t, err, "owner-only file")
	assert.Equal(t, []byte("a key"), read)

	tests := map[string]os.FileMode{
		"group readable": 0o640,
		"world readable": 0o604,
		"group writable": 0o620,
	}
	for name, mode := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := keyfile.Read(writeFile(t, "a key", mode))
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

// TestRead_RefusesAMissingFile asserts the refusal beside the accepted
// case, so what fails is the absent file rather than the directory it
// was looked for in: both paths are in the one tmpdir, and only the
// file at the end of them differs.
func TestRead_RefusesAMissingFile(t *testing.T) {
	present := writeFile(t, "a key", 0o600)
	read, err := keyfile.Read(present)
	require.NoError(t, err, "a file that is there")
	assert.Equal(t, []byte("a key"), read)

	_, err = keyfile.Read(filepath.Join(filepath.Dir(present), "absent"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// TestRead_RefusesAFileOwnedByAnotherUser covers the owner half of the
// rule. Proving it needs a file owned by another user, which only a
// privileged process can arrange, so the unprivileged suite skips it
// and the refusal is exercised only where the suite runs as root.
func TestRead_RefusesAFileOwnedByAnotherUser(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("needs a file owned by another user, which an unprivileged process cannot create")
	}
	path := writeFile(t, "a key", 0o600)
	require.NoError(t, os.Chown(path, 1, 1))

	_, err := keyfile.Read(path)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	assert.Contains(t, err.Error(), "owned by another user")
}
