package clisession_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/sdk/clisession"
	"github.com/garamsh/garam/platform/sdk/errorx"
)

// platform answers the device grant as garam's does: one poll pending,
// then ada's tokens; each refresh spends its token for the next pair.
type platform struct {
	mu       sync.Mutex
	polls    int
	refresh  string
	issued   int
	loggedIn map[string]bool
}

func (p *platform) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = r.ParseForm()
	answer := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	tokens := func() {
		p.issued++
		p.refresh = "refresh-" + string(rune('0'+p.issued))
		access := "access-" + string(rune('0'+p.issued))
		p.loggedIn[access] = true
		answer(200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600,
			"refresh_token": p.refresh, "refresh_token_expires_in": 30 * 86400})
	}
	switch r.URL.Path {
	case "/api/auth/device":
		if r.Form.Get("client_id") != clisession.ClientID || r.Form.Get("client_name") == "" {
			answer(400, map[string]string{"error": "invalid_client"})
			return
		}
		answer(200, map[string]any{"device_code": "dc", "user_code": "ABCD-EFGH", "verification_uri": "/device",
			"verification_uri_complete": "/device?code=ABCD-EFGH", "expires_in": 600, "interval": 0})
	case "/api/auth/device/token":
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			if p.polls++; p.polls == 1 {
				answer(400, map[string]string{"error": "authorization_pending"})
				return
			}
			tokens()
		case "refresh_token":
			if r.Form.Get("refresh_token") != p.refresh {
				answer(400, map[string]string{"error": "invalid_grant"})
				return
			}
			tokens()
		}
	case "/api/auth/logout":
		delete(p.loggedIn, r.Header.Get("Authorization")[len("Bearer "):])
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newSession(t *testing.T, now *time.Time) (*clisession.Session, *platform) {
	t.Helper()
	p := &platform{loggedIn: map[string]bool{}}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return &clisession.Session{
		Endpoint: srv.URL, Store: clisession.Store{Path: filepath.Join(t.TempDir(), "garam", "credentials")},
		HTTP: srv.Client(), Now: func() time.Time { return *now }, Login: "cowork login",
	}, p
}

// TestSession_SignsInByTheDeviceGrantAndKeepsTheSessionOwnersOnly
// polls past a pending answer, and keeps the tokens in a file only
// its owner can read.
func TestSession_SignsInByTheDeviceGrantAndKeepsTheSessionOwnersOnly(t *testing.T) {
	now := time.Now()
	s, _ := newSession(t, &now)
	code, err := s.Start(context.Background(), "cowork CLI on test")
	require.NoError(t, err)
	assert.Equal(t, s.Endpoint+"/device?code=ABCD-EFGH", s.VerificationURL(code))
	require.NoError(t, s.Wait(context.Background(), code))

	info, err := os.Stat(s.Store.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	bearer, err := s.Bearer(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "access-1", bearer)
}

// TestSession_RefreshesALapsingTokenOnce has the access token lapse,
// and the next bearer spend the refresh token for a new pair.
func TestSession_RefreshesALapsingTokenOnce(t *testing.T) {
	now := time.Now()
	s, p := newSession(t, &now)
	code, err := s.Start(context.Background(), "cowork CLI on test")
	require.NoError(t, err)
	require.NoError(t, s.Wait(context.Background(), code))

	now = now.Add(time.Hour)
	bearer, err := s.Bearer(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "access-2", bearer)
	bearer, err = s.Bearer(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "access-2", bearer, "a live token is used as kept")
	assert.Equal(t, 2, p.issued)
}

// TestSession_IsNotSignedInWithoutALiveSession answers unauthenticated,
// naming the command that signs in, for no session, a spent refresh
// token, and a session past its refresh token's life.
func TestSession_IsNotSignedInWithoutALiveSession(t *testing.T) {
	now := time.Now()
	s, p := newSession(t, &now)
	_, err := s.Bearer(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
	assert.Contains(t, err.Error(), "cowork login")

	code, err := s.Start(context.Background(), "cowork CLI on test")
	require.NoError(t, err)
	require.NoError(t, s.Wait(context.Background(), code))
	p.refresh = "spent elsewhere"
	now = now.Add(time.Hour)
	_, err = s.Bearer(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))

	now = now.Add(31 * 24 * time.Hour)
	_, err = s.Bearer(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
}

// TestStore_RefusesAFileOthersCanRead rather than using a person's
// session from it.
func TestStore_RefusesAFileOthersCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(path, []byte(`{"endpoints":{}}`), 0o644))
	err := clisession.Store{Path: path}.Update(func(*clisession.File) (bool, error) { return false, nil })
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))
}

// TestStore_ReadsTheFileGaramLoginWrites holds the format to the one
// garam's CLI keeps (internal/cli/credentials.go), field by field, so
// a session either CLI signs in is the other's.
func TestStore_ReadsTheFileGaramLoginWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(path, []byte(`{"endpoints":{"https://garam.sh":{
		"accessToken":"a","accessExpiresAt":"2030-01-01T00:00:00Z",
		"refreshToken":"r","refreshExpiresAt":"2030-02-01T00:00:00Z","org":"acme"}}}`), 0o600))
	s := &clisession.Session{Endpoint: "https://garam.sh", Store: clisession.Store{Path: path},
		Now: func() time.Time { return time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC) }}
	bearer, err := s.Bearer(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "a", bearer)
	org, err := s.Org()
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
}

// TestSession_LogoutRevokesAndKeepsTheOrganization forgets the tokens
// once the platform revoked them, and keeps the saved organization.
func TestSession_LogoutRevokesAndKeepsTheOrganization(t *testing.T) {
	now := time.Now()
	s, p := newSession(t, &now)
	code, err := s.Start(context.Background(), "cowork CLI on test")
	require.NoError(t, err)
	require.NoError(t, s.Wait(context.Background(), code))
	require.NoError(t, s.Store.Update(func(f *clisession.File) (bool, error) {
		f.Endpoints[s.Endpoint].Org = "acme"
		return true, nil
	}))
	require.NoError(t, s.Logout(context.Background()))
	assert.Empty(t, p.loggedIn)
	_, err = s.Bearer(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
	org, err := s.Org()
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
}

// TestParseEndpoint_RefusesTokensInTheClear: http only to loopback.
func TestParseEndpoint_RefusesTokensInTheClear(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://garam.sh/": true, "http://localhost:3000": true, "http://127.0.0.1": true,
		"http://garam.sh": false, "https://garam.sh/api": false, "ftp://garam.sh": false,
	} {
		_, err := clisession.ParseEndpoint(raw)
		assert.Equal(t, ok, err == nil, raw)
	}
}
