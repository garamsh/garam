package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/api/mocks"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// fixture wires a Server to in-memory doubles.
type fixture struct {
	handler   http.Handler
	users     *fakeUserStore
	orgs      *fakeOrgStore
	sessions  *fakeSessionStore
	provider  *fakeProvider
	evaluator *mocks.MockEvaluator
	policies  *mocks.MockPolicyManager
}

func newFixture(t *testing.T, extra ...authn.Provider) *fixture {
	t.Helper()
	f := &fixture{
		users:     newFakeUserStore(),
		orgs:      newFakeOrgStore(),
		sessions:  newFakeSessionStore(),
		provider:  &fakeProvider{name: "google"},
		evaluator: mocks.NewMockEvaluator(t),
		policies:  mocks.NewMockPolicyManager(t),
	}
	srv := api.NewServer(f.users, f.orgs, f.sessions, append([]authn.Provider{f.provider}, extra...), f.evaluator, f.policies, api.Config{SessionTTL: time.Hour})
	f.handler = apigen.Handler(srv)
	return f
}

// permission pairs a subject with an action the evaluator permits.
type permission struct {
	subject grn.GRN
	action  string
}

// permit stubs the Evaluator seam: only the listed subject × action
// pairs are allowed; every other question is denied.
func (f *fixture) permit(perms ...permission) {
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in api.EvalInput) (bool, error) {
			return slices.Contains(perms, permission{in.Subject, in.Action}), nil
		})
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
	g := grn.New("root", "default", user.ServiceType, subject)
	require.NoError(t, f.users.Create(testCtx(t), user.New(g, nickname)))
	token, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, f.sessions.Create(testCtx(t), token, authn.NewSession(g, time.Hour)))
	return &http.Cookie{Name: "garam_session", Value: token}
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
		case "garam_oauth_state":
			state = true
		case "garam_oauth_verifier":
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

func TestListProviders_ReflectsRegistration(t *testing.T) {
	// The dev provider is absent unless registered (GARAM_DEV_AUTH).
	f := newFixture(t)
	rec := f.do(t, "GET", "/auth/providers", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"google"}, decodeBody[[]string](t, rec))

	dev, err := authn.NewDevProvider(authn.DevConfig{RedirectURL: "http://web.test/api/auth/callback/dev"})
	require.NoError(t, err)
	f = newFixture(t, dev)
	rec = f.do(t, "GET", "/auth/providers", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"dev", "google"}, decodeBody[[]string](t, rec))
}

func TestDevLogin_RoundTrip(t *testing.T) {
	dev, err := authn.NewDevProvider(authn.DevConfig{RedirectURL: "http://web.test/api/auth/callback/dev"})
	require.NoError(t, err)
	f := newFixture(t, dev)

	// Login redirects to the provider's own callback with code and
	// state filled in.
	rec := f.do(t, "GET", "/auth/login/dev", "")
	require.Equal(t, http.StatusFound, rec.Code)
	loc, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "http://web.test/api/auth/callback/dev", loc.Scheme+"://"+loc.Host+loc.Path)
	state := loc.Query().Get("state")
	require.NotEmpty(t, state)
	var cookies []*http.Cookie
	var stateCookieFound bool
	for _, c := range rec.Result().Cookies() {
		cookies = append(cookies, c)
		if c.Name == "garam_oauth_state" {
			stateCookieFound = true
			assert.Equal(t, state, c.Value)
		}
	}
	assert.True(t, stateCookieFound, "state cookie set")

	// The callback upserts the dev user and issues a real session.
	rec = f.do(t, "GET", "/auth/callback/dev?"+loc.Query().Encode(), "", cookies...)
	require.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "/console", rec.Header().Get("Location"))
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "garam_session" {
			session = c
		}
	}
	require.NotNil(t, session, "session cookie set")

	rec = f.do(t, "GET", "/me", "", session)
	require.Equal(t, http.StatusOK, rec.Code)
	me := decodeBody[apigen.User](t, rec)
	assert.Equal(t, "Dev", me.Nickname)
	assert.Equal(t, "grn:root:default:user:dev", me.Grn)
}

func TestDevLogin_NotRegistered(t *testing.T) {
	// Without GARAM_DEV_AUTH the dev routes answer like any unknown
	// provider.
	f := newFixture(t)
	assert.Equal(t, http.StatusNotFound, f.do(t, "GET", "/auth/login/dev", "").Code)
	assert.Equal(t, http.StatusNotFound, f.do(t, "GET", "/auth/callback/dev?code=dev&state=x", "").Code)
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
		if c.Name == "garam_oauth_state" {
			state = c.Value
		}
	}
	require.NotEmpty(t, state)

	// Callback issues a session cookie and upserts the user.
	rec = f.do(t, "GET", "/auth/callback/google?code=ok&state="+state, "", cookies...)
	require.Equal(t, http.StatusFound, rec.Code)
	var session *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "garam_session" {
			session = c
		}
	}
	require.NotNil(t, session, "session cookie set")

	// Me returns the upserted user.
	rec = f.do(t, "GET", "/me", "", session)
	require.Equal(t, http.StatusOK, rec.Code)
	me := decodeBody[apigen.User](t, rec)
	assert.Equal(t, "Ada", me.Nickname)
	assert.Equal(t, "grn:root:default:user:123", me.Grn)

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

