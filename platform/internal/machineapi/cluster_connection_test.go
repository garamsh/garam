package machineapi_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	connectiongen "github.com/garamsh/garam/platform/api/cluster-connection/v1alpha1"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/operator"
)

const connectionTokenURL = "https://machine.garam.test/oauth/token"

// connectedGRN is an instance of an app whose machine operations are
// the connector's, and otherAppGRN one of an app that lists none.
var (
	connectedGRN = grn.New("acme", app.InstanceType, "5f60710a1b2c3d4e")
	otherAppGRN  = grn.New("acme", app.InstanceType, "0000000000000001")
)

// instanceReach is what each test instance's token reaches, as
// app.CredentialStore.ResolveToken reads it off the app's record. An
// instance it does not name reaches nothing.
var instanceReach = map[grn.GRN]machine.Reach{
	connectedGRN: machine.ReachOf([]string{
		http.MethodGet + " /cluster-connection/v1alpha1/clusters/self/declarations",
		http.MethodPost + " /cluster-connection/v1alpha1/clusters/self/reports",
	}),
}

// connectionClusters is app.Service and bot.Service answering err,
// or what it was asked, and recording the arguments.
type connectionClusters struct {
	err    error
	caller grn.GRN
	jwk    []byte
	token  string
	// page is what WatchDeclarations answers, and after and wait what
	// it was asked.
	page  bot.Page
	after *string
	wait  time.Duration
	// report is what Report was handed.
	report *bot.Report
	// exchange is what MintBotToken was handed.
	exchange *bot.BotExchange
	// held is the live bot token each value is, as ResolveBotToken
	// answers it; a value it does not name is not live.
	held map[string]bot.HeldBotToken
	// holdErr is what HoldMailboxPoll answers; nil holds the poll.
	holdErr error
}

func (c *connectionClusters) Report(_ context.Context, caller grn.GRN, r bot.Report) error {
	c.caller, c.report = caller, &r
	return c.err
}

func (c *connectionClusters) WatchDeclarations(_ context.Context, caller grn.GRN, after *string, wait time.Duration) (bot.Page, error) {
	c.caller, c.after, c.wait = caller, after, wait
	return c.page, c.err
}

func (c *connectionClusters) Register(_ context.Context, _, token string, jwk []byte) (grn.GRN, error) {
	c.token, c.jwk = token, jwk
	return connectedGRN, c.err
}

func (c *connectionClusters) ExchangeAssertion(_ context.Context, _ string, client grn.GRN, _ string) (*app.Token, error) {
	c.caller = client
	if c.err != nil {
		return nil, c.err
	}
	return &app.Token{Value: app.TokenPrefix + "answered", Instance: client, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (c *connectionClusters) MintBotToken(ctx context.Context, exchange bot.BotExchange) (*bot.BotToken, error) {
	c.exchange = &exchange
	if c.err != nil {
		return nil, c.err
	}
	// The domain spends the proof once the actor authenticated.
	if exchange.SpendProof != nil {
		if err := exchange.SpendProof(ctx); err != nil {
			return nil, err
		}
	}
	return &bot.BotToken{Value: bot.BotTokenPrefix + "answered", Bot: grn.New("acme", bot.BotServiceType, "decl-a"),
		Place: connectedGRN, Scope: []string{}, ExpiresAt: time.Now().Add(bot.BotTokenTTL)}, nil
}

func (c *connectionClusters) ResolveBotToken(_ context.Context, value string) (bot.HeldBotToken, error) {
	c.token = value
	held, ok := c.held[value]
	if !ok {
		return bot.HeldBotToken{}, bot.ErrBotTokenInvalid
	}
	return held, nil
}

func (c *connectionClusters) HoldMailboxPoll(grn.GRN) (func(), error) {
	if c.holdErr != nil {
		return nil, c.holdErr
	}
	return func() {}, nil
}

func (c *connectionClusters) Rotate(_ context.Context, caller grn.GRN, jwk []byte, _, _ string) (app.Rotation, error) {
	c.caller, c.jwk = caller, jwk
	return app.Rotation{KID: "successor", PredecessorRetiresAt: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)}, c.err
}

// serveConnection serves cluster-connection behind the machine
// listener, which resolves "cluster" to the connector's instance,
// "other-app" to an instance of an app listing none of its routes and
// "operator" to an operator.
func serveConnection(t *testing.T, clusters *connectionClusters) (string, *http.Client) {
	t.Helper()
	served := machine.NewServer(presentedTokens{
		"cluster":   connectedGRN,
		"other-app": otherAppGRN,
		"operator":  grn.New("acme", operator.ServiceType, "one"),
	}, nil, loopback(t), connectiongen.HandlerWithOptions(machineapi.NewClusterConnection(clusters, clusters, connectionTokenURL),
		connectiongen.StdHTTPServerOptions{ErrorHandlerFunc: machineapi.ConnectionBindingError}))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = served.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = served.Close() })
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		//nolint:gosec // the listener's own certificate is not under test.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}
	t.Cleanup(transport.CloseIdleConnections)
	return "https://" + lis.Addr().String() + "/cluster-connection/v1alpha1", &http.Client{Transport: transport}
}

