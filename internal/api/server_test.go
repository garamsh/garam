package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/authn"
	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/api"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
)

// fixture wires a Server to in-memory doubles.
type fixture struct {
	handler  http.Handler
	users    *fakeUserStore
	orgs     *fakeOrgStore
	sessions *fakeSessionStore
	provider *fakeProvider
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		users:    newFakeUserStore(),
		orgs:     newFakeOrgStore(),
		sessions: newFakeSessionStore(),
		provider: &fakeProvider{name: "google"},
	}
	srv := api.NewServer(f.users, f.orgs, f.sessions, []authn.Provider{f.provider}, api.Config{SessionTTL: time.Hour})
	f.handler = apigen.Handler(srv)
	return f
}

// do issues a request and returns the recorder.
func (f *fixture) do(t *testing.T, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	} else {
		rdr = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

// sessionCookieFor seeds a signed-in user and returns its cookie.
func (f *fixture) sessionCookieFor(t *testing.T, subject, nickname string) *http.Cookie {
	t.Helper()
	g := grn.New("garam", "default", user.ServiceType, subject)
	require.NoError(t, f.users.Create(testCtx(t), user.New(g, nickname)))
	token, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, f.sessions.Create(testCtx(t), token, authn.NewSession(g, time.Hour)))
	return &http.Cookie{Name: "gaon_session", Value: token}
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &v))
	return v
}

func TestLogin_Redirects(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/auth/login/google", "")
	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), "https://idp.example/auth?state=")

	var state, verifier bool
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "gaon_oauth_state":
			state = true
		case "gaon_oauth_verifier":
			verifier = true
		}
	}
	assert.True(t, state, "state cookie set")
	assert.True(t, verifier, "verifier cookie set")
}

func TestLogin_UnknownProvider(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/auth/login/github", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestOauthCallback_StateMismatch(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/auth/callback/google?code=x&state=y", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLoginCallbackMeLogout_RoundTrip(t *testing.T) {
	f := newFixture(t)
	f.provider.identity = &authn.Identity{Subject: "123", Nickname: "Ada"}

	// Login to obtain state and verifier cookies.
	rec := f.do(t, "GET", "/auth/login/google", "")
	require.Equal(t, http.StatusFound, rec.Code)
	var cookies []*http.Cookie
	var state string
	for _, c := range rec.Result().Cookies() {
		cookies = append(cookies, c)
		if c.Name == "gaon_oauth_state" {
			state = c.Value
		}
	}
	require.NotEmpty(t, state)

	// Callback issues a session cookie and upserts the user.
	rec = f.do(t, "GET", "/auth/callback/google?code=ok&state="+state, "", cookies...)
	require.Equal(t, http.StatusFound, rec.Code)
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "gaon_session" {
			session = c
		}
	}
	require.NotNil(t, session, "session cookie set")

	// Me returns the upserted user.
	rec = f.do(t, "GET", "/me", "", session)
	require.Equal(t, http.StatusOK, rec.Code)
	me := decodeBody[apigen.User](t, rec)
	assert.Equal(t, "Ada", me.Nickname)
	assert.Equal(t, "grn:garam:default:user:123", me.Grn)

	// Logout revokes the session.
	rec = f.do(t, "POST", "/auth/logout", "", session)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", "/me", "", session)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMe_NoSession(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/me", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// createOrg creates an org via the API and returns its identifier.
func createOrg(t *testing.T, f *fixture, owner *http.Cookie, name string) string {
	t.Helper()
	rec := f.do(t, "POST", "/orgs", `{"name":"`+name+`"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	body := decodeBody[apigen.Organization](t, rec)
	g, err := grn.Parse(body.Grn)
	require.NoError(t, err)
	return g.Identifier
}

func TestOrgFlow_RoleEnforcement(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")

	slug := createOrg(t, f, owner, "acme")

	// Owner reads and renames.
	rec := f.do(t, "GET", "/orgs/"+slug, "", owner)
	assert.Equal(t, http.StatusOK, rec.Code)
	rec = f.do(t, "PATCH", "/orgs/"+slug, `{"name":"acme2"}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "acme2", decodeBody[apigen.Organization](t, rec).Name)

	// Stranger is forbidden, and the error kind says why.
	rec = f.do(t, "GET", "/orgs/"+slug, "", stranger)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "permission_denied", decodeBody[apigen.Error](t, rec).Kind)

	// A plain member can read but not update or delete.
	member := f.sessionCookieFor(t, "member-sub", "Member")
	orgGRN := grn.New("garam", "default", org.ServiceTypeOrg, slug)
	memberGRN := grn.New("garam", "default", user.ServiceType, "member-sub")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))

	rec = f.do(t, "GET", "/orgs/"+slug, "", member)
	assert.Equal(t, http.StatusOK, rec.Code)
	rec = f.do(t, "PATCH", "/orgs/"+slug, `{"name":"nope"}`, member)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "DELETE", "/orgs/"+slug, "", member)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Membership list shows both members.
	rec = f.do(t, "GET", "/orgs/"+slug+"/members", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeBody[[]apigen.Member](t, rec), 2)

	// Only the owner can delete.
	rec = f.do(t, "DELETE", "/orgs/"+slug, "", owner)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListOrgs_OnlyMine(t *testing.T) {
	f := newFixture(t)
	alice := f.sessionCookieFor(t, "alice-sub", "Alice")
	bob := f.sessionCookieFor(t, "bob-sub", "Bob")

	createOrg(t, f, alice, "alice-org")
	createOrg(t, f, bob, "bob-org")

	rec := f.do(t, "GET", "/orgs", "", alice)
	require.Equal(t, http.StatusOK, rec.Code)
	orgs := decodeBody[[]apigen.Organization](t, rec)
	require.Len(t, orgs, 1)
	assert.Equal(t, "alice-org", orgs[0].Name)
}

