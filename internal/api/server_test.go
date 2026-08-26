package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	"github.com/garamsh/garam/internal/watch"
)

// fixture wires a Server to in-memory doubles.
type fixture struct {
	handler     http.Handler
	users       *fakeUserStore
	orgs        *fakeOrgStore
	agents      *fakeAgentStore
	operators   *fakeOperatorStore
	consents    *fakeConsentStore
	leases      *fakeWatchLeases
	relay       *fakeTurnEvents
	sessions    *fakeSessionStore
	states      *fakeOAuthStateStore
	credentials *fakePasswordCredentialStore
	attempts    *fakePasswordAttemptStore
	provider    *fakeProvider
	evaluator   *mocks.MockEvaluator
	policies    *mocks.MockPolicyManager
}

func newFixture(t *testing.T, extra ...authn.Provider) *fixture {
	t.Helper()
	f := &fixture{
		users:       newFakeUserStore(),
		orgs:        newFakeOrgStore(),
		agents:      newFakeAgentStore(),
		operators:   newFakeOperatorStore(),
		consents:    newFakeConsentStore(),
		leases:      newFakeWatchLeases(),
		relay:       newFakeTurnEvents(),
		sessions:    newFakeSessionStore(),
		states:      newFakeOAuthStateStore(),
		credentials: newFakePasswordCredentialStore(),
		attempts:    newFakePasswordAttemptStore(),
		provider:    &fakeProvider{name: "google"},
		evaluator:   mocks.NewMockEvaluator(t),
		policies:    mocks.NewMockPolicyManager(t),
	}
	// The events a stream carries were forwarded to another process, so
	// this one delivers what the relay announces to the watches it holds
	// (ADR-0017) — the goroutine `serve api` runs. The lease is short so
	// a test sees what a quiet stream carries without waiting on it.
	watches, err := watch.NewService(f.leases, f.relay, watch.Config{Lease: 300 * time.Millisecond})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = watches.Receive(ctx) }()
	srv := api.NewServer(f.users, f.orgs, f.agents, f.operators, f.consents, watches, f.sessions, f.states, f.credentials, f.attempts, append([]authn.Provider{f.provider}, extra...), f.evaluator, f.policies, api.Config{SessionTTL: time.Hour})
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

// TestOauthCallback_UnknownProvider holds the refusal the other half of
// this flow already makes: a provider nothing registered is not there,
// and the callback says so the way `Login` does. The registered
// provider beside it is the control — the same request, missing the
// same cookies, is refused as unauthenticated instead, so the 404
// tracks the provider rather than a surface that refuses every
// callback.
func TestOauthCallback_UnknownProvider(t *testing.T) {
	f := newFixture(t)

	rec := f.do(t, "GET", "/auth/callback/github?code=ok&state=s", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = f.do(t, "GET", "/auth/callback/google?code=ok&state=s", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"past the provider check a callback carrying no state is refused for another reason")
}

// TestLogin_ShedsLoadPastTheLiveStateBound drives the endpoint at the
// rate the code before the bound accepted: one request, one write,
// nothing in the way. Every login up to MaxLiveOAuthStates redirects;
// the one past the bound is refused with 429 rather than written. A
// spent state then admits one more — the control: the refusal tracks
// the states still live, not the number of requests the endpoint has
// seen, which would refuse this one too.
func TestLogin_ShedsLoadPastTheLiveStateBound(t *testing.T) {
	f := newFixture(t)
	for range authn.MaxLiveOAuthStates {
		rec := f.do(t, "GET", "/auth/login/google", "")
		require.Equal(t, http.StatusFound, rec.Code)
	}

	rec := f.do(t, "GET", "/auth/login/google", "")
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)

	// A completed callback spends its state; stand in for it.
	var spent string
	for spent = range f.states.expiryOf {
		break
	}
	require.NoError(t, f.states.Consume(testCtx(t), spent))
	rec = f.do(t, "GET", "/auth/login/google", "")
	assert.Equal(t, http.StatusFound, rec.Code)
}

func TestListProviders_ReflectsRegistration(t *testing.T) {
	// A provider the server was never given is absent from the list;
	// the one it was given is the control, so the absence is
	// registration and not an endpoint that names nothing.
	f := newFixture(t)
	rec := f.do(t, "GET", "/auth/providers", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"google"}, decodeBody[[]string](t, rec))

	f = newFixture(t, &fakeProvider{name: "second"})
	rec = f.do(t, "GET", "/auth/providers", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, []string{"google", "second"}, decodeBody[[]string](t, rec))
}

