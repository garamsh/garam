package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	agentmocks "github.com/garamsh/garam/internal/agent/mocks"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/api/mocks"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	operatormocks "github.com/garamsh/garam/internal/operator/mocks"
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
	authorities *fakeAuthorityReader
	authority   *operatormocks.MockAuthorityService
	standalone  *agentmocks.MockStandaloneService
}

// fixtureTokenURL is the token endpoint every fixture is configured
// with, which an issued key's answer carries (ADR-0102).
const fixtureTokenURL = "https://machine.garam.test/oauth/token"

func newFixture(t *testing.T, extra ...authn.Provider) *fixture {
	t.Helper()
	return newFixtureConfigured(t, api.Config{SessionTTL: time.Hour, TokenURL: fixtureTokenURL}, extra...)
}

// newFixtureConfigured wires a Server configured as cfg says.
func newFixtureConfigured(t *testing.T, cfg api.Config, extra ...authn.Provider) *fixture {
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
		authorities: newFakeAuthorityReader(),
		authority:   operatormocks.NewMockAuthorityService(t),
		standalone:  agentmocks.NewMockStandaloneService(t),
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
	srv := api.NewServer(f.users, f.orgs, f.agents, f.operators, f.consents, watches, f.sessions, f.states, f.credentials, f.attempts, append([]authn.Provider{f.provider}, extra...), f.evaluator, f.policies, f.authorities, f.authority, f.standalone, cfg)
	// Wired as `serve api` wires it: a binding failure is refused
	// before any handler runs, so the option is what decides the body.
	f.handler = apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{
		ErrorHandlerFunc: api.BindingError,
	})
	return f
}

// permission pairs a subject with an action the evaluator permits.
type permission struct {
	subject grn.GRN
	action  string
}

// permit stubs the Evaluator seam: only the listed subject × action
// pairs are allowed; every other question is denied.
// A batch is answered per object the same way; a test that lists
// nothing never asks one.
func (f *fixture) permit(perms ...permission) {
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in api.EvalInput) (bool, error) {
			return slices.Contains(perms, permission{in.Subject, in.Action}), nil
		})
	f.evaluator.EXPECT().EvaluateBatch(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in api.BatchInput) ([]bool, error) {
			allowed := make([]bool, len(in.Objects))
			for i := range in.Objects {
				allowed[i] = slices.Contains(perms, permission{in.Subject, in.Action})
			}
			return allowed, nil
		}).Maybe()
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

// TestLogin_UnknownProvider holds the refusal the callback half of this
// flow makes too: a provider nothing registered is not there, and
// `Login` says so. The registered provider beside it is the control —
// the same route under a name the fixture registered redirects, so the
// 404 tracks the provider rather than a surface that refuses every
// login.
func TestLogin_UnknownProvider(t *testing.T) {
	f := newFixture(t)

	rec := f.do(t, "GET", "/auth/login/github", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = f.do(t, "GET", "/auth/login/google", "")
	assert.Equal(t, http.StatusFound, rec.Code)
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
	_, err := f.states.Consume(testCtx(t), spent)
	require.NoError(t, err)
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
			require.NoError(t, f.states.Issue(testCtx(t), returnedState, time.Minute, ""))

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
	require.NoError(t, f.states.Issue(testCtx(t), returnedState, time.Minute, ""))

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
				require.NoError(t, f.states.Issue(testCtx(t), returnedState, tt.ttl, ""))
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
	require.NoError(t, f.states.Issue(testCtx(t), returnedState, time.Minute, ""))
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
	require.NoError(t, f.states.Issue(testCtx(t), returnedState, time.Minute, ""))

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

// TestMe_NoSession reads the request with a session against the same
// request without one: the control is what stops being answered if
// /me starts refusing every caller.
func TestMe_NoSession(t *testing.T) {
	f := newFixture(t)

	rec := f.do(t, "GET", "/me", "", f.sessionCookieFor(t, "ada", "Ada"))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "GET", "/me", "")
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

// reports collects what the handlers write to the default logger, which
// is where a failure a handler decided not to raise is the only
// evidence there is. A handler may still be running on another
// goroutine when a test reads this, so the buffer is guarded.
type reports struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *reports) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *reports) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// captureReports points the default logger at a buffer for the length
// of one test. slog.Default is process-wide, so the previous one is put
// back; nothing in this package runs in parallel.
func captureReports(t *testing.T) *reports {
	t.Helper()
	out := &reports{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return out
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

// TestOrgUpdate_RefusesANameThatIsEmpty covers the check between the
// body and the store. An empty name is well-formed JSON in a
// well-typed field, so nothing upstream refuses it and the store would
// take it: what a caller renaming an organization to nothing gets is
// decided here and nowhere else.
//
// The rename that succeeds is the control, so what the refusal answers
// is the empty name and not a handler refusing every patch.
func TestOrgUpdate_RefusesANameThatIsEmpty(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{grn.New("root", "default", user.ServiceType, "owner-sub"), org.ActionOrgUpdate})

	rec := f.do(t, "PATCH", "/orgs/"+slug, `{"name":""}`, owner)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "invalid_argument", decodeBody[apigen.Error](t, rec).Kind)

	rec = f.do(t, "PATCH", "/orgs/"+slug, `{"name":"acme2"}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "acme2", decodeBody[apigen.Organization](t, rec).Name)
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
	orgGRN := grn.Org(slug)
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
	orgGRN := grn.Org(slug)
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

// TestCreateOrg_RequiresAuth reads the same body under a session
// against the same body without one: the control is what stops being
// created if the route starts refusing every caller, or if the body
// itself is what the 401 is really about.
func TestCreateOrg_RequiresAuth(t *testing.T) {
	f := newFixture(t)

	rec := f.do(t, "POST", "/orgs", `{"name":"acme"}`, f.sessionCookieFor(t, "ada", "Ada"))
	require.Equal(t, http.StatusCreated, rec.Code)

	rec = f.do(t, "POST", "/orgs", `{"name":"acme"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestInvitationFlow_Lifecycle(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	// Members cannot invite; owners (admin+) can.
	member := f.sessionCookieFor(t, "member-sub", "Member")
	orgGRN := grn.Org(slug)
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

	orgGRN := grn.Org("acme")
	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	expired := org.NewInvitation("expired-tok", orgGRN, org.RoleMember, creator, time.Now().Add(-time.Hour))
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t), expired))

	rec := f.do(t, "POST", "/invitations/expired-tok/accept", "", invitee)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "failed_precondition", decodeBody[apigen.Error](t, rec).Kind)
}