// connect sends body to path, presenting token where one is named, and
// answers the status, the decoded body and the response.
func connect(t *testing.T, client *http.Client, method, url, token, body string) (int, map[string]any, *http.Response) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", machine.TokenScheme+" "+token)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded), string(raw))
	return resp.StatusCode, decoded, resp
}

const registration = `{"registrationToken":"garam_crt_x","jwk":{"kty":"RSA","n":"n","e":"AQAB","d":"private"},"connector":{"version":"0.1.0"}}`

// TestClusterConnection_RegisterAnswersTheRecordAndHandsTheKeyOnWhole
// holds REG-6's answer, and that the key reaches the service as sent,
// private members included, for it to refuse (REG-2).
func TestClusterConnection_RegisterAnswersTheRecordAndHandsTheKeyOnWhole(t *testing.T) {
	clusters := &connectionClusters{}
	base, client := serveConnection(t, clusters)

	status, body, _ := connect(t, client, http.MethodPost, base+"/registrations", "", registration)
	assert.Equal(t, http.StatusCreated, status)
	assert.Equal(t, map[string]any{"cluster": connectedGRN.String(), "clientId": connectedGRN.String(),
		"tokenUrl": connectionTokenURL}, body)
	assert.Equal(t, "garam_crt_x", clusters.token)
	assert.JSONEq(t, `{"kty":"RSA","n":"n","e":"AQAB","d":"private"}`, string(clusters.jwk))
}

// TestClusterConnection_RefusalsCarryTheInterfacesCodes holds T-5: each
// refusal the interface names is answered with its code and status,
// and a failure of garam's own with neither its kind nor its text.
func TestClusterConnection_RefusalsCarryTheInterfacesCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"a refused token", app.ErrRegistrationRefused, http.StatusUnauthorized, "registration_refused"},
		{"a refused key", app.ErrInvalidKey, http.StatusBadRequest, "invalid_request"},
		{"a refused proof", app.ErrInvalidProof, http.StatusBadRequest, "invalid_proof"},
		{"a key held before", app.ErrKeyHeld, http.StatusBadRequest, "invalid_request"},
		{"a third key", app.ErrTooManyKeys, http.StatusConflict, "too_many_keys"},
		{"a throttle", &operator.Exceeded{RetryAfter: 30 * time.Second}, http.StatusTooManyRequests, "throttled"},
		{"a missing record", errorx.New(errorx.KindNotFound, "cluster not found"), http.StatusNotFound, "not_found"},
		{"a rotation garam does not serve", app.ErrRotationUnavailable, http.StatusServiceUnavailable, "internal"},
		{"garam's own failure", errorx.Wrap(errors.New(`relation "cluster_keys" SQLSTATE 42P01`), "rotate",
			errorx.KindInternal), http.StatusInternalServerError, "internal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base, client := serveConnection(t, &connectionClusters{err: c.err})
			status, body, resp := connect(t, client, http.MethodPost, base+"/clusters/self/keys", "cluster",
				`{"jwk":{},"authorization":"a","proof":"p"}`)
			assert.Equal(t, c.status, status)
			assert.Equal(t, c.code, body["error"])
			assert.NotContains(t, body["message"], "SQLSTATE")
			if c.status == http.StatusTooManyRequests {
				assert.Equal(t, "30", resp.Header.Get("Retry-After"))
			}
		})
	}
}

