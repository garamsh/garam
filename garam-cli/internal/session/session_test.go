package session_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/garam-cli/internal/session"
	"github.com/garamsh/garam/lib/errorx"
	platformsdk "github.com/garamsh/garam/lib/platform-sdk-go"
)

// platform answers the device grant as garam's does: each poll the
// next of polls, then ada's tokens; each refresh spends its token for
// the next pair. It records when each poll arrived.
type platform struct {
	mu       sync.Mutex
	polls    []string
	at       []time.Time
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
		p.refresh = "refresh-" + strconv.Itoa(p.issued)
		access := "access-" + strconv.Itoa(p.issued)
		p.loggedIn[access] = true
		answer(200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 3600,
			"refresh_token": p.refresh, "refresh_token_expires_in": 30 * 86400})
	}
	switch r.URL.Path {
	case "/api/auth/device":
		if r.Form.Get("client_id") != "garam-cli" || r.Form.Get("client_name") == "" {
			answer(400, map[string]string{"error": "invalid_client"})
			return
		}
		answer(200, map[string]any{"device_code": "dc", "user_code": "ABCD-EFGH", "verification_uri": "/device",
			"verification_uri_complete": "/device?code=ABCD-EFGH", "expires_in": 600, "interval": 0})
	case "/api/auth/device/token":
		switch r.Form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			p.at = append(p.at, time.Now())
			if len(p.polls) > 0 {
				refusal := p.polls[0]
				p.polls = p.polls[1:]
				answer(400, map[string]string{"error": refusal})
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

func newSession(t *testing.T, now *time.Time, polls ...string) (*session.Session, *platform) {
	t.Helper()
	p := &platform{loggedIn: map[string]bool{}, polls: polls}
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	s := session.New(srv.URL, filepath.Join(t.TempDir(), "garam", "credentials"), srv.Client())
	s.Now = func() time.Time { return *now }
	s.SlowDown = 50 * time.Millisecond
	return s, p
}

func signIn(t *testing.T, s *session.Session) {
	t.Helper()
	code, err := s.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, s.Wait(context.Background(), code))
}

// TestSession_SignsInByTheDeviceGrantAndKeepsTheSessionOwnersOnly
// polls past a pending answer, and keeps the tokens in a file only
// its owner can read, in a directory only its owner can enter.
func TestSession_SignsInByTheDeviceGrantAndKeepsTheSessionOwnersOnly(t *testing.T) {
	now := time.Now()
	s, _ := newSession(t, &now, "authorization_pending")
	code, err := s.Start(context.Background())
	require.NoError(t, err)
	assert.Equal(t, s.Endpoint+"/device?code=ABCD-EFGH", s.VerificationURL(code))
	require.NoError(t, s.Wait(context.Background(), code))

	info, err := os.Stat(s.Store.Path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dir, err := os.Stat(filepath.Dir(s.Store.Path))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dir.Mode().Perm())
	bearer, err := s.Bearer(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "access-1", bearer)
}

// TestSession_SlowsDownWhenToldAndStopsOnTheToken: a slow_down moves
// the interval later before the next poll; the polls before it, at the
// code's own interval of zero, are the control.
func TestSession_SlowsDownWhenToldAndStopsOnTheToken(t *testing.T) {
	now := time.Now()
	s, p := newSession(t, &now, "authorization_pending", "slow_down")
	signIn(t, s)
	require.Len(t, p.at, 3)
	assert.Less(t, p.at[1].Sub(p.at[0]), 50*time.Millisecond)
	assert.GreaterOrEqual(t, p.at[2].Sub(p.at[1]), 50*time.Millisecond)
}

// TestSession_EndsTheGrantOnADenialOrALapse, each with its own kind.
func TestSession_EndsTheGrantOnADenialOrALapse(t *testing.T) {
	for refusal, kind := range map[string]errorx.Kind{
		"access_denied": errorx.KindPermissionDenied,
		"expired_token": errorx.KindDeadlineExceeded,
	} {
		now := time.Now()
		s, _ := newSession(t, &now, "authorization_pending", refusal)
		code, err := s.Start(context.Background())
		require.NoError(t, err)
		err = s.Wait(context.Background(), code)
		assert.True(t, errorx.IsKind(err, kind), "%s: %v", refusal, err)
	}
}

// TestSession_RefreshesALapsingTokenOnce has the access token lapse,
// and the next bearer spend the refresh token for a new pair.
func TestSession_RefreshesALapsingTokenOnce(t *testing.T) {
	now := time.Now()
	s, p := newSession(t, &now)
	signIn(t, s)

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
	assert.Contains(t, err.Error(), "garam login")

	signIn(t, s)
	p.refresh = "spent elsewhere"
	now = now.Add(time.Hour)
	_, err = s.Bearer(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))

	now = now.Add(31 * 24 * time.Hour)
	_, err = s.Bearer(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
}

// TestSession_LogoutRevokesAndKeepsTheOrganization forgets the tokens
// once the platform revoked them, and keeps the saved organization.
func TestSession_LogoutRevokesAndKeepsTheOrganization(t *testing.T) {
	now := time.Now()
	s, p := newSession(t, &now)
	signIn(t, s)
	require.NoError(t, s.SetOrg("acme"))
	signedIn, err := s.Logout(context.Background())
	require.NoError(t, err)
	assert.True(t, signedIn)
	assert.Empty(t, p.loggedIn)
	_, err = s.Bearer(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
	org, err := s.Org()
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
}

// TestStore_RefusesAFileOthersCanRead rather than using a person's
// session from it; the same file tightened again is read, which is the
// control.
func TestStore_RefusesAFileOthersCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(path, []byte(`{"endpoints":{"https://garam.sh":{"org":"acme"}}}`), 0o644))
	s := session.New("https://garam.sh", path, nil)
	_, err := s.Org()
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))
	assert.ErrorContains(t, err, "readable by others")
	require.NoError(t, os.Chmod(path, 0o600))
	org, err := s.Org()
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
}

