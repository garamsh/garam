package integration_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	operatorpg "github.com/garamsh/garam/internal/operator/postgres"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// heldClock is the time the machine listener measures certificates and
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
// controller its grant covers for agent:activate, a second controller
// the grant does not name, and globex's controller beside them.
type proofStack struct {
	pool       *pgxpool.Pool
	policies   *authz.PolicyGRPCServer
	engine     *grpc.ClientConn
	clock      *heldClock
	logs       *syncBuffer
	url        string
	sealer     ca.Sealer
	issuer     ca.Service
	acme       grn.GRN
	hosted     grn.GRN
	controller grn.GRN
	other      grn.GRN
	globex     grn.GRN
	grants     *operatorpg.Store
	grant      operator.GrantID
	// hostedTLS is the hosted operator's client, one connection reused
	// across every request a test sends it, and hostedLeaf the
	// certificate that connection authenticated with.
	hostedTLS  *http.Client
	hostedLeaf *x509.Certificate
	// controllerPEM, otherPEM and globexPEM are each controller's leaf
	// as its own TLS handshake presents it, which the hosted operator
	// forwards.
	controllerPEM string
	otherPEM      string
	globexPEM     string
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
	ctx := context.Background()
	orgs := orgpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))

	_, sealer := keyEncryptionKey(t)
	issuer, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	clock := &heldClock{at: time.Now()}
	logs := &syncBuffer{}
	presented := loopbackCertificate(t)
	srv, err := cli.NewMachineServer(p, cli.MachineConfig{
		Sealer:      sealer,
		Issuer:      testIssuer,
		Certificate: presented,
		ServerRoot:  certificatePEM(presented),
		Delivery:    briefDelivery(),
		Engine:      engine,
		Logger:      slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:         clock.now,
	})
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })

	s := &proofStack{
		pool: p, policies: policies, engine: engine, clock: clock, logs: logs,
		url: "https://" + lis.Addr().String(), sealer: sealer, issuer: issuer, acme: acme,
		hosted:     scopedGRN("acme", operator.ServiceType, "hosted"),
		controller: scopedGRN("acme", operator.ServiceType, "controller"),
		other:      scopedGRN("acme", operator.ServiceType, "other"),
		globex:     scopedGRN("globex", operator.ServiceType, "controller"),
		grants:     operatorpg.New(p),
	}
	hostedPair := installed(t, p, issuer, s.hosted)
	s.hostedTLS = machineClient(t, hostedPair)
	s.hostedLeaf, err = x509.ParseCertificate(hostedPair.Certificate[0])
	require.NoError(t, err)
	s.controllerPEM = leafPEM(installed(t, p, issuer, s.controller))
	s.otherPEM = leafPEM(installed(t, p, issuer, s.other))
	s.globexPEM = leafPEM(installed(t, p, issuer, s.globex))
	s.grant = s.delegate(t, "g-1", nil, time.Now().Add(time.Hour), s.controller)
	return s
}

// leafPEM is the leaf of pair in the PEM form a hosted operator
// forwards.
func leafPEM(pair tls.Certificate) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]}))
}

// delegate writes the hosted operator's grant of agent:activate on
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

// prove asks the route as client for controller, forwarding
// certificatePEM and naming agent where it is not empty.
func (s *proofStack) prove(t *testing.T, client *http.Client, controller grn.GRN, certificatePEM, agent string) proof {
	t.Helper()
	body := map[string]any{"certificatePem": certificatePEM}
	if agent != "" {
		body["agent"] = agent
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	var got proof
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { got.reused = info.Reused }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), http.MethodPost,
		s.url+"/operators/"+url.PathEscape(controller.String())+"/introspection", bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(operator.ContractVersionHeader, operator.ContractVersion)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, operator.ContractVersion, resp.Header.Get(operator.ContractVersionHeader))
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
	agents := agent.NewPostgresStore(s.pool)
	g := seedAgent(t, agents, grn.Org(controller.Organization))
	assignment, err := agents.Assign(context.Background(), g, controller)
	require.NoError(t, err)
	return g.String(), assignment.Epoch()
}

