package workload_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/workload"
)

// storeDir writes a stopped store: the memory file, its WAL, the lock
// file the writer left, an outbox with one entry, and a file that is
// not state.
func storeDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string, mode os.FileMode) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, rel), []byte(content), mode))
	}
	write("memory.db", "the store", 0o600)
	write("memory.db-wal", "its log", 0o600)
	write("memory.db.lock", "", 0o600)
	write("outbox/entry.json", `{"id":"entry"}`, 0o600)
	write("not-state", "kept out", 0o644)
	old := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "memory.db"), old, old))
	return dir
}

// TestCopyStore_CopiesTheStateAndNothingElse holds what a move carries:
// the memory file and the sidecar that exists, with their modes and
// times, and the outbox; an absent sidecar stays absent, and the lock
// and anything else on the volume stay behind.
func TestCopyStore_CopiesTheStateAndNothingElse(t *testing.T) {
	from, to := storeDir(t), t.TempDir()
	require.NoError(t, workload.CopyStore(from, to))

	for _, rel := range []string{"memory.db", "memory.db-wal", "outbox/entry.json"} {
		want, err := os.ReadFile(filepath.Join(from, rel))
		require.NoError(t, err)
		got, err := os.ReadFile(filepath.Join(to, rel))
		require.NoError(t, err, rel)
		assert.Equal(t, want, got, rel)
		info, err := os.Stat(filepath.Join(to, rel))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), rel)
	}
	info, err := os.Stat(filepath.Join(to, "memory.db"))
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), "the time is kept")
	for _, rel := range []string{"memory.db-shm", "memory.db.lock", "not-state"} {
		_, err := os.Lstat(filepath.Join(to, rel))
		assert.ErrorIs(t, err, os.ErrNotExist, rel)
	}
}

// TestCopyStore_RefusesAStoreWhoseWriterHoldsItsLock holds the copy to
// Sherlock's writer lock: held by another open file, as a writer that
// has not stopped holds it, the copy refuses and writes nothing. The
// control is the same store once the lock is released.
func TestCopyStore_RefusesAStoreWhoseWriterHoldsItsLock(t *testing.T) {
	from, to := storeDir(t), t.TempDir()
	writer, err := os.Open(filepath.Join(from, "memory.db.lock"))
	require.NoError(t, err)
	require.NoError(t, syscall.Flock(int(writer.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))

	require.ErrorIs(t, workload.CopyStore(from, to), workload.ErrStoreLocked)
	entries, err := os.ReadDir(to)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing was copied")

	require.NoError(t, writer.Close())
	require.NoError(t, workload.CopyStore(from, to), "the control: a free lock copies")
}

// TestCopyStore_RefusesWhatItCannotCopyWhole holds the copy's other
// refusals: a target that holds something, a store with no writer lock
// beside it, and a link in the outbox. The control is each source as
// storeDir writes it, into an empty target.
func TestCopyStore_RefusesWhatItCannotCopyWhole(t *testing.T) {
	tests := map[string]func(from, to string) error{
		"a target that holds something": func(_, to string) error {
			return os.WriteFile(filepath.Join(to, "memory.db"), []byte("other"), 0o600)
		},
		"no writer lock": func(from, _ string) error { return os.Remove(filepath.Join(from, "memory.db.lock")) },
		"a link in the outbox": func(from, _ string) error {
			return os.Symlink("..", filepath.Join(from, "outbox", "up"))
		},
	}
	for name, plant := range tests {
		t.Run(name, func(t *testing.T) {
			from, to := storeDir(t), t.TempDir()
			require.NoError(t, workload.CopyStore(from, t.TempDir()), "the control")
			require.NoError(t, plant(from, to))
			assert.Error(t, workload.CopyStore(from, to))
		})
	}
}
