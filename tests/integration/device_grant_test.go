package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
)

// deviceStack is the browser API over real Postgres and a real policy
// engine, with the device grant wired as serve api wires it. ada owns
// acme and signs in through the browser; eve is a second user whose
// sessions ada's requests must not reach.
type deviceStack struct {
	url      string
	pool     *pgxpool.Pool
	acme     grn.GRN
	ada, eve *http.Cookie
	adaGRN   grn.GRN
}

func newDeviceStack(t *testing.T) *deviceStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	truncate(t, p)
	_, engine := policyEngine(t, p)
	orgs := orgpg.New(p)
	users := userpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	sessions := authn.NewPostgresSessionStore(p)
	session := func(id string) (*http.Cookie, grn.GRN) {
		g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
		require.NoError(t, users.Create(ctx, user.New(g, id)))
		require.NoError(t, orgs.AddMember(ctx, org.NewMembership(acme, g, org.RoleOwner)))
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, sessions.Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}, g
	}
	s := &deviceStack{pool: p, acme: acme}
	s.ada, s.adaGRN = session("ada")
	s.eve, _ = session("eve")

	srv := api.NewServer(users, orgs, agent.NewPostgresStore(p), operator.NewPostgresStore(p), message.NewPostgresStore(p), nil,
		sessions, nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), nil, nil, cli.NewAuthorityService(p, engine), nil,
		api.Config{SessionTTL: time.Hour}).WithDeviceGrant(authn.NewPostgresDeviceStore(p))
	handler := apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError})
	// The CLI reaches the API behind the console's /api rewrite, so the
	// server answers there as the deployed one does.
	browser := httptest.NewServer(http.StripPrefix("/api", handler))
	t.Cleanup(browser.Close)
	s.url = browser.URL
	return s
}

// call makes one request under /api, with the session a cookie or a
// bearer presents, and answers the status and body.
func (s *deviceStack) call(t *testing.T, method, path string, body io.Reader, contentType string, present func(*http.Request)) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.url+"/api"+path, body)
	require.NoError(t, err)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if present != nil {
		present(req)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

func sessionCookie(c *http.Cookie) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(c) }
}