// selfSignedFor is a certificate naming subject that nothing but its
// own key signed: what anyone can make for a GRN they want to assert.
func selfSignedFor(t *testing.T, subject grn.GRN) string {
	t.Helper()
	uri, err := url.Parse(subject.String())
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, URIs: []*url.URL{uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// TestControllerProof_ProvesTheControllerItsCertificateNames is the
// session proof and the refusal of every party it is not: another
// controller, another organization's, a certificate naming another
// identity or signed by nothing, and another audience. The valid
// controller is the control, through the same listener and caller.
func TestControllerProof_ProvesTheControllerItsCertificateNames(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newProofStack(t, engine, p, policies)

	got := s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, "")
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.JSONEq(t, `{"operator":"`+s.controller.String()+`","org":"`+s.acme.String()+`","agent":null}`, got.body)

	got = s.prove(t, s.hostedTLS, s.other, s.otherPEM, "")
	assert.Equal(t, http.StatusForbidden, got.status, "a controller the grant does not name: %s", got.body)
	got = s.prove(t, s.hostedTLS, s.controller, s.otherPEM, "")
	assert.Equal(t, http.StatusUnprocessableEntity, got.status, "another controller's certificate: %s", got.body)
	got = s.prove(t, s.hostedTLS, s.controller, selfSignedFor(t, s.controller), "")
	assert.Equal(t, http.StatusUnprocessableEntity, got.status, "the controller's GRN in a certificate nobody issued: %s", got.body)
	got = s.prove(t, s.hostedTLS, s.globex, s.globexPEM, "")
	assert.Equal(t, http.StatusNotFound, got.status, "another organization's controller: %s", got.body)

	// The controller is registered and holds a valid certificate, but
	// no grant names it as an audience.
	controllerTLS := machineClient(t, installed(t, p, s.issuer, s.controller))
	got = s.prove(t, controllerTLS, s.controller, s.controllerPEM, "")
	assert.Equal(t, http.StatusForbidden, got.status, "another audience: %s", got.body)

	for _, line := range []string{s.logs.String(), got.body} {
		assert.NotContains(t, line, "BEGIN CERTIFICATE")
		assert.NotContains(t, line, strings.Split(s.controllerPEM, "\n")[1], "no part of a forwarded certificate")
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
	controllerTLS := machineClient(t, installed(t, p, s.issuer, s.controller))
	placed, epoch := s.provision(t, controllerTLS)

	got := s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, placed)
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.JSONEq(t, `{"operator":"`+s.controller.String()+`","org":"`+s.acme.String()+
		`","agent":{"grn":"`+placed+`","epoch":"`+strconv.Itoa(epoch)+`"}}`, got.body)

	elsewhere, _ := s.provision(t, machineClient(t, installed(t, p, s.issuer, s.other)))
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, elsewhere)
	assert.Equal(t, http.StatusForbidden, got.status, "an agent assigned to another controller: %s", got.body)
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, scopedGRN("acme", "agent", "nobody").String())
	assert.Equal(t, http.StatusNotFound, got.status, "an agent the organization does not have: %s", got.body)
	// globex's agent is real and placed, on globex's controller: what
	// refuses it is the organization, decided before its placement is
	// read.
	foreign, _ := s.provision(t, machineClient(t, installed(t, p, s.issuer, s.globex)))
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, foreign)
	assert.Equal(t, http.StatusNotFound, got.status, "another organization's agent: %s", got.body)

	// Moving the agent away and back raises the epoch: the proof answers
	// the current one, which is what the caller compares.
	agents := agent.NewPostgresStore(p)
	placedGRN, err := grn.Parse(placed)
	require.NoError(t, err)
	_, err = agents.Assign(context.Background(), placedGRN, s.other)
	require.NoError(t, err)
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, placed), http.StatusForbidden, "moved")
	back, err := agents.Assign(context.Background(), placedGRN, s.controller)
	require.NoError(t, err)
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, placed)
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.Contains(t, got.body, `"epoch":"`+strconv.Itoa(back.Epoch())+`"`)
	assert.NotEqual(t, epoch, back.Epoch())
}

// TestControllerProof_RefusesWithdrawalOnTheSameOpenConnection holds
// that a withdrawal refuses the next decision on a connection that
// authenticated before it: an ended grant, a deleted controller and the
// caller's own deleted registration. Each refusal follows a proof on
// the same connection, which is its control.
func TestControllerProof_RefusesWithdrawalOnTheSameOpenConnection(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newProofStack(t, engine, p, policies)

	require.Equal(t, http.StatusOK, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, "").status)
	require.NoError(t, s.grants.EndGrant(ctx, s.acme, s.hosted, s.grant, time.Now()))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, ""), http.StatusForbidden, "grant ended")

	s.delegate(t, "g-2", nil, time.Now().Add(time.Hour), s.controller, s.other)
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, ""), http.StatusOK, "granted again")
	require.NoError(t, operator.NewPostgresStore(p).Delete(ctx, s.controller))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, ""), http.StatusNotFound, "controller deleted")

	// The other controller, which the same grant names, is the control
	// for the caller's own deletion: it answers on this connection until
	// the caller goes.
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.other, s.otherPEM, ""), http.StatusOK, "the other controller")
	require.NoError(t, operator.NewPostgresStore(p).Delete(ctx, s.hosted))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.other, s.otherPEM, ""), http.StatusForbidden, "caller deleted")
}