// TestClusterConnection_RoutesButRegistrationAnswerAnInstancesTokenAlone
// holds AUTH-1 and AUTH-2 at the transport: no token, a token that
// proves nothing and another caller's token are each answered 401, as
// is an instance of an app whose machine operations leave out
// declarations and reports (ADR-0192 decision 7), and the connector's
// instance is the control.
func TestClusterConnection_RoutesButRegistrationAnswerAnInstancesTokenAlone(t *testing.T) {
	clusters := &connectionClusters{}
	base, client := serveConnection(t, clusters)
	rotation := `{"jwk":{"kty":"RSA"},"authorization":"a","proof":"p"}`

	for name, token := range map[string]string{"no token": "", "a voided token": "voided", "an operator's token": "operator"} {
		for _, route := range []struct{ method, path, body string }{
			{http.MethodPost, "/clusters/self/keys", rotation},
			{http.MethodGet, "/clusters/self/declarations", ""},
			{http.MethodPost, "/clusters/self/reports", "{}"},
		} {
			status, body, _ := connect(t, client, route.method, base+route.path, token, route.body)
			assert.Equal(t, http.StatusUnauthorized, status, name+" "+route.path)
			assert.Equal(t, "unauthorized", body["error"], name+" "+route.path)
		}
	}
	assert.Nil(t, clusters.jwk, "no refused request reached the service")

	// An instance whose app does not list declarations and reports among
	// its machine operations is refused them, and rotates its key, the
	// app system's own route.
	for _, route := range []struct{ method, path, body string }{
		{http.MethodGet, "/clusters/self/declarations", ""},
		{http.MethodPost, "/clusters/self/reports", "{}"},
	} {
		status, body, _ := connect(t, client, route.method, base+route.path, "other-app", route.body)
		assert.Equal(t, http.StatusUnauthorized, status, route.path)
		assert.Equal(t, "unauthorized", body["error"], route.path)
	}
	assert.Nil(t, clusters.report, "no refused report reached the service")
	status, _, _ := connect(t, client, http.MethodPost, base+"/clusters/self/keys", "other-app", rotation)
	assert.Equal(t, http.StatusCreated, status, "another app's instance rotates its key")
	assert.Equal(t, otherAppGRN, clusters.caller)

	status, body, _ := connect(t, client, http.MethodPost, base+"/clusters/self/keys", "cluster", rotation)
	assert.Equal(t, http.StatusCreated, status)
	assert.Equal(t, map[string]any{"kid": "successor", "predecessorRetiresAt": "2026-11-25T00:00:00Z"}, body)
	assert.Equal(t, connectedGRN, clusters.caller)
}

// TestClusterConnection_ARouteParameterTheRouterCannotBindIsRefused
// holds that a binding failure is answered in the interface's body.
func TestClusterConnection_ARouteParameterTheRouterCannotBindIsRefused(t *testing.T) {
	base, client := serveConnection(t, &connectionClusters{})
	status, body, _ := connect(t, client, http.MethodGet, base+"/clusters/self/declarations?wait=fifty", "cluster", "")
	assert.Equal(t, http.StatusBadRequest, status, "a parameter the router cannot bind")
	assert.Equal(t, "invalid_request", body["error"])
}