func sessionBearer(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func (s *deviceStack) form(t *testing.T, path string, values url.Values) (int, string) {
	t.Helper()
	return s.call(t, http.MethodPost, path, strings.NewReader(values.Encode()), "application/x-www-form-urlencoded", nil)
}

// issue requests a device code and answers it.
func (s *deviceStack) issue(t *testing.T) apigen.DeviceCode {
	t.Helper()
	status, body := s.form(t, "/auth/device", url.Values{"client_id": {"garam-cli"}, "client_name": {"laptop"}})
	require.Equal(t, http.StatusOK, status, body)
	var code apigen.DeviceCode
	require.NoError(t, json.Unmarshal([]byte(body), &code))
	return code
}

// poll asks the token endpoint once for code.
func (s *deviceStack) poll(t *testing.T, code apigen.DeviceCode) (int, string) {
	t.Helper()
	return s.form(t, "/auth/device/token", url.Values{
		"client_id": {"garam-cli"}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {code.DeviceCode},
	})
}

// letIntervalPass moves every authorization's last poll an hour back,
// so the next poll is not early and is answered for its decision.
func (s *deviceStack) letIntervalPass(t *testing.T) {
	t.Helper()
	_, err := s.pool.Exec(context.Background(),
		"UPDATE device_authorizations SET last_polled_at = last_polled_at - interval '1 hour' WHERE last_polled_at IS NOT NULL")
	require.NoError(t, err)
}

func (s *deviceStack) decide(t *testing.T, as *http.Cookie, userCode string, approve bool) (int, string) {
	t.Helper()
	raw, err := json.Marshal(apigen.DeviceDecision{UserCode: userCode, Approve: approve})
	require.NoError(t, err)
	return s.call(t, http.MethodPost, "/auth/device/approval", strings.NewReader(string(raw)), "application/json", sessionCookie(as))
}

func oauthError(t *testing.T, body string) apigen.OAuthError {
	t.Helper()
	var refusal apigen.OAuthError
	require.NoError(t, json.Unmarshal([]byte(body), &refusal), body)
	return refusal
}

func sessionToken(t *testing.T, body string) apigen.SessionToken {
	t.Helper()
	var tokens apigen.SessionToken
	require.NoError(t, json.Unmarshal([]byte(body), &tokens), body)
	return tokens
}

// TestDeviceGrant_AnApprovedCodeIsRedeemedOnceForASessionTheBearerPresents
// walks the grant end to end: pending until ada approves, then one
// session, whose access token is accepted where her cookie is, and a
// second redemption of the same code refused.
func TestDeviceGrant_AnApprovedCodeIsRedeemedOnceForASessionTheBearerPresents(t *testing.T) {
	s := newDeviceStack(t)
	code := s.issue(t)
	assert.Equal(t, "/device", code.VerificationUri)
	assert.Equal(t, "/device?code="+url.QueryEscape(code.UserCode), code.VerificationUriComplete)
	assert.Equal(t, 600, code.ExpiresIn)
	assert.Equal(t, 5, code.Interval)
	assert.Regexp(t, `^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`, code.UserCode)

	status, body := s.poll(t, code)
	require.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, apigen.AuthorizationPending, oauthError(t, body).Error)

	// A typed code reads the same in any case and without its dash.
	typed := strings.ToLower(strings.ReplaceAll(code.UserCode, "-", ""))
	status, body = s.decide(t, s.ada, typed, true)
	require.Equal(t, http.StatusNoContent, status, body)

	s.letIntervalPass(t)
	status, body = s.poll(t, code)
	require.Equal(t, http.StatusOK, status, body)
	tokens := sessionToken(t, body)
	assert.Equal(t, apigen.Bearer, tokens.TokenType)
	assert.Equal(t, 3600, tokens.ExpiresIn)
	assert.Equal(t, 30*24*3600, tokens.RefreshTokenExpiresIn)
	assert.Equal(t, 1, countRows(t, s.pool,
		"SELECT count(*) FROM sessions WHERE kind = 'cli' AND user_grn = $1 AND name = 'laptop'", s.adaGRN.String()))
	assert.Equal(t, 0, countRows(t, s.pool, "SELECT count(*) FROM device_authorizations"),
		"the redeemed authorization is spent")

	status, body = s.call(t, http.MethodGet, "/me", nil, "", sessionBearer(tokens.AccessToken))
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, s.adaGRN.String())
	status, body = s.call(t, http.MethodGet, "/orgs/acme/agents", nil, "", sessionBearer(tokens.AccessToken))
	assert.Equal(t, http.StatusOK, status, "an org route accepts the bearer as it accepts the cookie: %s", body)

	s.letIntervalPass(t)
	status, body = s.poll(t, code)
	require.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, apigen.InvalidGrant, oauthError(t, body).Error, "a code is redeemed once")
	assert.Equal(t, 1, countRows(t, s.pool, "SELECT count(*) FROM sessions WHERE kind = 'cli'"))
}

// TestDeviceGrant_ADeniedCodeAnswersAccessDenied: a denial opens no
// session. The control is the same flow approved, which does.
func TestDeviceGrant_ADeniedCodeAnswersAccessDenied(t *testing.T) {
	s := newDeviceStack(t)
	denied, approved := s.issue(t), s.issue(t)
	status, body := s.decide(t, s.ada, denied.UserCode, false)
	require.Equal(t, http.StatusNoContent, status, body)
	status, body = s.decide(t, s.ada, approved.UserCode, true)
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = s.poll(t, denied)
	require.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, apigen.AccessDenied, oauthError(t, body).Error)
	status, body = s.poll(t, approved)
	assert.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, 1, countRows(t, s.pool, "SELECT count(*) FROM sessions WHERE kind = 'cli'"))

	status, body = s.decide(t, s.ada, denied.UserCode, true)
	assert.Equal(t, http.StatusNotFound, status, "a decided code is not decided again: %s", body)
}

