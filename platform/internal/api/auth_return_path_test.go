package api_test

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startLoginWith posts the provider login form with next, as the
// site's Google button does for the console, and answers the state
// cookie the login set and the response.
func startLoginWith(t *testing.T, f *fixture, next string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	return startLoginFor(t, f, url.Values{"next": {next}})
}

// startLoginFor posts the provider login form with form, as the site's
// Google button does, and answers the same.
func startLoginFor(t *testing.T, f *fixture, form url.Values) (string, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/auth/login/google",
		strings.NewReader(form.Encode()))
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

// returnPathVectors is testdata/return-path-vectors.json, the table the
// console's TypeScript rule is tested against too.
type returnPathVectors struct {
	Vectors []struct {
		Name     string `json:"name"`
		App      string `json:"app"`
		Input    string `json:"input"`
		InputHex string `json:"inputHex"`
		Accepted bool   `json:"accepted"`
	} `json:"vectors"`
}

// TestLoginWithReturnPath_HoldsTheSharedVectors posts every vector
// both rules share as `next`, for the app it names, and follows the
// login to its callback: an accepted console path is where it lands
// and a dropped one lands on /console, and a cowork path lands on the
// site's handoff to cowork carrying it, or `/` where it was dropped
// (ADR-0135). A cross-site form can post any of them, so what it
// decides is at most where inside one of the apps the login lands. A
// path one rule accepts and the other drops fails here or in
// lib/platform-sdk-ts/src/__tests__/return-path.test.ts; the accepted
// vectors are the control for the dropped ones.
func TestLoginWithReturnPath_HoldsTheSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/return-path-vectors.json")
	require.NoError(t, err)
	var table returnPathVectors
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Vectors)

	for _, v := range table.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			next := v.Input
			if v.InputHex != "" {
				decoded, err := hex.DecodeString(v.InputHex)
				require.NoError(t, err)
				next = string(decoded)
			}
			form := url.Values{"next": {next}}
			if v.App != "" {
				form.Set("app", v.App)
			}
			f := callbackFixture(t)
			state, _ := startLoginFor(t, f, form)

			rec := f.callback(t, state)

			require.Equal(t, http.StatusFound, rec.Code, rec.Body.String())
			want := "/console"
			if v.Accepted {
				want = escapeNonASCII(next)
			}
			if v.App == "cowork" {
				landed := "/"
				if v.Accepted {
					landed = next
				}
				want = coworkHandoffPath(landed)
			}
			assert.Equal(t, want, rec.Header().Get("Location"))
		})
	}
}

// escapeNonASCII is the Location net/http writes for path: each byte
// past ASCII percent-encoded, which a browser reads as the same path.
func escapeNonASCII(path string) string {
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] >= 0x80 {
			fmt.Fprintf(&b, "%%%02x", path[i])
			continue
		}
		b.WriteByte(path[i])
	}
	return b.String()
}

// coworkHandoffPath is the site's handoff to cowork carrying path,
// where a provider sign-in for cowork lands (ADR-0135).
func coworkHandoffPath(path string) string {
	return "/handoff?" + url.Values{"app": {"cowork"}, "next": {path}}.Encode()
}

// TestLoginWithReturnPath_RefusesAnAppNotOnTheList posts an app the
// site hands no sign-in to and finds the login refused before any
// state is issued, rather than read as the console. cowork beside it
// is the control: the same form, starting a login.
func TestLoginWithReturnPath_RefusesAnAppNotOnTheList(t *testing.T) {
	f := callbackFixture(t)
	_, control := startLoginFor(t, f, url.Values{"app": {"cowork"}, "next": {"/acme"}})
	require.Equal(t, http.StatusFound, control.Code)

	for _, app := range []string{"evil", "", "console,cowork"} {
		t.Run(app, func(t *testing.T) {
			form := url.Values{"app": {app}, "next": {"/console"}}
			if app == "console,cowork" {
				form = url.Values{"app": {"console", "cowork"}, "next": {"/console"}}
			}
			req := httptest.NewRequest(http.MethodPost, "/auth/login/google", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Empty(t, rec.Result().Cookies(), "no state is issued")
		})
	}
}

// TestOauthCallback_ChecksTheStoredPathAgain issues a state whose
// stored destination is off the apps, as a row nothing but this
// surface should write would hold, and finds the callback landing on
// /console rather than following it. The console path and cowork's
// handoff beside them are the control: the same store, the same
// callback, landing where it was told.
func TestOauthCallback_ChecksTheStoredPathAgain(t *testing.T) {
	for _, tc := range []struct{ stored, want string }{
		{stored: "/console/invitations/tok-1", want: "/console/invitations/tok-1"},
		{stored: coworkHandoffPath("/acme/page-1"), want: coworkHandoffPath("/acme/page-1")},
		{stored: "//evil.example", want: "/console"},
		{stored: "https://evil.example/console", want: "/console"},
		{stored: "/acme/page-1", want: "/console"},
		{stored: coworkHandoffPath("//evil.example"), want: "/console"},
		{stored: "/handoff?app=evil&next=%2F", want: "/console"},
		{stored: "/handoff?app=cowork&next=%2F&state=x", want: "/console"},
		{stored: "/handoff?next=%2F&app=cowork", want: "/console"},
	} {
		t.Run(tc.stored, func(t *testing.T) {
			f := callbackFixture(t)
			require.NoError(t, f.states.Issue(testCtx(t), "state-abc", time.Minute, tc.stored))

			rec := f.callback(t, "state-abc")

			require.Equal(t, http.StatusFound, rec.Code)
			assert.Equal(t, tc.want, rec.Header().Get("Location"))
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
