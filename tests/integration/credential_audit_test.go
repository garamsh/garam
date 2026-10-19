package integration_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	apigen "github.com/garamsh/garam/api"
	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/audit"
	auditpg "github.com/garamsh/garam/internal/audit/postgres"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
	"github.com/garamsh/garam/internal/wire"
)

// auditBrowser is a browser-facing listener over the execution stack's
// database that records credential events and serves their read, as
// `garam serve api` composes it. The stack's sessions sign in here too.
func auditBrowser(t *testing.T, s *executionStack, engine grpc.ClientConnInterface) string {
	t.Helper()
	srv := api.NewServer(userpg.New(s.pool), orgpg.New(s.pool), agent.NewPostgresStore(s.pool), operator.NewPostgresStore(s.pool),
		nil, nil, authn.NewPostgresSessionStore(s.pool), nil, nil, nil, nil,
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

// leafFacts is a PEM certificate's serial and expiry.
func leafFacts(t *testing.T, certificatePEM string) (string, time.Time) {
	t.Helper()
	block, _ := pem.Decode([]byte(certificatePEM))
	require.NotNil(t, block)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert.SerialNumber.String(), cert.NotAfter
}

// TestCredentialAudit_RecordsASupervisorsCertificatesAndItsAgentsTokens
// carries a standalone host through the listener `serve machine`
// composes: a v1 enrollment's supervisor certificate is recorded issued
// with the serial and expiry of the certificate answered, and its
// renewal, asked with its whole life ahead, recorded refused. A v2
// host's credential is recorded issued, and its supervisor's mint of the
// agent's token recorded issued with the token's expiry, beside a mint
// naming an audience recorded refused. A member reads each subject's
// events newest first, and no body carries a certificate, a credential
// or a token.
func TestCredentialAudit_RecordsASupervisorsCertificatesAndItsAgentsTokens(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := &tokenStack{executionStack: newExecutionStack(t, p, engine)}
	s.client, _ = tokenClient(t)
	browser := auditBrowser(t, s.executionStack, engine)

	_, v1 := s.standaloneOf(t)
	csr, _ := enrollmentRequest(t)
	early := s.renewal(t, v1.supervisorTLS, "renew-early", csr)
	require.Equal(t, http.StatusConflict, early.status, early.body)

	events, raw := credentialEvents(t, browser, s.member, v1.supervisor)
	require.Len(t, events, 2)
	assert.Equal(t, apigen.EventRenew, events[0].Action, "newest first")
	assert.Equal(t, apigen.EventRefused, events[0].Outcome)
	require.NotNil(t, events[0].RefusalKind)
	assert.Equal(t, "failed_precondition", *events[0].RefusalKind)
	assert.Nil(t, events[0].Serial, "a refusal names nothing issued")
	issued := events[1]
	assert.Equal(t, apigen.EventSupervisorCertificate, issued.Credential)
	assert.Equal(t, apigen.EventIssue, issued.Action)
	assert.Equal(t, apigen.EventIssued, issued.Outcome)
	assert.False(t, issued.Replay)
	serial, notAfter := leafFacts(t, v1.supervisorLeaf)
	require.NotNil(t, issued.Serial)
	assert.Equal(t, serial, *issued.Serial, "the serial of the certificate answered")
	require.NotNil(t, issued.NotAfter)
	assert.True(t, notAfter.Equal(*issued.NotAfter), "its expiry: %v, %v", notAfter, *issued.NotAfter)
	assert.NotContains(t, raw, "CERTIFICATE")

	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.credentialApproval(t))
	require.Equal(t, http.StatusCreated, status, body)
	c := credentialOf(t, s.spendV2(t, h.secret, h.credentialEnrollment()))
	status, token := s.supervisorExchange(t, c.credential, `{}`)
	require.Equal(t, http.StatusCreated, status)
	minted := s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/tokens", operator.TokenContractVersion, token, "", `{}`)
	require.Equal(t, http.StatusCreated, minted.status, minted.body)
	var own machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(minted.body), &own))
	shown := s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/tokens", operator.TokenContractVersion, token, "",
		`{"audience":"`+s.hosted.String()+`"}`)
	require.Equal(t, http.StatusBadRequest, shown.status, shown.body)

	events, raw = credentialEvents(t, browser, s.member, c.supervisor)
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

// TestCredentialAudit_RecordsAnOperatorRenewalAndItsRefusal renews an
// operator certificate three quarters through its life, and one with
// its whole life ahead: the first is recorded renewed with the
// successor's serial, the second refused as failed_precondition.
func TestCredentialAudit_RecordsAnOperatorRenewalAndItsRefusal(t *testing.T) {
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(context.Background(), org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	listener := machineListener(t, p, sealer, engine)
	due := scopedGRN("acme", operator.ServiceType, "due")
	fresh := scopedGRN("acme", operator.ServiceType, "fresh")
	dueClient := machineClient(t, installed(t, p, aged(t, p, sealer, time.Hour, time.Now().Add(-45*time.Minute)), due))
	freshClient := machineClient(t, installed(t, p, aged(t, p, sealer, time.Hour, time.Now()), fresh))

	status, body := renew(t, dueClient, listener)
	require.Equal(t, http.StatusCreated, status, string(body))
	var issued machinegen.MachineCertificate
	require.NoError(t, json.Unmarshal(body, &issued))
	status, body = renew(t, freshClient, listener)
	require.Equal(t, http.StatusConflict, status, string(body))

	store := auditpg.NewCredentialStore(p)
	renewed, err := store.ListCredentials(context.Background(), due, listfilter.Filter{})
	require.NoError(t, err)
	require.Len(t, renewed, 1)
	assert.Equal(t, audit.ActionRenew, renewed[0].Action())
	assert.Equal(t, audit.OutcomeIssued, renewed[0].Outcome())
	serial, notAfter := leafFacts(t, issued.CertificatePem)
	assert.Equal(t, serial, renewed[0].Serial(), "the successor's serial")
	assert.True(t, notAfter.Equal(renewed[0].Validity().NotAfter))

	refused, err := store.ListCredentials(context.Background(), fresh, listfilter.Filter{})
	require.NoError(t, err)
	require.Len(t, refused, 1)
	assert.Equal(t, audit.OutcomeRefused, refused[0].Outcome())
	reason, ok := refused[0].Reason()
	require.True(t, ok)
	assert.Equal(t, "failed_precondition", reason)
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
	s := newExecutionStack(t, p, engine)
	browser := auditBrowser(t, s, engine)
	_, v1 := s.standaloneOf(t)
	csr, _ := enrollmentRequest(t)
	require.Equal(t, http.StatusConflict, s.renewal(t, v1.supervisorTLS, "renew-early", csr).status)
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
	status, body = read(v1.supervisor, "&limit=1&offset=1")
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
	status, body = read(v1.supervisor, "")
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
