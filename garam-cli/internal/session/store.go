// Package session is the garam CLI's session with the platform
// (ADR-0120): the credentials file it is kept in between runs, the lock
// two commands share it under, the device grant that signs the CLI in,
// and the refresh that keeps the session live. Every command, the
// platform's and cowork's alike (ADR-0133), presents the bearer
// Session.Bearer answers, so a credential read from anywhere else lands
// there and nowhere else.
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/garamsh/garam/lib/errorx"
)

// File is the credentials file: per platform endpoint, the session's
// tokens and the organization `garam orgs use` saved. The tokens are a
// session's, so the file is its owner's alone and nothing prints it.
type File struct {
	Endpoints map[string]*Entry `json:"endpoints"`
}

// Entry is one endpoint's.
type Entry struct {
	AccessToken      string    `json:"accessToken,omitempty"`
	AccessExpiresAt  time.Time `json:"accessExpiresAt,omitzero"`
	RefreshToken     string    `json:"refreshToken,omitempty"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt,omitzero"`
	Org              string    `json:"org,omitempty"`
}

// SignedIn reports whether the entry holds a session a refresh can
// still renew at now.
func (e *Entry) SignedIn(now time.Time) bool {
	return e != nil && e.RefreshToken != "" && now.Before(e.RefreshExpiresAt)
}

// Path is configHome/garam/credentials, or ~/.config/garam/credentials
// where configHome, the caller's XDG_CONFIG_HOME, is empty. A relative
// one is ignored, as the XDG base directory specification has it.
func Path(configHome string) (string, error) {
	if !filepath.IsAbs(configHome) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errorx.Wrap(err, "home directory", errorx.KindFailedPrecondition)
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "garam", "credentials"), nil
}

// Store reads and writes the credentials file at its path under an
// exclusive lock on a file beside it, so two commands refreshing at
// once spend the refresh token once: the second reads what the first
// wrote.
type Store struct {
	Path string
}

// Update runs change on the file's contents under the lock and writes
// what it leaves when it answers true.
func (s Store) Update(change func(*File) (bool, error)) error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errorx.Wrap(err, "credentials directory")
	}
	lock, err := os.OpenFile(s.Path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return errorx.Wrap(err, "credentials lock")
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return errorx.Wrap(err, "credentials lock")
	}
	file, err := s.read()
	if err != nil {
		return err
	}
	write, err := change(file)
	if err != nil || !write {
		return err
	}
	return s.write(file)
}

// read answers the file's contents, empty where there is no file. A
// file anyone but its owner can read is refused rather than used: the
// tokens in it are a person's sessions.
func (s Store) read() (*File, error) {
	file := &File{Endpoints: map[string]*Entry{}}
	info, err := os.Stat(s.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return nil, errorx.Wrap(err, "credentials file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errorx.New(errorx.KindFailedPrecondition, fmt.Sprintf(
			"%s is readable by others; chmod 600 it, or remove it and sign in again", s.Path))
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, errorx.Wrap(err, "credentials file")
	}
	if err := json.Unmarshal(raw, file); err != nil {
		return nil, errorx.Wrap(err, s.Path, errorx.KindFailedPrecondition)
	}
	if file.Endpoints == nil {
		file.Endpoints = map[string]*Entry{}
	}
	return file, nil
}

// write replaces the file whole, through a file created 0600 beside it,
// so a reader sees the old contents or the new and never a part.
func (s Store) write(file *File) error {
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return errorx.Wrap(err, "credentials file")
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".credentials-*")
	if err != nil {
		return errorx.Wrap(err, "credentials file")
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return errorx.Wrap(err, "credentials file")
	}
	if err := tmp.Close(); err != nil {
		return errorx.Wrap(err, "credentials file")
	}
	if err := os.Rename(tmp.Name(), s.Path); err != nil {
		return errorx.Wrap(err, "credentials file")
	}
	return nil
}