// lapse moves code's expiry into the past and checks it moved that row
// alone.
func (s *deviceStack) lapse(t *testing.T, code apigen.DeviceCode) {
	t.Helper()
	n, err := s.pool.Exec(context.Background(),
		"UPDATE device_authorizations SET expires_at = now() - interval '1 second' WHERE device_code_hash = $1",
		authn.HashToken(code.DeviceCode))
	require.NoError(t, err)
	require.EqualValues(t, 1, n.RowsAffected(), "the setup lapsed the one code it named")
}

// TestDeviceGrant_ALapsedCodeAnswersExpiredTokenEvenOnceApproved: a
// code approved in time but polled after it lapsed opens no session.
// The control is the code beside it, approved and polled in time.
func TestDeviceGrant_ALapsedCodeAnswersExpiredTokenEvenOnceApproved(t *testing.T) {
	s := newDeviceStack(t)
	lapsed, live := s.issue(t), s.issue(t)
	for _, code := range []apigen.DeviceCode{lapsed, live} {
		status, body := s.decide(t, s.ada, code.UserCode, true)
		require.Equal(t, http.StatusNoContent, status, body)
	}
	s.lapse(t, lapsed)

	status, body := s.poll(t, lapsed)
	require.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, apigen.ExpiredToken, oauthError(t, body).Error)
	status, body = s.poll(t, live)
	assert.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, 1, countRows(t, s.pool, "SELECT count(*) FROM sessions WHERE kind = 'cli'"))
}

// TestDeviceGrant_ALapsedCodeCannotBeApproved: the approval page cannot
// decide a code past its expiry. A live code beside it is the control.
func TestDeviceGrant_ALapsedCodeCannotBeApproved(t *testing.T) {
	s := newDeviceStack(t)
	lapsed, live := s.issue(t), s.issue(t)
	s.lapse(t, lapsed)
	status, body := s.decide(t, s.ada, lapsed.UserCode, true)
	assert.Equal(t, http.StatusNotFound, status, body)
	status, body = s.decide(t, s.ada, live.UserCode, true)
	assert.Equal(t, http.StatusNoContent, status, body)
}

// TestDeviceGrant_APollEarlierThanTheIntervalAnswersSlowDownAndMovesIt:
// the second of two back-to-back polls is early, and the interval it
// answers is five seconds later. Once the interval has passed, the same
// code is answered for its state again, which is the control.
func TestDeviceGrant_APollEarlierThanTheIntervalAnswersSlowDownAndMovesIt(t *testing.T) {
	s := newDeviceStack(t)
	code := s.issue(t)
	status, body := s.poll(t, code)
	require.Equal(t, http.StatusBadRequest, status, body)
	require.Equal(t, apigen.AuthorizationPending, oauthError(t, body).Error)

	status, body = s.poll(t, code)
	require.Equal(t, http.StatusBadRequest, status, body)
	refusal := oauthError(t, body)
	assert.Equal(t, apigen.SlowDown, refusal.Error)
	require.NotNil(t, refusal.Interval)
	assert.Equal(t, 10, *refusal.Interval)
	assert.Equal(t, 1, countRows(t, s.pool, "SELECT count(*) FROM device_authorizations WHERE interval_seconds = 10"))

	s.letIntervalPass(t)
	status, body = s.poll(t, code)
	require.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, apigen.AuthorizationPending, oauthError(t, body).Error)
}