// oauthStateCookie is the state cookie the callback compares against.
func oauthStateCookie(value string) *http.Cookie {
	return &http.Cookie{Name: "garam_oauth_state", Value: value}
}

// oauthVerifierCookie is the PKCE verifier cookie the callback requires.
func oauthVerifierCookie(value string) *http.Cookie {
	return &http.Cookie{Name: "garam_oauth_verifier", Value: value}
}

// callbackFixture wires a fixture whose provider returns an identity,
// so a callback that clears both guards reaches a session.
func callbackFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.provider.identity = &authn.Identity{Subject: "123", Nickname: "Ada"}
	return f
}

func TestOauthCallback_StateAndVerifierGuards(t *testing.T) {
	// The state the provider hands back in the callback URL. Every
	// case below sends it; only the cookies differ.
	const returnedState = "state-abc"
	const verifier = "verifier-xyz"

	// The accepted case is the control: every refusal below is that
	// same request with one cookie changed, so removing a guard turns
	// its refusal into the control's 302 instead of leaving a 401 that
	// arrives for another reason. Both refusals answer
	// `unauthenticated`, so the message is what separates the state
	// guard from the verifier guard.
	tests := []struct {
		name        string
		cookies     []*http.Cookie
		wantStatus  int
		wantMessage string
	}{
		{
			name:       "matching state and a present verifier",
			cookies:    []*http.Cookie{oauthStateCookie(returnedState), oauthVerifierCookie(verifier)},
			wantStatus: http.StatusFound,
		},
		{
			name:        "state cookie holds a different value",
			cookies:     []*http.Cookie{oauthStateCookie("state-zzz"), oauthVerifierCookie(verifier)},
			wantStatus:  http.StatusUnauthorized,
			wantMessage: "oauth state mismatch",
		},
		{
			name:        "state cookie is empty",
			cookies:     []*http.Cookie{oauthStateCookie(""), oauthVerifierCookie(verifier)},
			wantStatus:  http.StatusUnauthorized,
			wantMessage: "oauth state mismatch",
		},
		{
			name:        "state cookie is absent",
			cookies:     []*http.Cookie{oauthVerifierCookie(verifier)},
			wantStatus:  http.StatusUnauthorized,
			wantMessage: "oauth state mismatch",
		},
		{
			name:        "verifier cookie is absent",
			cookies:     []*http.Cookie{oauthStateCookie(returnedState)},
			wantStatus:  http.StatusUnauthorized,
			wantMessage: "missing pkce verifier",
		},
		{
			name:        "verifier cookie is empty",
			cookies:     []*http.Cookie{oauthStateCookie(returnedState), oauthVerifierCookie("")},
			wantStatus:  http.StatusUnauthorized,
			wantMessage: "missing pkce verifier",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := callbackFixture(t)
			// The login that started this flow issued the state the
			// query carries; only the cookies differ between cases.
			require.NoError(t, f.states.Issue(testCtx(t), returnedState, time.Minute))

			rec := f.do(t, "GET", "/auth/callback/google?code=ok&state="+returnedState, "", tt.cookies...)

			require.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusFound {
				assert.Equal(t, "/console", rec.Header().Get("Location"))
				return
			}
			body := decodeBody[apigen.Error](t, rec)
			assert.Equal(t, "unauthenticated", body.Kind)
			assert.Equal(t, tt.wantMessage, body.Message)
		})
	}
}

// sessionCookieIn returns the session cookie the response sets, or nil
// when it sets none.
func sessionCookieIn(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "garam_session" {
			return c
		}
	}
	return nil
}

// callback issues the callback a login's redirect would produce for
// state, carrying the cookies that login set.
func (f *fixture) callback(t *testing.T, state string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, "GET", "/auth/callback/google?code=ok&state="+state, "",
		oauthStateCookie(state), oauthVerifierCookie("verifier-xyz"))
}