func TestCreateOrg_RequiresAuth(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "POST", "/orgs", `{"name":"acme"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestInvitationFlow_Lifecycle(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	// Members cannot invite; owners (admin+) can.
	member := f.sessionCookieFor(t, "member-sub", "Member")
	orgGRN := grn.New("garam", "default", org.ServiceTypeOrg, slug)
	memberGRN := grn.New("garam", "default", user.ServiceType, "member-sub")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))

	rec := f.do(t, "POST", "/orgs/"+slug+"/invitations", `{"role":"member"}`, member)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = f.do(t, "POST", "/orgs/"+slug+"/invitations", `{"role":"member"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	token := decodeBody[apigen.Invitation](t, rec).Token
	require.NotEmpty(t, token)

	// The invitee accepts and joins with the invitation role.
	invitee := f.sessionCookieFor(t, "invitee-sub", "Invitee")
	rec = f.do(t, "POST", "/invitations/"+token+"/accept", "", invitee)
	require.Equal(t, http.StatusOK, rec.Code)
	m := decodeBody[apigen.Member](t, rec)
	assert.Equal(t, apigen.RoleMember, m.Role)
	assert.Equal(t, "grn:garam:default:user:invitee-sub", m.UserGrn)

	// Used tokens are rejected.
	rec = f.do(t, "POST", "/invitations/"+token+"/accept", "", invitee)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// Unknown tokens 404.
	rec = f.do(t, "POST", "/invitations/nope/accept", "", invitee)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAcceptInvitation_Expired(t *testing.T) {
	f := newFixture(t)
	invitee := f.sessionCookieFor(t, "invitee-sub", "Invitee")

	orgGRN := grn.New("garam", "default", org.ServiceTypeOrg, "acme")
	creator := grn.New("garam", "default", user.ServiceType, "owner-sub")
	expired := org.NewInvitation("expired-tok", orgGRN, org.RoleMember, creator, time.Now().Add(-time.Hour))
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t), expired))

	rec := f.do(t, "POST", "/invitations/expired-tok/accept", "", invitee)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "failed_precondition", decodeBody[apigen.Error](t, rec).Kind)
}
