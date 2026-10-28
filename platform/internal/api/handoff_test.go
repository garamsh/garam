package api_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
)

// handoffState is a state of the length the receiving app draws.
var handoffState = strings.Repeat("s", 43)

// issueHandoff asks for a code on the cookies and answers the
// recorder.
func (f *fixture) issueHandoff(t *testing.T, state string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, "POST", "/auth/handoff", fmt.Sprintf(`{"state":%q}`, state), cookies...)
}

// redeemHandoff spends code with state, carrying no session.
func (f *fixture) redeemHandoff(t *testing.T, code, state string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, "POST", "/auth/handoff/redeem", fmt.Sprintf(`{"code":%q,"state":%q}`, code, state))
}

// TestSessionHandoff_RedeemsOnceForASessionOfItsOwn walks the handoff
// ADR-0131 decides: the site's session issues a code, and redeeming it
// sets a host-only cookie for a second session of the same person. A
// second redemption of the same code is refused; the first one
// succeeding is the control that the refusal is the code being spent.
func TestSessionHandoff_RedeemsOnceForASessionOfItsOwn(t *testing.T) {
	f := newFixture(t)
	site := f.sessionCookieFor(t, "ada", "Ada")

	rec := f.issueHandoff(t, handoffState, site)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	code := decodeBody[apigen.SessionHandoff](t, rec).Code
	require.NotEmpty(t, code)

	rec = f.redeemHandoff(t, code, handoffState)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	console := sessionCookieIn(rec)
	require.NotNil(t, console, "session cookie set")
	assert.Empty(t, console.Domain, "host-only")
	assert.True(t, console.HttpOnly)
	assert.Positive(t, console.MaxAge)
	assert.NotEqual(t, site.Value, console.Value, "a session of its own")

	rec = f.do(t, "GET", "/me", "", &http.Cookie{Name: console.Name, Value: console.Value})
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "grn:root:default:user:ada", decodeBody[apigen.User](t, rec).Grn)

	rec = f.redeemHandoff(t, code, handoffState)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, sessionCookieIn(rec))
}

// TestSessionHandoff_RedeemsOnlyWithTheStateItWasIssuedFor presents a
// live code with another state, which is the page that did not ask for
// the code trying it: refused, and the code is still redeemable with
// its own state afterwards, which is the control.
func TestSessionHandoff_RedeemsOnlyWithTheStateItWasIssuedFor(t *testing.T) {
	f := newFixture(t)
	rec := f.issueHandoff(t, handoffState, f.sessionCookieFor(t, "ada", "Ada"))
	require.Equal(t, http.StatusCreated, rec.Code)
	code := decodeBody[apigen.SessionHandoff](t, rec).Code

	rec = f.redeemHandoff(t, code, strings.Repeat("x", 43))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, sessionCookieIn(rec))

	rec = f.redeemHandoff(t, code, handoffState)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// TestSessionHandoff_RefusesABearer presents one session token as a
// bearer and as the cookie: a CLI's session buys no browser session
// (ADR-0120), and the cookie being answered is the control that the
// token itself is good.
func TestSessionHandoff_RefusesABearer(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "ada", "Ada")

	req := httptest.NewRequest("POST", "/auth/handoff", strings.NewReader(fmt.Sprintf(`{"state":%q}`, handoffState)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cookie.Value)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = f.issueHandoff(t, handoffState, cookie)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

// TestSessionHandoff_RefusesAShortState issues with a state below the
// bound beside one at it: a state short enough to guess would let a
// page that did not ask redeem the code.
func TestSessionHandoff_RefusesAShortState(t *testing.T) {
	f := newFixture(t)
	cookie := f.sessionCookieFor(t, "ada", "Ada")

	assert.Equal(t, http.StatusBadRequest, f.issueHandoff(t, strings.Repeat("s", 31), cookie).Code)
	assert.Equal(t, http.StatusCreated, f.issueHandoff(t, strings.Repeat("s", 32), cookie).Code)
}

// TestSessionHandoff_NeedsASession issues without a session beside the
// same request with one.
func TestSessionHandoff_NeedsASession(t *testing.T) {
	f := newFixture(t)

	assert.Equal(t, http.StatusUnauthorized, f.issueHandoff(t, handoffState).Code)
	assert.Equal(t, http.StatusCreated, f.issueHandoff(t, handoffState, f.sessionCookieFor(t, "ada", "Ada")).Code)
}
