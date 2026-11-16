package integration_test

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/cli"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// heldClock is the time the machine listener measures tokens and
// grants against, moved by the test across an expiry while a
// connection stays open. The listener reads it from its own goroutines.
type heldClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *heldClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *heldClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = at
}

// proofStack is the machine listener `garam serve machine` builds, over
// Postgres and a policy engine, with acme's hosted operator, the
// controller its grant covers for iam:agent.activate, a second
// controller the grant does not name, and globex's controller beside
// them.
type proofStack struct {
	pool       *pgxpool.Pool
	policies   *authz.PolicyGRPCServer
	engine     *grpc.ClientConn
	clock      *heldClock
	logs       *syncBuffer
	url        string
	acme       grn.GRN
	hosted     grn.GRN
	controller grn.GRN
	other      grn.GRN
	globex     grn.GRN
	grants     *operatorpg.Store
	grant      operator.GrantID
	// client exchanges each operator's key, and keys holds the key
	// each operator signs its assertions with.
	client *http.Client
	keys   map[grn.GRN]*rsa.PrivateKey
	// hostedTLS is the hosted operator's client, one connection reused
	// across every request a test sends it, presenting the token whose
	// expiry is hostedExpiry.
	hostedTLS    *http.Client
	hostedExpiry time.Time
	// controllerShown and otherShown are each controller's token shown
	// to the hosted operator, which the hosted operator forwards, and
	// globexToken globex's controller's token for garam.
	controllerShown string
	otherShown      string
	globexToken     string
}

// syncBuffer is the listener's log, which its goroutines write.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newProofStack(t *testing.T, engine *grpc.ClientConn, p *pgxpool.Pool, policies *authz.PolicyGRPCServer) *proofStack {
	t.Helper()
	return newProofStackDeciding(t, engine, p, policies, newCurrentDecisions(p))
}

// newProofStackDeciding is newProofStack where decisions is the policy
// service the listener asks, which the suite tagged policymatrix gives
// the real one.
func newProofStackDeciding(t *testing.T, engine *grpc.ClientConn, p *pgxpool.Pool, policies *authz.PolicyGRPCServer, decisions policydecision.Service) *proofStack {
	t.Helper()
	ctx := context.Background()
	orgs := orgpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))

	_, sealer := keyEncryptionKey(t)
	clock := &heldClock{at: time.Now()}
	logs := &syncBuffer{}
	presented := loopbackCertificate(t)
	srv, err := cli.NewMachineServer(p, cli.MachineConfig{
		Sealer:      sealer,
		Issuer:      testIssuer,
		Certificate: presented,
		ServerRoot:  certificatePEM(presented),
		Delivery:    briefDelivery(),
		Decisions:   decisions,
		Logger:      slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:         clock.now,
		TokenURL:    testTokenURL,
	})
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })

	s := &proofStack{
		pool: p, policies: policies, engine: engine, clock: clock, logs: logs,
		url: "https://" + lis.Addr().String(), acme: acme, keys: map[grn.GRN]*rsa.PrivateKey{},
		hosted:     scopedGRN("acme", operator.ServiceType, "hosted"),
		controller: scopedGRN("acme", operator.ServiceType, "controller"),
		other:      scopedGRN("acme", operator.ServiceType, "other"),
		globex:     scopedGRN("globex", operator.ServiceType, "controller"),
		grants:     operatorpg.New(p),
	}
	s.client, _ = tokenClient(t)
	for _, g := range []grn.GRN{s.hosted, s.controller, s.other, s.globex} {
		registerOperator(t, p, g)
		s.keys[g], _ = issueKey(t, operatorpg.New(p), g, operator.KeyRotationFirst)
	}
	s.hostedTLS = tokenPresenting(t, s.token(t, s.hosted, nil))
	s.hostedExpiry = time.Now().Add(operator.TokenTTL)
	s.controllerShown = s.token(t, s.controller, &s.hosted)
	s.otherShown = s.token(t, s.other, &s.hosted)
	s.globexToken = s.token(t, s.globex, nil)
	s.grant = s.delegate(t, "g-1", nil, time.Now().Add(time.Hour), s.controller)
	return s
}