// TestDeviceGrant_IssuingRefusesPastTheLiveBound: with the bound's
// worth of live codes held, one more is refused; once one has lapsed,
// the next is issued again.
func TestDeviceGrant_IssuingRefusesPastTheLiveBound(t *testing.T) {
	s := newDeviceStack(t)
	_, err := s.pool.Exec(context.Background(), `
		INSERT INTO device_authorizations (device_code_hash, user_code_hash, interval_seconds, created_at, expires_at)
		SELECT 'd' || i, 'u' || i, 5, now(), now() + interval '10 minutes' FROM generate_series(1, $1) AS i`,
		authn.MaxLiveDeviceAuthorizations)
	require.NoError(t, err)
	require.Equal(t, authn.MaxLiveDeviceAuthorizations, countRows(t, s.pool, "SELECT count(*) FROM device_authorizations"))

	status, body := s.form(t, "/auth/device", url.Values{"client_id": {"garam-cli"}})
	assert.Equal(t, http.StatusTooManyRequests, status, body)

	_, err = s.pool.Exec(context.Background(),
		"UPDATE device_authorizations SET expires_at = now() - interval '1 second' WHERE device_code_hash = 'd1'")
	require.NoError(t, err)
	s.issue(t)
}

// approvedTokens runs the grant to a session for ada.
func (s *deviceStack) approvedTokens(t *testing.T) apigen.SessionToken {
	t.Helper()
	code := s.issue(t)
	status, body := s.decide(t, s.ada, code.UserCode, true)
	require.Equal(t, http.StatusNoContent, status, body)
	status, body = s.poll(t, code)
	require.Equal(t, http.StatusOK, status, body)
	return sessionToken(t, body)
}

func (s *deviceStack) refresh(t *testing.T, refresh string) (int, string) {
	t.Helper()
	return s.form(t, "/auth/device/token", url.Values{
		"client_id": {"garam-cli"}, "grant_type": {"refresh_token"}, "refresh_token": {refresh},
	})
}

// TestDeviceGrant_ARefreshReplacesBothTokensAndIsSpentOnce: the pair a
// refresh answers works, and the pair it replaced does not.
func TestDeviceGrant_ARefreshReplacesBothTokensAndIsSpentOnce(t *testing.T) {
	s := newDeviceStack(t)
	first := s.approvedTokens(t)

	status, body := s.refresh(t, first.RefreshToken)
	require.Equal(t, http.StatusOK, status, body)
	second := sessionToken(t, body)
	assert.NotEqual(t, first.AccessToken, second.AccessToken)
	assert.NotEqual(t, first.RefreshToken, second.RefreshToken)

	status, body = s.call(t, http.MethodGet, "/me", nil, "", sessionBearer(second.AccessToken))
	assert.Equal(t, http.StatusOK, status, "the new access token is live: %s", body)
	status, body = s.call(t, http.MethodGet, "/me", nil, "", sessionBearer(first.AccessToken))
	assert.Equal(t, http.StatusUnauthorized, status, "the replaced one is not: %s", body)

	status, body = s.refresh(t, first.RefreshToken)
	require.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, apigen.InvalidGrant, oauthError(t, body).Error, "a refresh token is spent once")
	assert.Equal(t, 1, countRows(t, s.pool, "SELECT count(*) FROM sessions WHERE kind = 'cli'"),
		"a refresh replaces the session's tokens rather than adding a session")

	// A browser sign-in prunes lapsed sessions; a CLI session whose
	// access token lapsed but whose refresh token is live is not one.
	_, err := s.pool.Exec(context.Background(), "UPDATE sessions SET expires_at = now() - interval '1 second' WHERE kind = 'cli'")
	require.NoError(t, err)
	token, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, authn.NewPostgresSessionStore(s.pool).Create(context.Background(), token, authn.NewSession(s.adaGRN, time.Hour)))
	status, body = s.refresh(t, second.RefreshToken)
	require.Equal(t, http.StatusOK, status, "the prune spared it: %s", body)
	third := sessionToken(t, body)

	_, err = s.pool.Exec(context.Background(), "UPDATE sessions SET refresh_expires_at = now() - interval '1 second' WHERE kind = 'cli'")
	require.NoError(t, err)
	status, body = s.refresh(t, third.RefreshToken)
	require.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, apigen.InvalidGrant, oauthError(t, body).Error, "a lapsed refresh token buys nothing")
}

