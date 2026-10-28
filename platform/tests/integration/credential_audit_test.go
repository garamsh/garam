package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/api"
	auditpg "github.com/garamsh/garam/platform/internal/audit/postgres"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
	"github.com/garamsh/garam/platform/internal/wire"
)

// auditBrowser is a browser-facing listener over the execution stack's
// database that records credential events and serves their read, as
// `garam serve api` composes it. The stack's sessions sign in here too.
func auditBrowser(t *testing.T, s *executionStack, engine grpc.ClientConnInterface) string {
	t.Helper()
	srv := api.NewServer(userpg.New(s.pool), orgpg.New(s.pool), agentpg.New(s.pool), operatorpg.New(s.pool),
		nil, nil, authnpg.NewSessionStore(s.pool), nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), nil, nil, nil, nil, api.Config{SessionTTL: time.Hour}).
		WithCredentialAudit(auditpg.NewCredentialStore(s.pool))
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	return browser.URL
}

// browserDo makes one browser request as cookie.
func browserDo(t *testing.T, method, target string, cookie *http.Cookie, body io.Reader) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, body)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

// credentialEvents reads subject's events as cookie, answering them and
// the body they arrived in.
func credentialEvents(t *testing.T, browser string, cookie *http.Cookie, subject grn.GRN) ([]apigen.CredentialEvent, string) {
	t.Helper()
	status, body := browserDo(t, http.MethodGet,
		browser+"/orgs/acme/credential-events?"+url.Values{"subject": {subject.String()}}.Encode(), cookie, nil)
	require.Equal(t, http.StatusOK, status, body)
	var events []apigen.CredentialEvent
	require.NoError(t, json.Unmarshal([]byte(body), &events))
	return events, body
}

// supervisedHost enrolls a standalone host under
// standalone-enrollment.v2 and answers its enrollment and the token its
// supervisor exchanged its credential for.
func supervisedHost(t *testing.T, s *tokenStack) (credentialed, string) {
	t.Helper()
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(time.Hour), nil))
	require.Equal(t, http.StatusCreated, status, body)
	c := credentialOf(t, s.spend(t, h.secret, h.enrollment(t)))
	status, token := s.supervisorExchange(t, c.credential, `{}`)
	require.Equal(t, http.StatusCreated, status)
	return c, token
}

// TestCredentialAudit_RecordsASupervisorsCredentialAndItsAgentsTokens
// carries a standalone host through the listener `serve machine`
// composes: its credential is recorded issued, and its supervisor's
// mint of the agent's token recorded issued with the token's expiry,
// beside a mint naming an audience recorded refused. A member reads
// each subject's events newest first, and no body carries a
// credential or a token.
func TestCredentialAudit_RecordsASupervisorsCredentialAndItsAgentsTokens(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := &tokenStack{executionStack: newExecutionStack(t, p, engine)}
	browser := auditBrowser(t, s.executionStack, engine)

	c, token := supervisedHost(t, s)
	minted := s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/tokens", operator.TokenContractVersion, token, "", `{}`)
	require.Equal(t, http.StatusCreated, minted.status, minted.body)
	var own machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(minted.body), &own))
	shown := s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/tokens", operator.TokenContractVersion, token, "",
		`{"audience":"`+s.hosted.String()+`"}`)
	require.Equal(t, http.StatusBadRequest, shown.status, shown.body)

	events, raw := credentialEvents(t, browser, s.member, c.supervisor)
	require.Len(t, events, 1)
	assert.Equal(t, apigen.EventSupervisorCredential, events[0].Credential)
	assert.Equal(t, apigen.EventIssued, events[0].Outcome)
	assert.NotContains(t, raw, c.credential)

	events, raw = credentialEvents(t, browser, s.member, c.agent)
	require.Len(t, events, 2)
	assert.Equal(t, apigen.EventAgentToken, events[0].Credential)
	assert.Equal(t, apigen.EventRefused, events[0].Outcome, "the mint naming an audience")
	require.NotNil(t, events[0].RefusalKind)
	assert.Equal(t, "invalid_argument", *events[0].RefusalKind)
	assert.Equal(t, apigen.EventIssued, events[1].Outcome)
	require.NotNil(t, events[1].NotAfter)
	// Postgres keeps microseconds, so the event is the expiry to that.
	assert.True(t, own.ExpiresAt.Truncate(time.Microsecond).Equal(*events[1].NotAfter),
		"the token's expiry: %v %v", own.ExpiresAt, *events[1].NotAfter)
	assert.NotContains(t, raw, own.Token)
	assert.NotContains(t, raw, token)
}

// TestCredentialAudit_IsReadOnTheSubjectsRead lists an operator's and a
// supervisor's events as the member, then attaches a Deny of
// operator:read to the member: the operator's events are refused and
// the supervisor's, read on agent:read, still answered — so what
// refuses the first is the subject's own read and not a gate on the
// route. A subject of another organization, and a user, are refused
// with 400; the paging is the shared limit and offset.
func TestCredentialAudit_IsReadOnTheSubjectsRead(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := &tokenStack{executionStack: newExecutionStack(t, p, engine)}
	browser := auditBrowser(t, s.executionStack, engine)
	c, token := supervisedHost(t, s)
	require.Equal(t, http.StatusCreated, s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/tokens",
		operator.TokenContractVersion, token, "", `{}`).status)
	require.Equal(t, http.StatusBadRequest, s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/tokens",
		operator.TokenContractVersion, token, "", `{"audience":"`+s.hosted.String()+`"}`).status)
	status, body := browserDo(t, http.MethodPost, browser+"/orgs/acme/operators", s.owner,
		jsonReader(t, map[string]string{"identifier": "field-read"}))
	require.Equal(t, http.StatusCreated, status, body)
	operatorGRN := scopedGRN("acme", operator.ServiceType, "field-read")

	read := func(subject grn.GRN, query string) (int, string) {
		return browserDo(t, http.MethodGet, browser+"/orgs/acme/credential-events?"+
			url.Values{"subject": {subject.String()}}.Encode()+query, s.member, nil)
	}
	status, body = read(operatorGRN, "")
	require.Equal(t, http.StatusOK, status, "control, before the Deny: %s", body)
	status, body = read(c.agent, "&limit=1&offset=1")
	require.Equal(t, http.StatusOK, status, body)
	var page []apigen.CredentialEvent
	require.NoError(t, json.Unmarshal([]byte(body), &page))
	require.Len(t, page, 1)
	assert.Equal(t, apigen.EventIssued, page[0].Outcome, "the second newest is the issuance")

	denied := activePolicy(t, policies, s.acme, "no-operator-read",
		statementDoc("no-operator-read", wire.EffectDeny, org.ActionOperatorRead, s.acme.String()))
	attachPolicy(t, policies, denied, grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "bob"))

	status, body = read(operatorGRN, "")
	assert.Equal(t, http.StatusForbidden, status, body)
	status, body = read(c.supervisor, "")
	assert.Equal(t, http.StatusOK, status, "a supervisor is read on agent:read: %s", body)
	status, body = read(scopedGRN("globex", operator.ServiceType, "x"), "")
	assert.Equal(t, http.StatusBadRequest, status, body)
	status, body = read(grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "ada"), "")
	assert.Equal(t, http.StatusBadRequest, status, body)
}

// jsonReader is v as a request body.
func jsonReader(t *testing.T, v any) io.Reader {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewReader(raw)
}