// TestControllerProof_ReadsTheTimeOnEveryDecision moves the listener's
// clock across three expiries on one open connection: the forwarded
// controller certificate's, the grant's, and the caller's own TLS
// certificate's. Each refusal is read against a certificate or caller
// the same move leaves valid.
func TestControllerProof_ReadsTheTimeOnEveryDecision(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newProofStack(t, engine, p, policies)
	start := s.clock.now()

	// A controller certificate issued half a lifetime ago runs out
	// first; one issued a quarter of a lifetime from now outlives the
	// caller's.
	issuedAt := func(at time.Time) *x509.Certificate {
		svc, err := ca.NewService(capg.New(p), s.sealer, ca.Config{Now: func() time.Time { return at }})
		require.NoError(t, err)
		issued, err := svc.Issue(context.Background(), s.controller)
		require.NoError(t, err)
		leaf, err := x509.ParseCertificate(mustPEMBlock(t, issued.CertificatePEM()))
		require.NoError(t, err)
		return leaf
	}
	early := issuedAt(start.Add(-ca.DefaultCertificateLifetime / 2))
	late := issuedAt(start.Add(ca.DefaultCertificateLifetime / 4))
	caller := s.hostedLeaf
	require.True(t, early.NotAfter.Before(caller.NotAfter) && caller.NotAfter.Before(late.NotAfter),
		"early %s, caller %s, late %s", early.NotAfter, caller.NotAfter, late.NotAfter)
	require.True(t, late.NotBefore.Before(early.NotAfter))
	earlyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: early.Raw}))
	latePEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: late.Raw}))

	require.Equal(t, http.StatusOK, s.prove(t, s.hostedTLS, s.controller, earlyPEM, "").status)

	s.clock.set(early.NotAfter.Add(time.Second))
	second := s.delegate(t, "g-2", &s.grant, early.NotAfter.Add(time.Hour), s.controller)
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, earlyPEM, ""), http.StatusForbidden, "forwarded certificate expired")
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, latePEM, ""), http.StatusOK, "a forwarded certificate still valid")

	s.clock.set(early.NotAfter.Add(time.Hour + time.Second))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, latePEM, ""), http.StatusForbidden, "grant expired")

	s.delegate(t, "g-3", &second, caller.NotAfter.Add(time.Hour), s.controller)
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, latePEM, ""), http.StatusOK, "granted past the caller's expiry")
	s.clock.set(caller.NotAfter.Add(time.Second))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, latePEM, ""), http.StatusForbidden, "caller certificate expired")
}

func mustPEMBlock(t *testing.T, raw []byte) []byte {
	t.Helper()
	block, _ := pem.Decode(raw)
	require.NotNil(t, block)
	return block.Bytes
}