// connectionReport is a report REP-1 and REP-2 admit, objects being
// added in place of its empty list.
const connectionReport = `{"observedAt":"2026-11-24T12:00:00Z",
	"connector":{"version":"0.1.0","mode":"apply","namespaces":["team-a"]},
	"declarations":[{"id":"decl-a","generation":1,"outcome":"applied"}],"objects":[]}`

func withObject(object string) string {
	return strings.Replace(connectionReport, `"objects":[]`, `"objects":[`+object+`]`, 1)
}

// postReport sends body as the connected cluster's report, answering
// the status and the body as text.
func postReport(t *testing.T, client *http.Client, base, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/clusters/self/reports", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", machine.TokenScheme+" cluster")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(raw)
}

// TestClusterConnection_AReportIsHandedOnAndAnswered204 holds REP-5's
// answer: the report reaches the service as the caller's, with what it
// said kept as sent.
func TestClusterConnection_AReportIsHandedOnAndAnswered204(t *testing.T) {
	clusters := &connectionClusters{}
	base, client := serveConnection(t, clusters)
	status, body := postReport(t, client, base, withObject(`{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot",
		"namespace":"team-a","name":"mira","uid":"u1","owner":"cluster","generation":2,
		"spec":{"persona":{"role":"Lead"}},"status":{"phase":"Ready"},"unknownMember":true}`))
	require.Equal(t, http.StatusNoContent, status, body)
	assert.Empty(t, body)
	assert.Equal(t, connectedGRN, clusters.caller)
	require.Len(t, clusters.report.Objects, 1)
	assert.JSONEq(t, `{"persona":{"role":"Lead"}}`, string(clusters.report.Objects[0].Spec))
	assert.Equal(t, "decl-a", clusters.report.Declarations[0].ID)
}

// TestClusterConnection_AReportIsRefusedAsREP6AndREP7Refuse holds the
// refusals a report is answered before the service reads it.
func TestClusterConnection_AReportIsRefusedAsREP6AndREP7Refuse(t *testing.T) {
	object := func(kind string) string {
		return `{"apiVersion":"v1","kind":"` + kind + `","namespace":"team-a","name":"x","uid":"u","owner":"cluster",
			"generation":1,"status":{}}`
	}
	big := withObject(`{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot","namespace":"team-a","name":"x","uid":"u",
		"owner":"cluster","generation":1,"status":{"note":"` + strings.Repeat("x", bot.ReportMaxBytes) + `"}}`)
	for name, tc := range map[string]struct {
		body, code string
		status     int
	}{
		"a Secret (REP-7)":           {withObject(object("Secret")), "invalid_request", http.StatusBadRequest},
		"a ConfigMap (REP-7)":        {withObject(object("ConfigMap")), "invalid_request", http.StatusBadRequest},
		"an undeclared kind (REP-7)": {withObject(object("Deployment")), "invalid_request", http.StatusBadRequest},
		"not a report":               {`[]`, "invalid_request", http.StatusBadRequest},
		"over 4 MiB (REP-6)":         {big, "report_too_large", http.StatusRequestEntityTooLarge},
	} {
		t.Run(name, func(t *testing.T) {
			clusters := &connectionClusters{}
			base, client := serveConnection(t, clusters)
			status, body := postReport(t, client, base, tc.body)
			assert.Equal(t, tc.status, status, body)
			var refusal map[string]any
			require.NoError(t, json.Unmarshal([]byte(body), &refusal))
			assert.Equal(t, tc.code, refusal["error"])
			assert.Nil(t, clusters.report, "the service is not asked")
		})
	}
	clusters := &connectionClusters{}
	base, client := serveConnection(t, clusters)
	status, body := postReport(t, client, base, withObject(object("Workspace")))
	assert.Equal(t, http.StatusNoContent, status, "the control: a declared kind is handed on: %s", body)
}

