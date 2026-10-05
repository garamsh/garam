package workload

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/garamsh/garam/internal/errorx"
)

// storeFiles is the runtime's memory file and the two sidecars SQLite
// keeps beside it, which with the outbox are the agent's stored state
// (Sherlock v0.2.0, docs/architecture/deployment.md §Snapshot and
// restore). Its writer lock is not state and is not copied.
var storeFiles = []string{"memory.db", "memory.db-wal", "memory.db-shm"}

// writerLock is the file Sherlock's writer holds an exclusive flock on
// for as long as it has the store open.
const writerLock = "memory.db.lock"

var (
	// ErrStoreLocked is a memory store whose writer lock another
	// process holds: its writer has not stopped.
	ErrStoreLocked = errorx.New(errorx.KindFailedPrecondition,
		"workload: the store's writer lock is held, so its writer has not stopped")
	// ErrTargetNotFresh is a copy asked into a directory that already
	// holds something.
	ErrTargetNotFresh = errorx.New(errorx.KindFailedPrecondition,
		"workload: a copy goes to an empty volume and replaces nothing")
)

// CopyStore copies the stored state under from into to, an empty
// directory, holding the source's writer lock throughout, so a writer
// that has not stopped refuses the copy and none can start during it.
// A sidecar that is absent stays absent. Each file keeps its mode and
// modification time and is owned by the caller; anything in the state
// that is neither a regular file nor a directory refuses.
//
// It runs inside the helper container the supervisor creates for a
// move, where from is the source volume mounted read-only.
func CopyStore(from, to string) error {
	lock, err := os.Open(filepath.Join(from, writerLock))
	if err != nil {
		return errorx.Wrap(err, "workload: the store's writer lock", errorx.KindFailedPrecondition)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrStoreLocked
		}
		return errorx.Wrap(err, "workload: the store's writer lock", errorx.KindFailedPrecondition)
	}
	entries, err := os.ReadDir(to)
	if err != nil {
		return errorx.Wrap(err, "workload: the copy's target", errorx.KindFailedPrecondition)
	}
	if len(entries) > 0 {
		return ErrTargetNotFresh
	}
	if _, err := os.Lstat(filepath.Join(from, storeFiles[0])); err != nil {
		return errorx.Wrap(err, "workload: the memory file", errorx.KindFailedPrecondition)
	}
	for _, name := range storeFiles {
		info, err := os.Lstat(filepath.Join(from, name))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return errorx.Wrap(err, "workload: "+name, errorx.KindFailedPrecondition)
		}
		if err := copyEntry(from, to, name, info); err != nil {
			return err
		}
	}
	if err := copyTree(from, to, outboxDir); err != nil {
		return err
	}
	return syncPath(to)
}

// copyTree copies the directory rel and everything below it, setting
// each directory's mode and time once what it holds is written.
func copyTree(from, to, rel string) error {
	info, err := os.Lstat(filepath.Join(from, rel))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errorx.Wrap(err, "workload: "+rel, errorx.KindFailedPrecondition)
	}
	if !info.IsDir() {
		return errorx.New(errorx.KindFailedPrecondition, "workload: "+rel+" is not a directory")
	}
	if err := os.Mkdir(filepath.Join(to, rel), 0o700); err != nil {
		return errorx.Wrap(err, "workload: "+rel, errorx.KindUnavailable)
	}
	children, err := os.ReadDir(filepath.Join(from, rel))
	if err != nil {
		return errorx.Wrap(err, "workload: "+rel, errorx.KindFailedPrecondition)
	}
	for _, child := range children {
		name := filepath.Join(rel, child.Name())
		if child.IsDir() {
			if err := copyTree(from, to, name); err != nil {
				return err
			}
			continue
		}
		info, err := child.Info()
		if err != nil {
			return errorx.Wrap(err, "workload: "+name, errorx.KindFailedPrecondition)
		}
		if err := copyEntry(from, to, name, info); err != nil {
			return err
		}
	}
	return settle(filepath.Join(to, rel), info)
}

// copyEntry copies one regular file, refusing anything else.
func copyEntry(from, to, rel string, info fs.FileInfo) error {
	if !info.Mode().IsRegular() {
		return errorx.New(errorx.KindFailedPrecondition, "workload: "+rel+" is not a regular file")
	}
	src, err := os.Open(filepath.Join(from, rel))
	if err != nil {
		return errorx.Wrap(err, "workload: "+rel, errorx.KindFailedPrecondition)
	}
	defer func() { _ = src.Close() }()
	path := filepath.Join(to, rel)
	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errorx.Wrap(err, "workload: "+rel, errorx.KindUnavailable)
	}
	_, err = io.Copy(dst, src)
	if err == nil {
		err = dst.Sync()
	}
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return errorx.Wrap(err, "workload: "+rel, errorx.KindUnavailable)
	}
	return settle(path, info)
}

// settle gives path the mode and modification time info names.
func settle(path string, info fs.FileInfo) error {
	err := os.Chmod(path, info.Mode().Perm())
	if err == nil {
		err = os.Chtimes(path, info.ModTime(), info.ModTime())
	}
	if err == nil && info.IsDir() {
		err = syncPath(path)
	}
	if err != nil {
		return errorx.Wrap(err, "workload: "+path, errorx.KindUnavailable)
	}
	return nil
}

func syncPath(path string) error {
	if err := syncDir(path); err != nil {
		return errorx.Wrap(err, "workload: "+path, errorx.KindUnavailable)
	}
	return nil
}
