package policy

import (
	"io"
	"io/fs"
	"os"
	"syscall"

	"github.com/garamsh/garam/lib/errorx"
)

// readKeyFile returns the contents of path, refusing a file any group
// or other user can reach or one owned by a user other than the
// process. The mode and owner are read from the open descriptor, so
// there is no window between the check and the read.
func readKeyFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errorx.Wrap(err, "policy: open deployment key", errorx.KindInvalidArgument)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, errorx.Wrap(err, "policy: stat deployment key", errorx.KindInvalidArgument)
	}
	if info.Mode().Perm()&fs.FileMode(0o077) != 0 {
		return nil, errorx.New(errorx.KindInvalidArgument, "policy: deployment key is readable beyond its owner")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, errorx.New(errorx.KindInvalidArgument, "policy: deployment key ownership is unreadable")
	}
	if uint64(stat.Uid) != uint64(os.Getuid()) {
		return nil, errorx.New(errorx.KindInvalidArgument, "policy: deployment key is owned by another user")
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, errorx.Wrap(err, "policy: read deployment key", errorx.KindInvalidArgument)
	}
	return raw, nil
}