// TestExchangeClientAssertion_SendsAnInstancesAssertionToItsRecord
// holds that /oauth/token picks the instance's profile by client_id's
// GRN type (ADR-0186 decision 4): an instance's assertion reaches the
// app service and no operator's, and an audience refuses it. An
// operator's assertion, the control, reaches the operator service.
func TestExchangeClientAssertion_SendsAnInstancesAssertionToItsRecord(t *testing.T) {
	operators, clusters := &assertionService{}, &connectionClusters{}
	base, _ := serveTLS(t, machineapi.NewServer(&agentService{}, operators, &messageService{}, unwatched(t),
		nil, nil, nil, nil, nil, nil, stubKeyring{}).WithInstances(clusters, clusters, nil, "", ""), connectedGRN)
	form := validForm()
	form.Set("client_id", connectedGRN.String())

	resp, body := exchangeForm(t, anonymous(t), base, form.Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Contains(t, body, app.TokenPrefix+"answered")
	assert.Equal(t, connectedGRN, clusters.caller)
	assert.Empty(t, operators.calls, "no operator's exchange ran")

	form.Set("audience", "grn:acme/iam/operator/control")
	resp, body = exchangeForm(t, anonymous(t), base, form.Encode(), nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
	assert.Equal(t, "invalid_request", oauthError(t, body))

	resp, body = exchangeForm(t, anonymous(t), base, validForm().Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Len(t, operators.calls, 1, "the control: an operator's assertion reaches the operator service")
}

// TestClusterConnection_WatchAnswersThePageAsGaramKeptIt holds DECL-1's
// page on the wire: the caller's record is read after the cursor and
// for the wait sent, an apply carries its object byte for byte, so no
// number of spec is rounded, and a delete carries none.
func TestClusterConnection_WatchAnswersThePageAsGaramKeptIt(t *testing.T) {
	clusters := &connectionClusters{page: bot.Page{Cursor: "c.2", More: true, Changes: []bot.Change{
		{Op: bot.OpApply, ID: "decl-a", Generation: 1, Object: json.RawMessage(`{"spec":{"replicas":12345678901234567890}}`)},
		{Op: bot.OpDelete, ID: "decl-b", Generation: 3},
	}}}
	base, client := serveConnection(t, clusters)
	req, err := http.NewRequest(http.MethodGet, base+"/clusters/self/declarations?after=c.0&wait=7", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", machine.TokenScheme+" cluster")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(raw))
	assert.JSONEq(t, `{"cursor":"c.2","more":true,"changes":[
		{"op":"apply","id":"decl-a","generation":1,"object":{"spec":{"replicas":12345678901234567890}}},
		{"op":"delete","id":"decl-b","generation":3}]}`, string(raw))
	assert.Contains(t, string(raw), "12345678901234567890")
	assert.Equal(t, connectedGRN, clusters.caller)
	require.NotNil(t, clusters.after)
	assert.Equal(t, "c.0", *clusters.after)
	assert.Equal(t, 7*time.Second, clusters.wait)

	_, _, _ = connect(t, client, http.MethodGet, base+"/clusters/self/declarations", "cluster", "")
	assert.Nil(t, clusters.after, "an absent cursor reaches the service as none")
	assert.Equal(t, bot.MaxWait, clusters.wait, "an absent wait is DECL-3's fifty seconds")
}

// TestClusterConnection_WatchAnswersAPurgedCursorGone holds DECL-6: a
// cursor at or before the purged position is 410 cursor_expired, the
// one refusal no kind maps to. A cursor that is no cursor is 400, the
// control.
func TestClusterConnection_WatchAnswersAPurgedCursorGone(t *testing.T) {
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{bot.ErrCursorExpired, http.StatusGone, "cursor_expired"},
		{bot.ErrCursorInvalid, http.StatusBadRequest, "invalid_request"},
	} {
		base, client := serveConnection(t, &connectionClusters{err: c.err})
		status, body, _ := connect(t, client, http.MethodGet, base+"/clusters/self/declarations?after=c.0&wait=0", "cluster", "")
		assert.Equal(t, c.status, status, c.code)
		assert.Equal(t, c.code, body["error"])
	}
}