// TestControllerProof_DecidesOnCurrentPolicyPastACachedAllow holds that
// the proof is decided on the hosted operator's groups as they are now
// and on persisted policy past the engine's cache. A Deny attached to a
// group the operator is not in leaves the proof answering, which is the
// control for adding it to that group; a Deny another process writes
// refuses while the ordinary Check still answers its cached allow; and
// a Deny on one agent refuses that agent's proof and not the session.
func TestControllerProof_DecidesOnCurrentPolicyPastACachedAllow(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newProofStack(t, engine, p, policies)
	orgs := orgpg.New(p)
	controllerTLS := machineClient(t, installed(t, p, s.issuer, s.controller))
	placed, _ := s.provision(t, controllerTLS)

	team := scopedGRN("acme", org.ServiceTypeGroup, "controllers")
	require.NoError(t, orgs.CreateGroup(ctx, org.NewGroup(team, "controllers", s.acme)))
	denied := activePolicy(t, policies, s.acme, "deny-activate",
		statementDoc("deny-activate", wire.EffectDeny, "agent:activate", s.acme.String()))
	attachPolicy(t, policies, denied, team)
	got := s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, "")
	require.Equal(t, http.StatusOK, got.status, "not in the denied group: %s", got.body)
	require.NoError(t, orgs.AddGroupMember(ctx, team, s.hosted))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, ""), http.StatusForbidden, "added to the denied group")
	require.NoError(t, orgs.RemoveGroupMember(ctx, team, s.hosted))
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, ""), http.StatusOK, "removed again")

	check := &authzv1.CheckRequest{
		SubjectGrn: s.hosted.String(), ObjectGrn: s.acme.String(), Action: "agent:activate",
		Statements: []*authzv1.Statement{{Sid: "delegation", Effect: authzv1.Effect_EFFECT_ALLOW,
			Action: []string{"agent:activate"}, Resource: []string{s.acme.String()}}},
		SubjectOrg: s.acme.String(),
	}
	client := authzv1.NewAuthzServiceClient(engine)
	cached, err := client.Check(ctx, check)
	require.NoError(t, err)
	require.True(t, cached.GetAllowed())
	// Another process's write: it drops its own cache, not the engine's.
	store := authz.NewPostgresStore(p)
	elsewhere := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))
	attachPolicy(t, elsewhere, activePolicy(t, elsewhere, s.acme, "deny-agent",
		statementDoc("deny-agent", wire.EffectDeny, "agent:activate", placed)), s.hosted)
	stale, err := client.Check(ctx, &authzv1.CheckRequest{
		SubjectGrn: s.hosted.String(), ObjectGrn: placed, Action: "agent:activate",
		Statements: []*authzv1.Statement{{Sid: "delegation", Effect: authzv1.Effect_EFFECT_ALLOW,
			Action: []string{"agent:activate"}, Resource: []string{placed}}},
		SubjectOrg: s.acme.String(),
	})
	require.NoError(t, err)
	require.True(t, stale.GetAllowed(), "the control: the ordinary check still answers its cached allow")

	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, placed), http.StatusForbidden, "a Deny on the agent")
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, ""), http.StatusOK, "the session, which the Deny does not name")
}

// TestControllerProof_UnknownFreshnessRefusesAsUnavailable holds that
// authority garam cannot read as current is 503 naming no cause: an
// engine predating CheckCurrent, and a database that stops answering
// under an open connection. The proof each refuses after is the
// control, on the same connection.
func TestControllerProof_UnknownFreshnessRefusesAsUnavailable(t *testing.T) {
	p := pool(t)
	srv := grpc.NewServer()
	current := authz.NewGRPCServer(authz.NewPolicyCache(authz.NewPostgresStore(p)))
	gate := &engineGate{current: current}
	authzv1.RegisterAuthzServiceServer(srv, gate)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	engine, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = engine.Close() })
	s := newProofStack(t, engine, p, nil)

	require.Equal(t, http.StatusOK, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, "").status)
	gate.checkOnly()
	got := s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, "")
	onOpenConnection(t, got, http.StatusServiceUnavailable, "an engine without CheckCurrent")
	assert.Contains(t, got.body, `"message":"internal error"`)

	gate.restore()
	onOpenConnection(t, s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, ""), http.StatusOK, "the engine restored")
	p.Close()
	got = s.prove(t, s.hostedTLS, s.controller, s.controllerPEM, "")
	onOpenConnection(t, got, http.StatusServiceUnavailable, "a database that stopped answering")
	assert.Contains(t, got.body, `"message":"internal error"`)
}

// engineGate serves the current engine until checkOnly, after which it
// answers as an engine predating CheckCurrent does: UNIMPLEMENTED.
type engineGate struct {
	authzv1.UnimplementedAuthzServiceServer
	mu      sync.Mutex
	old     bool
	current authzv1.AuthzServiceServer
}

func (g *engineGate) checkOnly() { g.mu.Lock(); g.old = true; g.mu.Unlock() }
func (g *engineGate) restore()   { g.mu.Lock(); g.old = false; g.mu.Unlock() }

func (g *engineGate) Check(ctx context.Context, r *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	return g.current.Check(ctx, r)
}

func (g *engineGate) CheckCurrent(ctx context.Context, r *authzv1.CheckCurrentRequest) (*authzv1.CheckCurrentResponse, error) {
	g.mu.Lock()
	old := g.old
	g.mu.Unlock()
	if old {
		return g.UnimplementedAuthzServiceServer.CheckCurrent(ctx, r)
	}
	return g.current.CheckCurrent(ctx, r)
}
