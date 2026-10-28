package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseEndpoint_AcceptsAnOriginTheTokensMayCross: https anywhere,
// http to loopback alone, and an origin with nothing after it.
func TestParseEndpoint_AcceptsAnOriginTheTokensMayCross(t *testing.T) {
	for raw, want := range map[string]string{
		"https://garam.sh":       "https://garam.sh",
		"https://garam.sh/":      "https://garam.sh",
		"http://127.0.0.1:23148": "http://127.0.0.1:23148",
		"http://localhost:3000/": "http://localhost:3000",
		"http://[::1]:3000":      "http://[::1]:3000",
	} {
		got, err := parseEndpoint(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{
		"http://garam.sh", "ftp://garam.sh", "garam.sh", "https://garam.sh/api",
		"https://user@garam.sh", "https://garam.sh?x=1", "https://",
	} {
		_, err := parseEndpoint(raw)
		assert.Error(t, err, raw)
	}
}

// TestCredentialStore_WritesTheFileOwnerOnlyAndRefusesOneOthersCanRead:
// a write leaves 0600 in 0700; a file loosened afterwards is refused
// rather than read, and the same file tightened again is read, which
// is the control.
func TestCredentialStore_WritesTheFileOwnerOnlyAndRefusesOneOthersCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garam", "credentials")
	store := credentialStore{path: path}
	require.NoError(t, store.update(func(file *credentialsFile) (bool, error) {
		file.Endpoints["https://garam.sh"] = &endpointCredentials{
			RefreshToken: "r", RefreshExpiresAt: time.Now().Add(time.Hour), Org: "acme",
		}
		return true, nil
	}))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dir, err := os.Stat(filepath.Dir(path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm())

	require.NoError(t, os.Chmod(path, 0o644))
	_, err = store.read()
	assert.ErrorContains(t, err, "readable by others")
	require.NoError(t, os.Chmod(path, 0o600))
	file, err := store.read()
	require.NoError(t, err)
	assert.Equal(t, "acme", file.Endpoints["https://garam.sh"].Org)
}

// TestCredentialsPath_FollowsXDGConfigHomeWhenItIsAbsolute, and falls
// back to ~/.config for a relative one, as the specification has it.
func TestCredentialsPath_FollowsXDGConfigHomeWhenItIsAbsolute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(string(envXDGConfigHome), "/etc/xdg-here")
	got, err := credentialsPath()
	require.NoError(t, err)
	assert.Equal(t, "/etc/xdg-here/garam/credentials", got)
	t.Setenv(string(envXDGConfigHome), "relative")
	got, err = credentialsPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "garam", "credentials"), got)
}

// TestVerificationURL_JoinsThePathTheAPIAnswersToTheEndpoint, and keeps
// an absolute URL as answered.
func TestVerificationURL_JoinsThePathTheAPIAnswersToTheEndpoint(t *testing.T) {
	c := &userCommand{endpoint: "https://garam.sh"}
	assert.Equal(t, "https://garam.sh/device?code=BCDF-GHJK", c.verificationURL("/device?code=BCDF-GHJK"))
	assert.Equal(t, "https://other.example/device", c.verificationURL("https://other.example/device"))
}