// TestListPaging_RefusesAPageTheColumnsCannotHold drives every list
// endpoint with the two values api/openapi.yaml's `minimum: 0` excludes
// and the two the paging columns cannot hold. Each is refused with the
// status the specification now declares, naming the parameter.
//
// The unpaged read is the control, per endpoint. Four 400s prove
// nothing beside a surface that refuses these routes outright, and
// without it the refusal could be the authorization ahead of it rather
// than the paging.
func TestListPaging_RefusesAPageTheColumnsCannotHold(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionGroupRead},
		permission{userGRNOf("owner-sub"), org.ActionProjectRead},
		permission{userGRNOf("owner-sub"), org.ActionAgentRead},
	)

	endpoints := map[string]string{
		"orgs":     "/orgs",
		"groups":   "/orgs/" + slug + "/groups",
		"projects": "/orgs/" + slug + "/projects",
		"agents":   "/orgs/" + slug + "/agents",
	}
	refused := []struct{ name, param, query string }{
		{"negative limit", "limit", "limit=-1"},
		{"negative offset", "offset", "offset=-1"},
		{"limit past the column", "limit", "limit=4294967296"},
		{"offset past the column", "offset", "offset=4294967296"},
	}
	for endpoint, path := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			control := f.do(t, "GET", path, "", owner)
			require.Equal(t, http.StatusOK, control.Code, "the endpoint answers a page unpaged")

			for _, tt := range refused {
				t.Run(tt.name, func(t *testing.T) {
					rec := f.do(t, "GET", path+"?"+tt.query, "", owner)

					require.Equal(t, http.StatusBadRequest, rec.Code)
					body := decodeBody[apigen.Error](t, rec)
					assert.Equal(t, "out_of_range", body.Kind)
					assert.Contains(t, body.Message, tt.param)
				})
			}
		})
	}
}

// TestListPaging_AnswersNoPageTheCallerDidNotAskFor is the defect in
// its own terms. `limit=4294967296` truncated onto the `integer` column
// as 0, which the store reads as "no limit", and `offset=4294967296`
// truncated the same way onto page one — each answered 200 carrying a
// page nobody asked for, which is what makes it worse than an error: a
// caller cannot tell it from the page it asked for.
//
// That no page comes back is asserted by the body decoding as the
// declared Error object: a JSON array does not unmarshal into it, so a
// page would fail that step rather than pass it. What the store does
// with a page it is given is not re-proved here —
// TestOrgStore_ListOrgsPagesEveryOrg covers that against real Postgres,
// and the fake this fixture holds ignores the filter outright.
func TestListPaging_AnswersNoPageTheCallerDidNotAskFor(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	createOrg(t, f, owner, "acme")
	createOrg(t, f, owner, "beta")
	createOrg(t, f, owner, "gamma")

	all := f.do(t, "GET", "/orgs", "", owner)
	require.Equal(t, http.StatusOK, all.Code)
	require.Len(t, decodeBody[[]apigen.Organization](t, all), 3, "control: three orgs to page")

	// The control is the column's own maximum, accepted where the next
	// value up is refused. It is what says the refusals below are the
	// bound rather than a list that has stopped answering, and it is
	// the pair of values a bound off by one would not separate.
	edge := f.do(t, "GET", "/orgs?limit=2147483647&offset=2147483647", "", owner)
	require.Equal(t, http.StatusOK, edge.Code, "control: int32's own maximum is in range")

	for _, query := range []string{"limit=4294967296", "offset=4294967296"} {
		t.Run(query, func(t *testing.T) {
			rec := f.do(t, "GET", "/orgs?"+query, "", owner)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, "out_of_range", decodeBody[apigen.Error](t, rec).Kind)
		})
	}
}