// TestBearer_AnAuthorizationHeaderIsReadAloneWhereItIsPresent: a
// malformed or dead bearer is not rescued by a live cookie beside it.
// The cookie alone is the control.
func TestBearer_AnAuthorizationHeaderIsReadAloneWhereItIsPresent(t *testing.T) {
	s := newDeviceStack(t)
	status, body := s.call(t, http.MethodGet, "/me", nil, "", sessionCookie(s.ada))
	require.Equal(t, http.StatusOK, status, body)
	for _, header := range []string{"Basic " + s.ada.Value, "Bearer", "Bearer not-a-session"} {
		status, body = s.call(t, http.MethodGet, "/me", nil, "", func(r *http.Request) {
			r.AddCookie(s.ada)
			r.Header.Set("Authorization", header)
		})
		assert.Equal(t, http.StatusUnauthorized, status, "%q beside a live cookie: %s", strings.SplitN(header, " ", 2)[0], body)
	}
}

// sessions lists the sessions present presents.
func (s *deviceStack) sessions(t *testing.T, present func(*http.Request)) []apigen.Session {
	t.Helper()
	status, body := s.call(t, http.MethodGet, "/me/sessions", nil, "", present)
	require.Equal(t, http.StatusOK, status, body)
	var list []apigen.Session
	require.NoError(t, json.Unmarshal([]byte(body), &list))
	return list
}

// TestSessions_APersonListsAndRevokesTheirOwnSessionsAlone: ada sees
// her browser and CLI sessions, marks the one she is on, and revoking
// the CLI's ends its bearer. eve's revoke of ada's session finds
// nothing, and ada's revoke of the same is the control.
func TestSessions_APersonListsAndRevokesTheirOwnSessionsAlone(t *testing.T) {
	s := newDeviceStack(t)
	tokens := s.approvedTokens(t)

	list := s.sessions(t, sessionCookie(s.ada))
	require.Len(t, list, 2)
	var cliSession apigen.Session
	for _, sess := range list {
		if sess.Kind == apigen.SessionKind("cli") {
			cliSession = sess
		}
	}
	require.NotEmpty(t, cliSession.Id)
	require.NotNil(t, cliSession.Name)
	assert.Equal(t, "laptop", *cliSession.Name)
	assert.False(t, cliSession.Current, "listed on the cookie, the CLI's is not the current one")
	assert.True(t, cliSession.ExpiresAt.After(time.Now().Add(29*24*time.Hour)), "a CLI session lives as long as its refresh token")
	for _, sess := range s.sessions(t, sessionBearer(tokens.AccessToken)) {
		assert.Equal(t, sess.Id == cliSession.Id, sess.Current, "listed on the bearer, the CLI's is")
	}
	assert.Len(t, s.sessions(t, sessionCookie(s.eve)), 1, "eve's list holds her browser session alone")

	status, body := s.call(t, http.MethodDelete, "/me/sessions/"+cliSession.Id, nil, "", sessionCookie(s.eve))
	assert.Equal(t, http.StatusNotFound, status, "eve revokes nothing of ada's: %s", body)
	status, body = s.call(t, http.MethodGet, "/me", nil, "", sessionBearer(tokens.AccessToken))
	require.Equal(t, http.StatusOK, status, body)

	status, body = s.call(t, http.MethodDelete, "/me/sessions/"+cliSession.Id, nil, "", sessionCookie(s.ada))
	require.Equal(t, http.StatusNoContent, status, body)
	status, body = s.call(t, http.MethodGet, "/me", nil, "", sessionBearer(tokens.AccessToken))
	assert.Equal(t, http.StatusUnauthorized, status, "a revoked CLI session's bearer is refused: %s", body)
	status, body = s.refresh(t, tokens.RefreshToken)
	require.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, apigen.InvalidGrant, oauthError(t, body).Error, "and its refresh token buys nothing")
}