// TestStore_ReadsTheFileFieldByField holds the format the file has
// kept since ADR-0120, so a session signed in before this package
// existed is still read.
func TestStore_ReadsTheFileFieldByField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(path, []byte(`{"endpoints":{"https://garam.sh":{
		"accessToken":"a","accessExpiresAt":"2030-01-01T00:00:00Z",
		"refreshToken":"r","refreshExpiresAt":"2030-02-01T00:00:00Z","org":"acme"}}}`), 0o600))
	s := session.New("https://garam.sh", path, nil)
	s.Now = func() time.Time { return time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC) }
	bearer, err := s.Bearer(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "a", bearer)
	org, err := s.Org()
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
}

// TestPath_FollowsXDGConfigHomeWhenItIsAbsolute, and falls back to
// ~/.config for a relative one, as the specification has it.
func TestPath_FollowsXDGConfigHomeWhenItIsAbsolute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := session.Path("/etc/xdg-here")
	require.NoError(t, err)
	assert.Equal(t, "/etc/xdg-here/garam/credentials", got)
	got, err = session.Path("relative")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "garam", "credentials"), got)
}

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
		got, err := session.ParseEndpoint(raw)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{
		"http://garam.sh", "ftp://garam.sh", "garam.sh", "https://garam.sh/api",
		"https://user@garam.sh", "https://garam.sh?x=1", "https://",
	} {
		_, err := session.ParseEndpoint(raw)
		assert.Error(t, err, raw)
	}
}

// TestVerificationURL_JoinsThePathTheAPIAnswersToTheEndpoint, and keeps
// an absolute URL as answered.
func TestVerificationURL_JoinsThePathTheAPIAnswersToTheEndpoint(t *testing.T) {
	s := session.New("https://garam.sh", "", nil)
	assert.Equal(t, "https://garam.sh/device?code=BCDF-GHJK",
		s.VerificationURL(&platformsdk.DeviceCode{VerificationUriComplete: "/device?code=BCDF-GHJK"}))
	assert.Equal(t, "https://other.example/device",
		s.VerificationURL(&platformsdk.DeviceCode{VerificationUriComplete: "https://other.example/device"}))
}
