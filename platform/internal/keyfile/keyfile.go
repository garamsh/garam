// Package keyfile holds the one rule key material is read under
// (ADR-0015): a file only the reading process's owner can reach. The
// rule is neither certificates nor listeners, so it lives here once for
// every process that opens one.
package keyfile

import (
	"io"
	"io/fs"
	"os"
	"syscall"

	"github.com/garamsh/garam/platform/internal/errorx"
)

// Read returns the contents of path, refusing a file any group or other
// user can reach or one owned by a user other than the process. The
// mode and owner are read from the open descriptor rather than the
// path, so there is no window between the check and the read.
func Read(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errorx.Wrap(err, "keyfile: open", errorx.KindInvalidArgument)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, errorx.Wrap(err, "keyfile: stat", errorx.KindInvalidArgument)
	}
	if info.Mode().Perm()&fs.FileMode(0o077) != 0 {
		return nil, errorx.New(errorx.KindInvalidArgument, "keyfile: file is readable beyond its owner")
	}
	// Owner-only bits protect nothing from a process that is not the
	// owner, so the mode above is only half the rule.
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errorx.New(errorx.KindInvalidArgument, "keyfile: file ownership is unreadable")
	}
	if uint64(stat.Uid) != uint64(os.Getuid()) {
		return nil, errorx.New(errorx.KindInvalidArgument, "keyfile: file is owned by another user")
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, errorx.Wrap(err, "keyfile: read", errorx.KindInvalidArgument)
	}
	return raw, nil
}
