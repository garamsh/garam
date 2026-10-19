package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/authn"
)

// postForm posts values to path as an RFC 6749 form body.
func (f *fixture) postForm(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func refusalOf(t *testing.T, rec *httptest.ResponseRecorder) apigen.OAuthErrorError {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	var refusal apigen.OAuthError
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &refusal))
	return refusal.Error
}

const pollForm = "client_id=garam-cli&grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Adevice_code&device_code=d"

// TestDeviceToken_RefusesAParameterSentTwice: RFC 6749 §3.2 has a
// token endpoint refuse a repeated parameter rather than pick one. The
// same poll with each parameter once is the control.
func TestDeviceToken_RefusesAParameterSentTwice(t *testing.T) {
	f := newFixture(t)
	f.devices.poll = authn.PollResult{Outcome: authn.PollPending}
	assert.Equal(t, apigen.InvalidRequest, refusalOf(t, f.postForm(t, "/auth/device/token", pollForm+"&device_code=e")))
	assert.Equal(t, apigen.AuthorizationPending, refusalOf(t, f.postForm(t, "/auth/device/token", pollForm)))
}

// TestDeviceToken_ServesTheCLIAlone: a client other than garam-cli is
// refused on both endpoints; garam-cli is the control.
func TestDeviceToken_ServesTheCLIAlone(t *testing.T) {
	f := newFixture(t)
	f.devices.poll = authn.PollResult{Outcome: authn.PollPending}
	other := strings.Replace(pollForm, "garam-cli", "other", 1)
	assert.Equal(t, apigen.InvalidClient, refusalOf(t, f.postForm(t, "/auth/device/token", other)))
	assert.Equal(t, apigen.AuthorizationPending, refusalOf(t, f.postForm(t, "/auth/device/token", pollForm)))
	assert.Equal(t, apigen.InvalidClient, refusalOf(t, f.postForm(t, "/auth/device", "client_id=other")))
	assert.Equal(t, http.StatusOK, f.postForm(t, "/auth/device", "client_id=garam-cli").Code)
}

// TestDeviceToken_RefusesAGrantItDoesNotServe, and answers an unknown
// device code invalid_grant rather than pending.
func TestDeviceToken_RefusesAGrantItDoesNotServe(t *testing.T) {
	f := newFixture(t)
	assert.Equal(t, apigen.UnsupportedGrantType,
		refusalOf(t, f.postForm(t, "/auth/device/token", "client_id=garam-cli&grant_type=password")))
	assert.Equal(t, apigen.InvalidGrant, refusalOf(t, f.postForm(t, "/auth/device/token", pollForm)))
}

// TestRequestDeviceCode_BoundsTheClientName: a name is stored trimmed,
// at most 100 characters and free of control characters. A name of
// exactly 100 is the control.
func TestRequestDeviceCode_BoundsTheClientName(t *testing.T) {
	f := newFixture(t)
	request := func(name string) *httptest.ResponseRecorder {
		return f.postForm(t, "/auth/device", url.Values{"client_id": {"garam-cli"}, "client_name": {name}}.Encode())
	}
	assert.Equal(t, apigen.InvalidRequest, refusalOf(t, request(strings.Repeat("é", 101))))
	assert.Equal(t, apigen.InvalidRequest, refusalOf(t, request("laptop\nApproved by garam")))
	rec := request("  " + strings.Repeat("é", 100) + "  ")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"), "a device code is not cached")
	assert.Equal(t, []string{strings.Repeat("é", 100)}, f.devices.issued)
}

// TestLogout_ByBearerRevokesThatSessionAndSetsNoCookie: a CLI signs out
// on its bearer, and nothing is cleared that it did not present. The
// cookie's logout, which expires the cookie, is the control.
func TestLogout_ByBearerRevokesThatSessionAndSetsNoCookie(t *testing.T) {
	f := newFixture(t)
	browser := f.sessionCookieFor(t, "ada", "Ada")
	token, err := authn.NewToken()
	require.NoError(t, err)
	ada, err := f.sessions.Get(testCtx(t), browser.Value)
	require.NoError(t, err)
	require.NoError(t, f.sessions.Create(testCtx(t), token, authn.NewSession(ada.User(), time.Hour)))

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Empty(t, rec.Header().Values("Set-Cookie"))
	_, err = f.sessions.Get(testCtx(t), token)
	assert.ErrorIs(t, err, authn.ErrSessionNotFound, "the bearer's session is revoked")
	_, err = f.sessions.Get(testCtx(t), browser.Value)
	assert.NoError(t, err, "and the browser's is not")

	rec = f.do(t, http.MethodPost, "/auth/logout", "", browser)
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Contains(t, rec.Header().Get("Set-Cookie"), "garam_session=;")
}
