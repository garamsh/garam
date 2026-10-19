package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/garamsh/garam/internal/errorx"
)

// credentialsFile is what the CLI keeps between runs (ADR-0120): per
// endpoint, the session's tokens and the organization `garam orgs use`
// saved. The tokens are a session's, so the file is its owner's alone
// and nothing prints it.
type credentialsFile struct {
	Endpoints map[string]*endpointCredentials `json:"endpoints"`
}

// endpointCredentials is one endpoint's entry.
type endpointCredentials struct {
	AccessToken      string    `json:"accessToken,omitempty"`
	AccessExpiresAt  time.Time `json:"accessExpiresAt,omitzero"`
	RefreshToken     string    `json:"refreshToken,omitempty"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt,omitzero"`
	Org              string    `json:"org,omitempty"`
}

// signedIn reports whether the entry holds a session a refresh can
// still renew at now.
func (c *endpointCredentials) signedIn(now time.Time) bool {
	return c != nil && c.RefreshToken != "" && now.Before(c.RefreshExpiresAt)
}

// credentialsPath is $XDG_CONFIG_HOME/garam/credentials, or
// ~/.config/garam/credentials where that is unset. A relative
// XDG_CONFIG_HOME is ignored, as the XDG base directory specification
// has it.
func credentialsPath() (string, error) {
	base := envOr(envXDGConfigHome, "")
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errorx.Wrap(err, "home directory", errorx.KindFailedPrecondition)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "garam", "credentials"), nil
}

// credentialStore reads and writes the credentials file under an
// exclusive lock on a file beside it, so two commands refreshing at
// once spend the refresh token once: the second reads what the first
// wrote.
type credentialStore struct {
	path string
}

// update runs change on the file's contents under the lock and writes
// what it leaves when it answers true.
func (s credentialStore) update(change func(*credentialsFile) (bool, error)) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errorx.Wrap(err, "credentials directory")
	}
	lock, err := os.OpenFile(s.path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
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
func (s credentialStore) read() (*credentialsFile, error) {
	file := &credentialsFile{Endpoints: map[string]*endpointCredentials{}}
	info, err := os.Stat(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return file, nil
	}
	if err != nil {
		return nil, errorx.Wrap(err, "credentials file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errorx.New(errorx.KindFailedPrecondition, fmt.Sprintf(
			"%s is readable by others; chmod 600 it, or remove it and sign in again", s.path))
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, errorx.Wrap(err, "credentials file")
	}
	if err := json.Unmarshal(raw, file); err != nil {
		return nil, errorx.Wrap(err, s.path, errorx.KindFailedPrecondition)
	}
	if file.Endpoints == nil {
		file.Endpoints = map[string]*endpointCredentials{}
	}
	return file, nil
}

// write replaces the file whole, through a file created 0600 beside it,
// so a reader sees the old contents or the new and never a part.
func (s credentialStore) write(file *credentialsFile) error {
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return errorx.Wrap(err, "credentials file")
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".credentials-*")
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
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return errorx.Wrap(err, "credentials file")
	}
	return nil
}