// token exchanges subject's key for a token: garam's where audience is
// nil, and one shown to audience otherwise.
func (s *proofStack) token(t *testing.T, subject grn.GRN, audience *grn.GRN) string {
	t.Helper()
	return exchangeKey(t, s.client, s.url, subject, s.keys[subject], audience)
}

// operatorTLS is a client presenting subject's token for garam.
func (s *proofStack) operatorTLS(t *testing.T, subject grn.GRN) *http.Client {
	t.Helper()
	return tokenPresenting(t, s.token(t, subject, nil))
}

// delegate writes the hosted operator's grant of iam:agent.activate on
// controllers, replacing expected, and answers its identifier.
func (s *proofStack) delegate(t *testing.T, requestID string, expected *operator.GrantID, expiresAt time.Time, controllers ...grn.GRN) operator.GrantID {
	t.Helper()
	id := operator.GrantID("grant-" + requestID)
	g, _, err := s.grants.WriteGrant(context.Background(), operator.GrantWrite{
		Org: s.acme, Operator: s.hosted, RequestID: operator.RequestID(requestID), Expected: expected,
		Terms: operator.GrantTerms{Controllers: controllers,
			Operations: []operator.Operation{operator.OperationAgentActivate},
			ExpiresAt:  expiresAt.UTC().Truncate(time.Microsecond)},
		Actor: scopedGRN("root", "user", "ada"), Now: time.Now(),
	}, id)
	require.NoError(t, err)
	return g.ID
}

// proof is one answer from the route, and whether the client sent it
// over a connection an earlier request had opened.
type proof struct {
	status int
	body   string
	reused bool
}

// prove asks the route as client for controller under
// operation-authority.v2, forwarding token and naming agent where it is
// not empty.
func (s *proofStack) prove(t *testing.T, client *http.Client, controller grn.GRN, token, agent string) proof {
	t.Helper()
	body := map[string]any{"token": token}
	if agent != "" {
		body["agent"] = agent
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	var got proof
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { got.reused = info.Reused }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodPost,
		s.url+"/operators/"+pathEscape(controller.String())+"/introspection", bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(operator.ContractVersionHeader, operator.TokenAuthorityContractVersion)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, operator.TokenAuthorityContractVersion, resp.Header.Get(operator.ContractVersionHeader))
	got.status, got.body = resp.StatusCode, string(answer)
	return got
}

// onOpenConnection asserts the answer came over a connection an earlier
// request opened, so what refused it was read on this request rather
// than at a handshake.
func onOpenConnection(t *testing.T, p proof, status int, msg string) {
	t.Helper()
	assert.Equal(t, status, p.status, "%s: %s", msg, p.body)
	assert.True(t, p.reused, "%s: the request reused an open connection", msg)
}

// provision stands an agent up assigned to the controller client
// authenticates as, read off the listener, and answers it with its
// epoch.
func (s *proofStack) provision(t *testing.T, client *http.Client) (string, int) {
	t.Helper()
	status, body := machineRequest(t, client, http.MethodGet, s.url+"/identity")
	require.Equal(t, http.StatusOK, status, string(body))
	var identity struct {
		Grn string `json:"grn"`
	}
	require.NoError(t, json.Unmarshal(body, &identity))
	controller, err := grn.Parse(identity.Grn)
	require.NoError(t, err)
	agents := agentpg.New(s.pool)
	g := seedAgent(t, agents, grn.Org(controller.Organization))
	assignment, err := agents.Assign(context.Background(), g, controller)
	require.NoError(t, err)
	return g.String(), assignment.Epoch
}