// TestOauthCallback_StateIsSingleUse is the replay: the same callback
// request twice, cookies included, as anyone holding a captured one
// would send it. Expiring the cookies asks the browser to drop them
// and binds nobody else, so the first attempt is the control — the
// second is refused because the state was spent, not because the
// request differs from one that worked.
func TestOauthCallback_StateIsSingleUse(t *testing.T) {
	const returnedState = "state-abc"
	f := callbackFixture(t)
	require.NoError(t, f.states.Issue(testCtx(t), returnedState, time.Minute))

	rec := f.callback(t, returnedState)
	require.Equal(t, http.StatusFound, rec.Code)
	require.NotNil(t, sessionCookieIn(rec), "session cookie set")

	rec = f.callback(t, returnedState)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	body := decodeBody[apigen.Error](t, rec)
	assert.Equal(t, "unauthenticated", body.Kind)
	assert.Equal(t, "oauth state not live", body.Message)
	assert.Nil(t, sessionCookieIn(rec), "no session for the replay")
}

// TestOauthCallback_StateMustBeLive covers the two states a callback
// meets that no replay produced: one no login issued — a code with a
// state of the sender's own choosing — and one whose flow has run out.
// The accepted case is the control, and its message separates this
// refusal from the state guard's.
func TestOauthCallback_StateMustBeLive(t *testing.T) {
	const returnedState = "state-abc"
	tests := []struct {
		name       string
		issue      bool
		ttl        time.Duration
		wantStatus int
	}{
		{
			name:       "a state a login issued and nothing has spent",
			issue:      true,
			ttl:        time.Minute,
			wantStatus: http.StatusFound,
		},
		{
			name:       "a state no login issued",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "a state whose flow has run out",
			issue:      true,
			ttl:        -time.Minute,
			wantStatus: http.StatusUnauthorized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := callbackFixture(t)
			if tt.issue {
				require.NoError(t, f.states.Issue(testCtx(t), returnedState, tt.ttl))
			}

			rec := f.callback(t, returnedState)

			require.Equal(t, tt.wantStatus, rec.Code)
			if tt.wantStatus == http.StatusFound {
				assert.NotNil(t, sessionCookieIn(rec), "session cookie set")
				return
			}
			body := decodeBody[apigen.Error](t, rec)
			assert.Equal(t, "unauthenticated", body.Kind)
			assert.Equal(t, "oauth state not live", body.Message)
			assert.Nil(t, sessionCookieIn(rec), "no session")
		})
	}
}

// TestOauthCallback_RefusedExchangeLeavesStateLive holds the decision
// that a failed callback spends nothing: the state is spent where a
// session is about to exist, not where a request arrives, so a probe
// that cannot pass the provider costs the browser its sign-in.
func TestOauthCallback_RefusedExchangeLeavesStateLive(t *testing.T) {
	const returnedState = "state-abc"
	f := callbackFixture(t)
	require.NoError(t, f.states.Issue(testCtx(t), returnedState, time.Minute))
	f.provider.identity, f.provider.err = nil, errorx.New(errorx.KindUnauthenticated, "token exchange")

	rec := f.callback(t, returnedState)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "token exchange", decodeBody[apigen.Error](t, rec).Message)

	// The control: the browser that owns the state completes with it
	// afterwards, which it cannot do if the refusal spent it.
	f.provider.identity, f.provider.err = &authn.Identity{Subject: "123", Nickname: "Ada"}, nil

	rec = f.callback(t, returnedState)

	require.Equal(t, http.StatusFound, rec.Code)
	assert.NotNil(t, sessionCookieIn(rec), "session cookie set")
}

func TestOauthCallback_ExpiresSingleUseCookies(t *testing.T) {
	const returnedState = "state-abc"
	f := callbackFixture(t)
	require.NoError(t, f.states.Issue(testCtx(t), returnedState, time.Minute))

	rec := f.do(t, "GET", "/auth/callback/google?code=ok&state="+returnedState, "",
		oauthStateCookie(returnedState), oauthVerifierCookie("verifier-xyz"))
	require.Equal(t, http.StatusFound, rec.Code)

	set := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		set[c.Name] = c
	}
	require.NotNil(t, set["garam_session"], "session cookie set")

	// Expiry is an instruction to the browser, so the property is a
	// Set-Cookie carrying a negative MaxAge — not the absence of one.
	for _, name := range []string{"garam_oauth_state", "garam_oauth_verifier"} {
		c := set[name]
		require.NotNil(t, c, "%s cleared", name)
		assert.Negative(t, c.MaxAge, "%s cleared", name)
		assert.Empty(t, c.Value, "%s cleared", name)
	}
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