func TestPrincipal_RecordedOnlyWhenAuthenticated(t *testing.T) {
	tests := []struct {
		name   string
		signIn bool
		want   string
	}{
		{name: "signed in", signIn: true, want: "grn:root:default:user:ada"},
		{name: "anonymous"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			req := httptest.NewRequest("GET", "/me", nil)
			if tt.signIn {
				req.AddCookie(f.sessionCookieFor(t, "ada", "Ada"))
			}

			var principal api.Principal
			f.handler.ServeHTTP(httptest.NewRecorder(), api.WithPrincipal(req, &principal))

			assert.Equal(t, tt.want, principal.GRN)
		})
	}
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
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	memberGRN := grn.New("root", "default", user.ServiceType, "member-sub")
	f.permit(
		permission{ownerGRN, org.ActionOrgRead},
		permission{ownerGRN, org.ActionOrgUpdate},
		permission{ownerGRN, org.ActionOrgDelete},
		permission{memberGRN, org.ActionOrgRead},
		permission{memberGRN, org.ActionOrgMemberList},
	)

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
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)
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

func TestListOrgMembers_Nicknames(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	adaGRN := grn.New("root", "default", user.ServiceType, "ada-sub")
	f.sessionCookieFor(t, "ada-sub", "Ada")
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, adaGRN, org.RoleMember)))
	f.permit(permission{ownerGRN, org.ActionOrgMemberList})

	nicknames := func(t *testing.T) map[string]*string {
		t.Helper()
		rec := f.do(t, "GET", "/orgs/"+slug+"/members", "", owner)
		require.Equal(t, http.StatusOK, rec.Code)
		members := decodeBody[[]apigen.Member](t, rec)
		require.Len(t, members, 2)
		byGRN := make(map[string]*string, len(members))
		for _, m := range members {
			byGRN[m.UserGrn] = m.Nickname
		}
		return byGRN
	}

	got := nicknames(t)
	require.NotNil(t, got[ownerGRN.String()])
	assert.Equal(t, "Owner", *got[ownerGRN.String()])
	require.NotNil(t, got[adaGRN.String()])
	assert.Equal(t, "Ada", *got[adaGRN.String()])

	// The membership read and the name read are separate queries. A
	// user deleted between them resolves to no nickname; the listing
	// still succeeds, and the GRN is not offered as a substitute.
	f.users.remove(adaGRN)
	got = nicknames(t)
	assert.Nil(t, got[adaGRN.String()])
	require.NotNil(t, got[ownerGRN.String()])
	assert.Equal(t, "Owner", *got[ownerGRN.String()])
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
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)
	memberGRN := grn.New("root", "default", user.ServiceType, "member-sub")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))
	f.permit(permission{grn.New("root", "default", user.ServiceType, "owner-sub"), org.ActionOrgInvite})

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
	assert.Equal(t, "grn:root:default:user:invitee-sub", m.UserGrn)
	require.NotNil(t, m.Nickname)
	assert.Equal(t, "Invitee", *m.Nickname)

	// Used tokens are rejected.
	rec = f.do(t, "POST", "/invitations/"+token+"/accept", "", invitee)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// Unknown tokens 404.
	rec = f.do(t, "POST", "/invitations/nope/accept", "", invitee)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateInvitation_DefaultsExpiry(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{grn.New("root", "default", user.ServiceType, "owner-sub"), org.ActionOrgInvite})

	rec := f.do(t, "POST", "/orgs/"+slug+"/invitations", `{"role":"member"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	inv := decodeBody[apigen.Invitation](t, rec)
	assert.WithinDuration(t, time.Now().UTC().Add(7*24*time.Hour), inv.ExpiresAt, time.Minute)
}

func TestAcceptInvitation_Expired(t *testing.T) {
	f := newFixture(t)
	invitee := f.sessionCookieFor(t, "invitee-sub", "Invitee")

	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, "acme")
	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	expired := org.NewInvitation("expired-tok", orgGRN, org.RoleMember, creator, time.Now().Add(-time.Hour))
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t), expired))

	rec := f.do(t, "POST", "/invitations/expired-tok/accept", "", invitee)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "failed_precondition", decodeBody[apigen.Error](t, rec).Kind)
}
