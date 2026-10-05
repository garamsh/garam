package delivery_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/errorx"
)

// privateKey is what an adapter-private file holds in these tests: the
// content no refusal may repeat.
const privateKey = "-----BEGIN EC PRIVATE KEY-----\nnot-a-real-key\n-----END EC PRIVATE KEY-----\n"

// outboxBeside makes an outbox directory and, outside it, a private
// file of the adapter's, as a pod mounts both.
func outboxBeside(t *testing.T) (dir, private string) {
	t.Helper()
	root := t.TempDir()
	dir = filepath.Join(root, "outbox")
	require.NoError(t, os.Mkdir(dir, 0o700))
	private = filepath.Join(root, "credentials", "key.pem")
	require.NoError(t, os.Mkdir(filepath.Dir(private), 0o700))
	require.NoError(t, os.WriteFile(private, []byte(privateKey), 0o600))
	return dir, private
}

// TestOutbox_ReadsOnlyARegularEntryInsideIt is the file boundary of
// #1169's 18:37 decision: a tool that can write the outbox can plant
// anything there, so a link to an adapter-private path, a link inside
// it, a second name for a file, a FIFO, an oversized file and a file
// not named for its identifier are each refused, and none of their
// contents reaches the error. The valid entry beside them is the
// control.
func TestOutbox_ReadsOnlyARegularEntryInsideIt(t *testing.T) {
	dir, private := outboxBeside(t)
	writeEntry(t, dir, entryFor("entry-valid", "reply"))
	require.NoError(t, os.Symlink(private, filepath.Join(dir, "entry-key.json")))
	// Each link below names a file that would read as a valid entry of
	// the link's own name, so only the refusal to follow it refuses it.
	outside := filepath.Join(filepath.Dir(private), "entry-escape.json")
	writeEntry(t, filepath.Dir(private), entryFor("entry-escape", "planted outside"))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "entry-escape.json")))
	writeEntry(t, dir, entryFor("entry-inside", "planted inside"))
	require.NoError(t, os.Rename(filepath.Join(dir, "entry-inside.json"), filepath.Join(dir, ".planted")))
	require.NoError(t, os.Symlink(".planted", filepath.Join(dir, "entry-inside.json")))
	writeEntry(t, dir, entryFor("entry-linked", "reply"))
	require.NoError(t, os.Link(filepath.Join(dir, "entry-linked.json"), filepath.Join(dir, "entry-second.json")))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "entry-fifo.json"), 0o600))
	writeEntry(t, dir, entryFor("entry-large", strings.Repeat("x", 1<<20)))
	writeEntry(t, dir, entryFor("entry-named", "reply"))
	require.NoError(t, os.Rename(filepath.Join(dir, "entry-named.json"), filepath.Join(dir, "entry-misnamed.json")))

	outbox, err := delivery.NewOutbox(dir)
	require.NoError(t, err)
	valid, err := outbox.Read("entry-valid.json")
	require.NoError(t, err, "the control: a regular entry inside the outbox is read")
	assert.Equal(t, "entry-valid", valid.ID)
	assert.Equal(t, "reply", valid.Content)

	for _, name := range []string{
		"entry-key.json", "entry-escape.json", "entry-inside.json", "entry-linked.json", "entry-second.json",
		"entry-fifo.json", "entry-large.json", "entry-misnamed.json",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := outbox.Read(name)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition), "%s is refused, not missing: %v", name, err)
			assert.NotContains(t, err.Error(), "not-a-real-key")
		})
	}

	names, err := outbox.Names(100, func(string) bool { return false })
	require.NoError(t, err)
	assert.NotContains(t, names, ".entry-valid.tmp", "a writer's temporary file is never an entry")
}

// TestOutbox_NamesTheOldestEntriesItIsNotToldToPassOver holds the
// directory read to its bound: over several chunks of a large outbox,
// it answers no more than the limit, the first in name order among the
// entries not passed over. The entries passed over are the control:
// they would be the first answered otherwise.
func TestOutbox_NamesTheOldestEntriesItIsNotToldToPassOver(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 600; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("entry-%04d.json", i)), []byte("{}"), 0o600))
	}
	outbox, err := delivery.NewOutbox(dir)
	require.NoError(t, err)

	names, err := outbox.Names(5, func(name string) bool { return name < "entry-0003.json" })
	require.NoError(t, err)
	assert.Equal(t, []string{
		"entry-0003.json", "entry-0004.json", "entry-0005.json", "entry-0006.json", "entry-0007.json",
	}, names)
}

// TestOutbox_RemovesOnlyTheFileThatWasRead is the acknowledgement of
// one reply that must not erase another: a file replaced under the same
// name after it was read is kept. Removing the same entry unreplaced is
// the control.
func TestOutbox_RemovesOnlyTheFileThatWasRead(t *testing.T) {
	dir, _ := outboxBeside(t)
	writeEntry(t, dir, entryFor("entry-1", "the reply that was offered"))
	writeEntry(t, dir, entryFor("entry-2", "another reply"))
	outbox, err := delivery.NewOutbox(dir)
	require.NoError(t, err)

	read, err := outbox.Read("entry-1.json")
	require.NoError(t, err)
	writeEntry(t, dir, entryFor("entry-1", "a reply written after it was read"))
	require.Error(t, outbox.Remove(read), "a replaced entry is not removed")
	kept, err := outbox.Read("entry-1.json")
	require.NoError(t, err)
	assert.Equal(t, "a reply written after it was read", kept.Content, "and the replacement is what remains")

	control, err := outbox.Read("entry-2.json")
	require.NoError(t, err)
	require.NoError(t, outbox.Remove(control), "the control: the file that was read is removed")
	assert.NoFileExists(t, filepath.Join(dir, "entry-2.json"))
}
