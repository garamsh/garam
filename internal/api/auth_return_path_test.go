package api_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startLoginWith posts the provider login form with next, as the
// console's Google button does, and answers the state cookie the login
// set and the response.
func startLoginWith(t *testing.T, f *fixture, next string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/login/google",
		strings.NewReader(url.Values{"next": {next}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	for _, c := range rec.Result().Cookies() {
		if c.Name == "garam_oauth_state" {
			return c.Value, rec
		}
	}
	t.Fatal("the login set no state cookie")
	return "", nil
}

// TestLoginWithReturnPath_LandsOnTheConsolePathItWasStartedFor walks the
// provider sign-in from the form the console posts to the callback's
// redirect. The path is held with the state, so the redirect to the
// provider carries none of it, and the callback lands on it.
func TestLoginWithReturnPath_LandsOnTheConsolePathItWasStartedFor(t *testing.T) {
	const next = "/console/invitations/tok-secret"
	f := callbackFixture(t)

	state, login := startLoginWith(t, f, next)

	location := login.Header().Get("Location")
	require.NotEmpty(t, location, "the login redirects to the provider")
	assert.NotContains(t, location, "tok-secret", "nothing the browser is sent repeats the path")
	for _, c := range login.Result().Cookies() {
		assert.NotContains(t, c.Value, "tok-secret", "cookie %s", c.Name)
	}

	rec := f.callback(t, state)
	require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
	assert.Equal(t, next, rec.Header().Get("Location"))
}

// TestLoginWithReturnPath_DropsAPathOffTheConsole is the cross-site
// case: anyone can post the form, and what they post decides only where
// inside the console the login lands. Each path below is refused by
// the rule and lands on /console; the console path above is the
// control.
func TestLoginWithReturnPath_DropsAPathOffTheConsole(t *testing.T) {
	for _, next := range []string{
		"https://evil.example/console",
		"//evil.example/console",
		"/consoleevil",
		"/console/\\evil.example",
		"/console/\torgs",
		"/login",
		"/console/" + strings.Repeat("a", 2048),
		"",
	} {
		t.Run(next, func(t *testing.T) {
			f := callbackFixture(t)
			state, _ := startLoginWith(t, f, next)

			rec := f.callback(t, state)

			require.Equal(t, http.StatusFound, rec.Code)
			assert.Equal(t, "/console", rec.Header().Get("Location"))
		})
	}
}

// TestLoginWithReturnPath_SpendsThePathWithTheState replays the
// callback that completed: the state is spent, and with it the path,
// so the replay is refused rather than redirected anywhere.
func TestLoginWithReturnPath_SpendsThePathWithTheState(t *testing.T) {
	f := callbackFixture(t)
	state, _ := startLoginWith(t, f, "/console/invitations/tok-secret")

	first := f.callback(t, state)
	require.Equal(t, http.StatusFound, first.Code, "the control: the first callback lands")

	replay := f.callback(t, state)
	assert.Equal(t, http.StatusUnauthorized, replay.Code)
	assert.Empty(t, replay.Header().Get("Location"))
}