// TestControllerProof_ProvesTheControllerItsTokenNames is the session
// proof and the refusal of every party it is not: another controller,
// another organization's, a token naming another operator or minted by
// nobody, and another audience. The valid controller is the control,
// through the same listener and caller.
func TestControllerProof_ProvesTheControllerItsTokenNames(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newProofStack(t, engine, p, policies)

	got := s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "")
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.JSONEq(t, `{"operator":"`+s.controller.String()+`","org":"`+s.acme.String()+`","agent":null}`, got.body)

	got = s.prove(t, s.hostedTLS, s.other, s.otherShown, "")
	assert.Equal(t, http.StatusForbidden, got.status, "a controller the grant does not name: %s", got.body)
	got = s.prove(t, s.hostedTLS, s.controller, s.otherShown, "")
	assert.Equal(t, http.StatusUnprocessableEntity, got.status, "another controller's token: %s", got.body)
	got = s.prove(t, s.hostedTLS, s.controller, operator.TokenPrefix+strings.Repeat("0", 64), "")
	assert.Equal(t, http.StatusUnprocessableEntity, got.status, "a token of the right spelling nobody minted: %s", got.body)
	got = s.prove(t, s.hostedTLS, s.globex, s.globexToken, "")
	assert.Equal(t, http.StatusNotFound, got.status, "another organization's controller: %s", got.body)

	// The controller is registered and holds a live token, and proves
	// the other controller by a token shown to it, but no grant names it
	// as an audience.
	got = s.prove(t, s.operatorTLS(t, s.controller), s.other, s.token(t, s.other, &s.controller), "")
	assert.Equal(t, http.StatusForbidden, got.status, "another audience: %s", got.body)

	for _, line := range []string{s.logs.String(), got.body} {
		assert.NotContains(t, line, strings.TrimPrefix(s.controllerShown, operator.TokenPrefix), "no part of a forwarded token")
	}
}

// TestControllerProof_BindsAnAgentToItsCurrentAssignment holds the
// agent-bound proof: the agent's current epoch for an agent placed on
// the controller, and a refusal once it is placed elsewhere or for an
// agent the organization does not have. The first proof is the control.
func TestControllerProof_BindsAnAgentToItsCurrentAssignment(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newProofStack(t, engine, p, policies)
	controllerTLS := s.operatorTLS(t, s.controller)
	placed, epoch := s.provision(t, controllerTLS)

	got := s.prove(t, s.hostedTLS, s.controller, s.controllerShown, placed)
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.JSONEq(t, `{"operator":"`+s.controller.String()+`","org":"`+s.acme.String()+
		`","agent":{"grn":"`+placed+`","epoch":"`+strconv.Itoa(epoch)+`"}}`, got.body)

	elsewhere, _ := s.provision(t, s.operatorTLS(t, s.other))
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, elsewhere)
	assert.Equal(t, http.StatusForbidden, got.status, "an agent assigned to another controller: %s", got.body)
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, scopedGRN("acme", "agent", "nobody").String())
	assert.Equal(t, http.StatusNotFound, got.status, "an agent the organization does not have: %s", got.body)
	// globex's agent is real and placed, on globex's controller: what
	// refuses it is the organization, decided before its placement is
	// read.
	foreign, _ := s.provision(t, s.operatorTLS(t, s.globex))
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, foreign)
	assert.Equal(t, http.StatusNotFound, got.status, "another organization's agent: %s", got.body)

	// Moving the agent away and back raises the epoch: the proof answers
	// the current one, which is what the caller compares.
	agents := agentpg.New(p)
	placedGRN, err := grn.Parse(placed)
	require.NoError(t, err)
	_, err = agents.Assign(context.Background(), placedGRN, s.other)
	require.NoError(t, err)
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, placed), http.StatusForbidden, "moved")
	back, err := agents.Assign(context.Background(), placedGRN, s.controller)
	require.NoError(t, err)
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, placed)
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.Contains(t, got.body, `"epoch":"`+strconv.Itoa(back.Epoch)+`"`)
	assert.NotEqual(t, epoch, back.Epoch)
}

// TestControllerProof_RefusesWithdrawalOnTheSameOpenConnection holds
// that a withdrawal refuses the next decision on a connection that
// authenticated before it: an ended grant, a deleted controller, whose
// tokens go with it, and the caller's own deleted registration. Each
// refusal follows a proof on the same connection, which is its control.
func TestControllerProof_RefusesWithdrawalOnTheSameOpenConnection(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newProofStack(t, engine, p, policies)

	require.Equal(t, http.StatusOK, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "").status)
	require.NoError(t, s.grants.EndGrant(ctx, s.acme, s.hosted, s.grant, time.Now()))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, ""), http.StatusForbidden, "grant ended")

	s.delegate(t, "g-2", nil, time.Now().Add(time.Hour), s.controller, s.other)
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, ""), http.StatusOK, "granted again")
	require.NoError(t, operatorpg.New(p).Delete(ctx, s.controller))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, ""), http.StatusUnprocessableEntity,
		"controller deleted, and its tokens with it")

	// The other controller, which the same grant names, is the control
	// for the caller's own deletion: it answers on this connection until
	// the caller goes.
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.other, s.otherShown, ""), http.StatusOK, "the other controller")
	require.NoError(t, operatorpg.New(p).Delete(ctx, s.hosted))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.other, s.otherShown, ""), http.StatusUnauthorized,
		"caller deleted, and its token with it")
}

// TestControllerProof_ReadsTheTimeOnEveryDecision holds three expiries
// on one open connection: the forwarded controller token's, which the
// database reads, and the grant's and the caller's own token's, which
// the listener's clock is moved across. Each refusal is read against a
// token or caller the same move leaves valid.
func TestControllerProof_ReadsTheTimeOnEveryDecision(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newProofStack(t, engine, p, policies)
	start := s.clock.now()
	fresh := s.token(t, s.controller, &s.hosted)

	require.Equal(t, http.StatusOK, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "").status)
	_, err := p.Exec(context.Background(), `UPDATE operator_tokens SET expires_at = now() - interval '1 second' WHERE token_hash = $1`,
		operator.HashToken(s.controllerShown))
	require.NoError(t, err)
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, ""), http.StatusUnprocessableEntity, "forwarded token expired")
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, fresh, ""), http.StatusOK, "a forwarded token still live")

	grantEnds := start.Add(30 * time.Minute)
	second := s.delegate(t, "g-2", &s.grant, grantEnds, s.controller)
	s.clock.set(grantEnds.Add(time.Second))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, fresh, ""), http.StatusForbidden, "grant expired")

	s.delegate(t, "g-3", &second, s.hostedExpiry.Add(time.Hour), s.controller)
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, fresh, ""), http.StatusOK, "granted past the caller's expiry")
	s.clock.set(s.hostedExpiry.Add(time.Minute))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, fresh, ""), http.StatusForbidden, "caller token expired")
}

// TestControllerProof_AsksTheServiceOnEveryProof holds that the proof is
// decided by the policy service's answer read now, not by one kept from
// an earlier proof: each proof on an open connection is a question to
// it, and an answer that turns to a denial refuses the next, for the
// session and for an agent. What the service decides from what it holds
// is the policymatrix suite's.
func TestControllerProof_AsksTheServiceOnEveryProof(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	decisions := newCurrentDecisions(p)
	s := newProofStackDeciding(t, engine, p, policies, decisions)
	controllerTLS := s.operatorTLS(t, s.controller)
	placed, _ := s.provision(t, controllerTLS)

	// A proof opens the hosted client's connection, so that the two
	// counted ones are each asked on an open one.
	require.Equal(t, http.StatusOK, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "").status)
	before := len(decisions.currentQuestions())
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, ""), http.StatusOK, "the first proof")
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, ""), http.StatusOK, "the second, on the same connection")
	asked := decisions.currentQuestions()[before:]
	require.Len(t, asked, 2, "one question for each proof, none answered from the one before")
	for _, q := range asked {
		assert.Equal(t, policydecision.Question{
			Subject: s.hosted, Action: "iam:agent.activate", Resource: s.acme, Current: true,
		}, q)
	}

	decisions.forbid(s.hosted, "iam:agent.activate")
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, ""), http.StatusForbidden, "the service now denies it")
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, placed), http.StatusForbidden, "and denies it for an agent")
}

// TestControllerProof_UnknownFreshnessRefusesAsUnavailable holds that
// authority garam cannot read as current is 503 naming no cause: a
// policy service that fails, and a database that stops answering
// under an open connection. The proof each refuses after is the
// control, on the same connection.
func TestControllerProof_UnknownFreshnessRefusesAsUnavailable(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	decisions := newCurrentDecisions(p)
	s := newProofStackDeciding(t, engine, p, nil, decisions)

	require.Equal(t, http.StatusOK, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "").status)
	decisions.failWith(errorx.New(errorx.KindInternal, "the policy service is down"))
	got := s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "")
	onOpenConnection(t, got, http.StatusServiceUnavailable, "a policy service that fails")
	assert.Contains(t, got.body, `"message":"internal error"`)

	decisions.failWith(nil)
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerShown, ""), http.StatusOK, "the service restored")
	p.Close()
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerShown, "")
	onOpenConnection(t, got, http.StatusServiceUnavailable, "a database that stopped answering")
	assert.Contains(t, got.body, `"message":"internal error"`)
}
