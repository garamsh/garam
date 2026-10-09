package integration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apigen "github.com/garamsh/garam/api"
	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// shiftedClock is the time the machine listener verifies a handshake
// and a lifecycle caller against: now, moved by an offset the test
// sets. The database's own clock is never moved.
type shiftedClock struct {
	mu sync.Mutex
	by time.Duration
}

func (c *shiftedClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.by)
}

func (c *shiftedClock) shift(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.by = by
}

// executionStack is both listeners as the serve commands compose them,
// over one database: acme's owner and member on the browser surface;
// on the machine surface the hosted control service, whose grant
// covers agent:activate, agent:recover, agent:configure and
// agent:cutover for the controller, the controller itself, and an
// agent the controller created with the certificate the legacy route
// issued it. A second agent of the controller posts to the first.
type executionStack struct {
	pool    *pgxpool.Pool
	browser string
	machine string
	// server is the machine listener serving at machine, composed from
	// config, which restart composes the next one from.
	server     *http.Server
	config     cli.MachineConfig
	clock      *shiftedClock
	acme       grn.GRN
	owner      *http.Cookie
	member     *http.Cookie
	hosted     grn.GRN
	controller grn.GRN
	hostedTLS  *http.Client
	controlTLS *http.Client
	issuer     ca.Service
	// past issues as of three hours ago. The organization's authority
	// is created through it first, so a certificate that expired before
	// the test began still chains to it.
	past     ca.Service
	agent    grn.GRN
	epoch    int
	leaf     string
	agentTLS *http.Client
	peer     grn.GRN
	peerTLS  *http.Client
	keys     atomic.Int64
	// grant is the hosted control service's current grant, which the
	// next delegation replaces.
	grant *string
}

// newExecutionStack is the stack once its agent's cutover is switched
// and before any activation: a legacy agent's first activation waits
// for its switch (ADR-0086).
func newExecutionStack(t *testing.T, p *pgxpool.Pool, engine grpc.ClientConnInterface) *executionStack {
	t.Helper()
	s := newLegacyStack(t, p, engine)
	s.cutOver(t, s.agent)
	return s
}

// newLegacyStack is the stack before its agent's cutover.
func newLegacyStack(t *testing.T, p *pgxpool.Pool, engine grpc.ClientConnInterface) *executionStack {
	t.Helper()
	ctx := context.Background()
	orgs := org.NewPostgresStore(p)
	users := userpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))
	session := func(id string, role org.Role) *http.Cookie {
		g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
		require.NoError(t, users.Create(ctx, user.New(g, id)))
		require.NoError(t, orgs.AddMember(ctx, org.NewMembership(acme, g, role)))
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, authn.NewPostgresSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}
	}

	browserSrv := api.NewServer(users, orgs, nil, operator.NewPostgresStore(p), nil, nil,
		authn.NewPostgresSessionStore(p), nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), nil, nil, cli.NewAuthorityService(p, engine), cli.NewStandaloneService(p, engine),
		api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(browserSrv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)

	_, sealer := keyEncryptionKey(t)
	at := time.Now().Add(-3 * time.Hour)
	past, err := ca.NewService(capg.New(p), sealer, ca.Config{
		CertificateLifetime: ca.MinCertificateLifetime, Now: func() time.Time { return at },
	})
	require.NoError(t, err)
	issuer, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	hosted := scopedGRN("acme", operator.ServiceType, "hosted")
	_, err = past.Issue(ctx, hosted)
	require.NoError(t, err)

	clock := &shiftedClock{}
	presented := loopbackCertificate(t)
	s := &executionStack{
		pool: p, browser: browser.URL, clock: clock,
		config: cli.MachineConfig{
			Sealer: sealer, Issuer: testIssuer, Certificate: presented, ServerRoot: certificatePEM(presented),
			Delivery: briefDelivery(), Engine: engine,
			Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Now: clock.now,
		},
		acme: acme, owner: session("ada", org.RoleOwner), member: session("bob", org.RoleMember),
		hosted: hosted, controller: scopedGRN("acme", operator.ServiceType, "controller"),
		issuer: issuer, past: past,
	}
	s.serve(t)
	s.hostedTLS = machineClient(t, installed(t, p, issuer, s.hosted))
	s.controlTLS = machineClient(t, installed(t, p, issuer, s.controller))
	s.delegate(t, "agent:activate", "agent:recover", "agent:configure", "agent:cutover")

	status, body := machineRequest(t, s.controlTLS, http.MethodPost, s.machine+"/agents")
	require.Equal(t, http.StatusCreated, status, string(body))
	var created machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal(body, &created))
	s.agent, err = grn.Parse(created.Grn)
	require.NoError(t, err)
	s.epoch = created.Epoch
	issued := issuedTo(t, s.controlTLS, s.machine, created.Grn)
	s.leaf = issued.CertificatePem
	s.agentTLS = machineClient(t, pairOf(t, issued))
	s.peer, s.peerTLS = provisioned(t, s.machine, s.controlTLS)
	return s
}

// serve starts a machine listener composed from s.config, as `serve
// machine` composes it, on a port of its own.
func (s *executionStack) serve(t *testing.T) {
	t.Helper()
	srv, err := cli.NewMachineServer(s.pool, s.config)
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	s.server, s.machine = srv, "https://"+lis.Addr().String()
}

// restart stops the machine listener, closing every connection it
// held, and serves a new one over the same database.
func (s *executionStack) restart(t *testing.T) {
	t.Helper()
	require.NoError(t, s.server.Close())
	s.serve(t)
}

// delegate writes the hosted control service's grant as the owner,
// replacing the one it wrote before.
func (s *executionStack) delegate(t *testing.T, operations ...string) {
	t.Helper()
	s.delegateTo(t, []grn.GRN{s.controller}, operations...)
}

// delegateTo is delegate for the controllers named.
func (s *executionStack) delegateTo(t *testing.T, controllers []grn.GRN, operations ...string) {
	t.Helper()
	names := make([]string, 0, len(controllers))
	for _, c := range controllers {
		names = append(names, c.String())
	}
	raw, err := json.Marshal(map[string]any{
		"requestId": s.key(), "controllers": names, "operations": operations,
		"expiresAt":       time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano),
		"expectedGrantId": s.grant,
	})
	require.NoError(t, err)
	status, got := call(t, http.DefaultClient, http.MethodPut, s.browser+"/orgs/acme/operators/hosted/delegation", s.owner, string(raw))
	require.Equal(t, http.StatusCreated, status, got)
	var d apigen.Delegation
	require.NoError(t, json.Unmarshal([]byte(got), &d))
	s.grant = &d.GrantId
}

// reply is one answer, and whether it came over a connection an
// earlier request opened.
type reply struct {
	status  int
	body    string
	version string
	reused  bool
}

// exchange performs one request. version names a contract version and
// authorization an Authorization header; either is left off when
// empty. It reports failure rather than failing the test, so a
// goroutine can call it.
func (s *executionStack) exchange(client *http.Client, method, path, version, authorization, body string) (reply, error) {
	var got reply
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { got.reused = info.Reused }}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(context.Background(), trace), method,
		s.machine+path, strings.NewReader(body))
	if err != nil {
		return reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if version != "" {
		req.Header.Set(operator.ContractVersionHeader, version)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	if err != nil {
		return reply{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	if err != nil {
		return reply{}, err
	}
	got.status, got.body, got.version = resp.StatusCode, string(answer), resp.Header.Get(operator.ContractVersionHeader)
	return got, nil
}

func (s *executionStack) send(t *testing.T, client *http.Client, method, path, version, authorization, body string) reply {
	t.Helper()
	got, err := s.exchange(client, method, path, version, authorization, body)
	require.NoError(t, err)
	return got
}

// lifecycle sends one execution-fence.v1 request, whose every answer
// names that version.
func (s *executionStack) lifecycle(t *testing.T, client *http.Client, path, authorization, body string) reply {
	t.Helper()
	got := s.send(t, client, http.MethodPost, path, agent.ExecutionContractVersion, authorization, body)
	assert.Equal(t, agent.ExecutionContractVersion, got.version, "every execution answer names its contract")
	return got
}

func (s *executionStack) agentPath(suffix string) string {
	return "/agents/" + s.agent.String() + suffix
}

func activationBody(t *testing.T, requestID string, epoch int, generation agent.Generation, replaces *string, leaf string) string {
	t.Helper()
	return activationBodyOf(requestID, epoch, generation, replaces, leaf)
}

// activationBodyOf is activationBody for a goroutine, which cannot fail
// the test; a map of strings always marshals.
func activationBodyOf(requestID string, epoch int, generation agent.Generation, replaces *string, leaf string) string {
	raw, _ := json.Marshal(map[string]any{
		"requestId": requestID, "epoch": strconv.Itoa(epoch), "generation": string(generation),
		"replacesActivationId": replaces, "operationRef": nil, "certificatePem": leaf,
	})
	return string(raw)
}

func (s *executionStack) activate(t *testing.T, client *http.Client, requestID string, g agent.Generation, replaces *string, leaf string) reply {
	t.Helper()
	return s.lifecycle(t, client, s.agentPath("/activations"), "", activationBody(t, requestID, s.epoch, g, replaces, leaf))
}

// activated activates g for the agent and answers the activation.
func (s *executionStack) activated(t *testing.T, requestID string, g agent.Generation, replaces *string, leaf string) machinegen.Activation {
	t.Helper()
	got := s.activate(t, s.hostedTLS, requestID, g, replaces, leaf)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var a machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	return a
}

func (s *executionStack) introspect(t *testing.T, client *http.Client, leaf string, g *agent.Generation) reply {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"certificatePem": leaf, "generation": g})
	require.NoError(t, err)
	return s.lifecycle(t, client, s.agentPath("/execution/introspection"), "", string(raw))
}

// standing answers what introspection says of leaf and g.
func (s *executionStack) standing(t *testing.T, leaf string, g *agent.Generation) machinegen.ExecutionIntrospection {
	t.Helper()
	got := s.introspect(t, s.hostedTLS, leaf, g)
	require.Equal(t, http.StatusOK, got.status, got.body)
	var in machinegen.ExecutionIntrospection
	require.NoError(t, json.Unmarshal([]byte(got.body), &in))
	return in
}

func (s *executionStack) deactivate(t *testing.T, id string) reply {
	t.Helper()
	return s.lifecycle(t, s.hostedTLS, s.agentPath("/activations/"+id+"/deactivation"), "", "")
}

// recovery is a credential recovery prepared as the Operator prepares
// one: a key made in the cluster, the exact request body over it, and
// the owner's agent:recover handoff for that body.
type recovery struct {
	key       *ecdsa.PrivateKey
	body      string
	authority string
}

func (s *executionStack) prepareRecovery(t *testing.T, requestID string) recovery {
	t.Helper()
	request, key := enrollmentRequest(t)
	raw, err := json.Marshal(map[string]string{
		"requestId": requestID, "epoch": strconv.Itoa(s.epoch), "certificateRequestPem": request,
	})
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	status, got := s.mintHandoff(t, s.owner, hex.EncodeToString(digest[:]), "h-"+requestID)
	require.Equal(t, http.StatusCreated, status, got)
	return recovery{key: key, body: string(raw), authority: authorityOf(t, got).Authority}
}

// mintHandoff asks for an agent:recover handoff of the agent as the
// user cookie names, bound to digest.
func (s *executionStack) mintHandoff(t *testing.T, cookie *http.Cookie, digest, requestID string) (int, string) {
	t.Helper()
	body := `{"audience":"` + s.hosted.String() + `","operation":"agent:recover","target":"` + s.agent.String() +
		`","requestId":"` + requestID + `","bodySha256":"` + digest + `"}`
	return call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", cookie, body)
}

func (s *executionStack) recover(t *testing.T, r recovery, body string) reply {
	t.Helper()
	return s.lifecycle(t, s.hostedTLS, s.agentPath("/credential-recovery"), "Garam-Operation "+r.authority, body)
}

// recovered answers the client and leaf of the certificate a recovery
// issued, over the key it was prepared with, once the answer's
// certificate has verified against the issuer the same answer names.
func recovered(t *testing.T, got reply, key *ecdsa.PrivateKey) (*http.Client, string) {
	t.Helper()
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var issued machinegen.RecoveredCredential
	require.NoError(t, json.Unmarshal([]byte(got.body), &issued))
	require.NoError(t, chainsToIssuer(t, issued), "the recovered certificate chains to the issuer its answer names")
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair([]byte(issued.CertificatePem), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)
	return machineClient(t, pair), issued.CertificatePem
}

// chainsToIssuer verifies a recovered certificate against the issuer
// its answer names, as a holder with nothing but the answer would.
func chainsToIssuer(t *testing.T, issued machinegen.RecoveredCredential) error {
	t.Helper()
	issuerBlock, _ := pem.Decode([]byte(issued.IssuerPem))
	require.NotNil(t, issuerBlock, "the answer names its issuer")
	issuer, err := x509.ParseCertificate(issuerBlock.Bytes)
	require.NoError(t, err)
	leafBlock, _ := pem.Decode([]byte(issued.CertificatePem))
	require.NotNil(t, leafBlock)
	leaf, err := x509.ParseCertificate(leafBlock.Bytes)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(issuer)
	_, err = leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	return err
}

func bearer(token string) string {
	if token == "" {
		return ""
	}
	return "Garam-Execution " + token
}

// absentClaim is a well-formed claim no message holds. A refused call
// is refused before its claim is looked up, so it names this one.
const absentClaim = "00000000-0000-0000-0000-000000000000"

// protectedCalls makes every call the execution fence protects once,
// as client carrying token, and answers each by route. The peer first
// posts a message for the claim to take; renewal and completion act on
// that claim when it was taken, and on absentClaim when it was not.
func (s *executionStack) protectedCalls(t *testing.T, client *http.Client, token string) map[string]reply {
	t.Helper()
	got := s.send(t, s.peerTLS, http.MethodPost, "/messages", "", "", submissionTo(s.agent, s.key()))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	auth := bearer(token)

	out := map[string]reply{}
	out["claim"] = s.send(t, client, http.MethodPost, s.agentPath("/claims"), "", auth, "")
	claim := absentClaim
	if out["claim"].status == http.StatusOK {
		var c machinegen.Claim
		require.NoError(t, json.Unmarshal([]byte(out["claim"].body), &c))
		claim = c.Id
	}
	out["renewal"] = s.send(t, client, http.MethodPost, s.agentPath("/claims/"+claim+"/renewal"), "", auth, "")
	out["completion"] = s.send(t, client, http.MethodPost, s.agentPath("/claims/"+claim+"/completion"), "", auth, "")
	out["post"] = s.send(t, client, http.MethodPost, "/messages", "", auth, submissionTo(s.peer, s.key()))
	posted := absentClaim
	if out["post"].status == http.StatusCreated {
		var m machinegen.Message
		require.NoError(t, json.Unmarshal([]byte(out["post"].body), &m))
		posted = m.Id
	}
	out["status"] = s.send(t, client, http.MethodGet, "/messages/"+posted, "", auth, "")
	out["runtime-state"] = s.send(t, client, http.MethodPost, s.agentPath("/runtime-state"), "", auth, reportBody(s.epoch, "connected"))
	out["turn-event"] = s.send(t, client, http.MethodPost, s.agentPath("/turn-events"), "", auth, `{"kind":"turn.start","body":"b"}`)
	out["certificate"] = s.send(t, client, http.MethodPost, s.agentPath("/certificate"), "", auth, "")
	return out
}

func (s *executionStack) key() string { return "key-" + strconv.FormatInt(s.keys.Add(1), 10) }

// admittedStatus is what each protected call answers when admitted.
var admittedStatus = map[string]int{
	"claim": http.StatusOK, "renewal": http.StatusOK, "completion": http.StatusNoContent,
	"post": http.StatusCreated, "status": http.StatusOK, "runtime-state": http.StatusNoContent,
	"turn-event": http.StatusAccepted, "certificate": http.StatusCreated,
}

// allAdmitted asserts every protected call was admitted, over a
// connection an earlier request opened.
func allAdmitted(t *testing.T, got map[string]reply, msg string) {
	t.Helper()
	require.Len(t, got, len(admittedStatus))
	for route, r := range got {
		assert.Equal(t, admittedStatus[route], r.status, "%s: %s: %s", msg, route, r.body)
		assert.True(t, r.reused, "%s: %s reused an open connection", msg, route)
	}
}

// allRefused asserts every call in got was refused with status,
// over a connection an earlier request opened.
func allRefused(t *testing.T, got map[string]reply, status int, msg string) {
	t.Helper()
	require.NotEmpty(t, got)
	for route, r := range got {
		assert.Equal(t, status, r.status, "%s: %s: %s", msg, route, r.body)
		assert.True(t, r.reused, "%s: %s reused an open connection", msg, route)
	}
}

// open makes a request no fence decides on client, so the next one
// reuses the connection it authenticated.
func (s *executionStack) open(t *testing.T, client *http.Client) {
	t.Helper()
	got := s.send(t, client, http.MethodGet, "/identity", "", "", "")
	require.Equal(t, http.StatusOK, got.status, got.body)
}

func generationOf(n int) agent.Generation {
	return agent.Generation(strings.Repeat("0", 31) + strconv.Itoa(n))
}

// TestExecutionFence_AnActivationFencesEveryProtectedRouteOnAnOpenConnection
// carries a GRN from the legacy cohort through cutover, a retried
// activation, a replacement and a deactivation, with the agent on one
// TLS connection throughout. Every protected route is refused the
// token each step supersedes and admits the one it issues.
func TestExecutionFence_AnActivationFencesEveryProtectedRouteOnAnOpenConnection(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	s.open(t, s.agentTLS)

	// The legacy cohort needs no token, and offering one is not how a
	// GRN cuts over.
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, ""), "legacy")
	allRefused(t, s.protectedCalls(t, s.agentTLS, strings.Repeat("A", 43)), http.StatusConflict, "a token on a legacy GRN")
	in := s.standing(t, s.leaf, nil)
	assert.Equal(t, machinegen.ExecutionIntrospectionModeLegacy, in.Mode)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, in.Credential)
	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationNone, in.Generation)
	assert.Nil(t, in.ActivationId)

	// Only the hosted control service activates: not the agent's own
	// certificate, not the controller, and not another organization's
	// operator.
	globexTLS := machineClient(t, installed(t, p, s.issuer, scopedGRN("globex", operator.ServiceType, "hosted")))
	for name, client := range map[string]*http.Client{"agent": s.agentTLS, "controller": s.controlTLS, "globex": globexTLS} {
		got := s.activate(t, client, "r-1", generationOf(1), nil, s.leaf)
		assert.Equal(t, http.StatusForbidden, got.status, "%s: %s", name, got.body)
	}

	first := s.activated(t, "r-1", generationOf(1), nil, s.leaf)
	assert.Equal(t, s.agent.String(), first.Grn)
	assert.Equal(t, strconv.Itoa(s.epoch), first.Epoch)
	assert.Nil(t, first.Lineage, "the legacy cohort carries no lineage")
	allRefused(t, s.protectedCalls(t, s.agentTLS, ""), http.StatusUnauthorized, "no token after cutover")
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, first.Token), "the activation's token")

	// A retry whose answer was lost gets the same activation and a new
	// token; only the newest is admitted.
	retried := s.activate(t, s.hostedTLS, "r-1", generationOf(1), nil, s.leaf)
	require.Equal(t, http.StatusOK, retried.status, retried.body)
	var again machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(retried.body), &again))
	assert.Equal(t, first.ActivationId, again.ActivationId)
	assert.Greater(t, again.TokenVersion, first.TokenVersion)
	allRefused(t, s.protectedCalls(t, s.agentTLS, first.Token), http.StatusForbidden, "the retried activation's earlier token")
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, again.Token), "the retry's token")

	// The operator route no longer issues this agent's certificate.
	got := s.send(t, s.controlTLS, http.MethodPost, s.agentPath("/certificate"), "", "", "")
	assert.Equal(t, http.StatusConflict, got.status, got.body)

	// A replacement fences the generation it replaces; a replay of the
	// first activation's placement does not win.
	second := s.activated(t, "r-2", generationOf(2), &first.ActivationId, s.leaf)
	allRefused(t, s.protectedCalls(t, s.agentTLS, again.Token), http.StatusForbidden, "the replaced generation's token")
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, second.Token), "the replacement's token")
	got = s.activate(t, s.hostedTLS, "r-3", generationOf(3), nil, s.leaf)
	assert.Equal(t, http.StatusConflict, got.status, "a replay naming no predecessor: %s", got.body)
	got = s.activate(t, s.hostedTLS, "r-3", generationOf(2), &second.ActivationId, s.leaf)
	assert.Equal(t, http.StatusConflict, got.status, "a generation already active in this lineage: %s", got.body)

	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationNotCurrent, s.standing(t, s.leaf, ptr(generationOf(1))).Generation)
	in = s.standing(t, s.leaf, ptr(generationOf(2)))
	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationCurrent, in.Generation)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, in.Credential)
	assert.Equal(t, machinegen.ExecutionIntrospectionModeFenced, in.Mode)
	assert.Equal(t, s.controller.String(), in.Assignee)
	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationNotSupplied, s.standing(t, s.leaf, nil).Generation)

	// Deactivation ends the current token and is answered the same when
	// replayed; the next activation still replaces the deactivated one.
	got = s.deactivate(t, second.ActivationId)
	require.Equal(t, http.StatusOK, got.status, got.body)
	allRefused(t, s.protectedCalls(t, s.agentTLS, second.Token), http.StatusForbidden, "a deactivated token")
	got = s.deactivate(t, second.ActivationId)
	assert.Equal(t, http.StatusOK, got.status, "a replayed deactivation: %s", got.body)
	got = s.deactivate(t, absentClaim)
	assert.Equal(t, http.StatusNotFound, got.status, "an activation nobody made: %s", got.body)
	third := s.activated(t, "r-4", generationOf(4), &second.ActivationId, s.leaf)
	got = s.deactivate(t, first.ActivationId)
	assert.Equal(t, http.StatusOK, got.status, "an ended activation replayed after its successor: %s", got.body)
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, third.Token), "the successor survives the replay")
	got = s.activate(t, s.hostedTLS, "r-5", generationOf(5), &first.ActivationId, s.leaf)
	assert.Equal(t, http.StatusConflict, got.status, "the replayed ID is not the anchor: %s", got.body)
	s.activated(t, "r-5", generationOf(5), &third.ActivationId, s.leaf)
}

// TestExecutionFence_RecoveryFencesTheOldLineageOnAnOpenConnection
// holds that an explicit recovery replaces the credential lineage:
// the old certificate, still inside its validity and on the connection
// it opened, is refused even with a current token, while the recovered
// one is admitted with that same token.
func TestExecutionFence_RecoveryFencesTheOldLineageOnAnOpenConnection(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	s.open(t, s.agentTLS)
	first := s.activated(t, "r-1", generationOf(1), nil, s.leaf)

	status, got := s.mintHandoff(t, s.member, strings.Repeat("c", 64), "h-member")
	assert.Equal(t, http.StatusForbidden, status, "a member does not recover: %s", got)
	// A grant that stops listing agent:recover ends the handoff minted
	// under it; one minted under the restored grant recovers.
	withdrawn := s.prepareRecovery(t, "rec-0")
	s.delegate(t, "agent:activate")
	refused := s.recover(t, withdrawn, withdrawn.body)
	assert.Equal(t, http.StatusForbidden, refused.status, refused.body)
	s.delegate(t, "agent:activate", "agent:recover", "agent:configure")
	r := s.prepareRecovery(t, "rec-1")

	changed := s.recover(t, r, r.body+" ")
	assert.Equal(t, http.StatusConflict, changed.status, "a byte the handoff does not cover: %s", changed.body)
	answer := s.recover(t, r, r.body)
	recoveredTLS, recoveredLeaf := recovered(t, answer, r.key)
	var named machinegen.RecoveredCredential
	require.NoError(t, json.Unmarshal([]byte(answer.body), &named))
	assert.Equal(t, string(s.config.ServerRoot), named.ServerRootPem, "the answer names the server root")
	other, err := s.issuer.Issue(context.Background(), scopedGRN("globex", operator.ServiceType, "other"))
	require.NoError(t, err)
	substituted := named
	substituted.IssuerPem = string(other.IssuerPEM())
	assert.Error(t, chainsToIssuer(t, substituted), "control: another organization's issuer verifies nothing")
	retry := s.recover(t, r, r.body)
	assert.Equal(t, http.StatusCreated, retry.status, retry.body)
	assert.JSONEq(t, answer.body, retry.body, "a retry answers the stored certificate and chain")
	forgetChain(t, s.pool, s.agent.String(), "rec-1")
	legacy := s.recover(t, r, r.body)
	recovered(t, legacy, r.key)
	assert.JSONEq(t, answer.body, legacy.body,
		"a recovery recorded before 000036 answers the chain the authority signs under now")
	second := s.prepareRecovery(t, "rec-2")
	got2 := s.recover(t, r, second.body)
	assert.Equal(t, http.StatusConflict, got2.status, "a second recovery under a spent handoff: %s", got2.body)

	// The recovery ended the activation, so its token admits nothing.
	// Renewal reads the certificate's standing before it signs, so it
	// answers that first; every other call answers the token.
	ended := s.protectedCalls(t, s.agentTLS, first.Token)
	assert.Equal(t, http.StatusConflict, ended["certificate"].status, ended["certificate"].body)
	delete(ended, "certificate")
	allRefused(t, ended, http.StatusForbidden, "the token the recovery ended")

	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialFenced, s.standing(t, s.leaf, nil).Credential)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, s.standing(t, recoveredLeaf, nil).Credential)
	stale := s.activate(t, s.hostedTLS, "r-2", generationOf(2), &first.ActivationId, s.leaf)
	assert.Equal(t, http.StatusConflict, stale.status, "the old lineage activates nothing: %s", stale.body)

	// The same generation activates again in the new lineage.
	next := s.activated(t, "r-3", generationOf(1), &first.ActivationId, recoveredLeaf)
	require.NotNil(t, next.Lineage)
	allRefused(t, s.protectedCalls(t, s.agentTLS, next.Token), http.StatusConflict, "the old certificate with the current token")
	s.open(t, recoveredTLS)
	allAdmitted(t, s.protectedCalls(t, recoveredTLS, next.Token), "the recovered certificate with the same token")
}

// TestExecutionFence_AMutationQueuedBehindItsReplacementCannotCommit
// pauses an agent's post after it authenticated, on its open
// connection, waiting for the agent's execution row. Each act that
// ends what the post was admitted under is queued ahead of it and
// commits; the post then resumes and is refused, and nothing it would
// have written is stored. The same post queued ahead of the same act is
// the control: it commits first and the act still succeeds.
func TestExecutionFence_AMutationQueuedBehindItsReplacementCannotCommit(t *testing.T) {
	type act struct {
		name      string
		cutOver   bool
		do        func(t *testing.T, s *executionStack, current *machinegen.Activation, r recovery) (reply, error)
		succeeded int
		refused   int
	}
	acts := []act{
		{name: "cutover", refused: http.StatusUnauthorized, succeeded: http.StatusCreated,
			do: func(t *testing.T, s *executionStack, _ *machinegen.Activation, _ recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations"), agent.ExecutionContractVersion, "",
					activationBody(t, "race", s.epoch, generationOf(9), nil, s.leaf))
			}},
		{name: "replacement", cutOver: true, refused: http.StatusForbidden, succeeded: http.StatusCreated,
			do: func(t *testing.T, s *executionStack, current *machinegen.Activation, _ recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations"), agent.ExecutionContractVersion, "",
					activationBody(t, "race", s.epoch, generationOf(9), &current.ActivationId, s.leaf))
			}},
		{name: "deactivation", cutOver: true, refused: http.StatusForbidden, succeeded: http.StatusOK,
			do: func(_ *testing.T, s *executionStack, current *machinegen.Activation, _ recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations/"+current.ActivationId+"/deactivation"),
					agent.ExecutionContractVersion, "", "")
			}},
		{name: "recovery", cutOver: true, refused: http.StatusForbidden, succeeded: http.StatusCreated,
			do: func(_ *testing.T, s *executionStack, _ *machinegen.Activation, r recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/credential-recovery"), agent.ExecutionContractVersion,
					"Garam-Operation "+r.authority, r.body)
			}},
		{name: "reassignment", cutOver: true, refused: http.StatusForbidden, succeeded: http.StatusOK,
			do: func(_ *testing.T, s *executionStack, _ *machinegen.Activation, _ recovery) (reply, error) {
				_, err := agent.NewPostgresStore(s.pool).Assign(context.Background(), s.agent, scopedGRN("acme", operator.ServiceType, "other"))
				return reply{status: http.StatusOK}, err
			}},
	}
	for _, a := range acts {
		for _, behind := range []bool{true, false} {
			order := "the post queued behind the " + a.name
			if !behind {
				order = "control: the post queued ahead of the " + a.name
			}
			t.Run(order, func(t *testing.T) {
				p := pool(t)
				_, engine := policyEngine(t, p)
				s := newExecutionStack(t, p, engine)
				var current *machinegen.Activation
				token := ""
				if a.cutOver {
					activated := s.activated(t, "r-1", generationOf(1), nil, s.leaf)
					current, token = &activated, activated.Token
				}
				r := s.prepareRecovery(t, "rec-1")
				s.open(t, s.agentTLS)
				key := s.key()
				post := func() (reply, error) {
					return s.exchange(s.agentTLS, http.MethodPost, "/messages", "", bearer(token), submissionTo(s.peer, key))
				}
				replace := func() (reply, error) { return a.do(t, s, current, r) }

				var posted, replaced reply
				if behind {
					replaced, posted = s.queued(t, replace, post)
					assert.Equal(t, a.refused, posted.status, "the post resumed after the %s committed: %s", a.name, posted.body)
					assert.Zero(t, s.stored(t, key), "the refused post stored its message")
				} else {
					posted, replaced = s.queued(t, post, replace)
					assert.Equal(t, http.StatusCreated, posted.status, posted.body)
					assert.Equal(t, 1, s.stored(t, key), "the admitted post committed")
				}
				assert.True(t, posted.reused, "the post was made on the connection it had authenticated")
				assert.Equal(t, a.succeeded, replaced.status, replaced.body)
			})
		}
	}
}

// queued holds the agent's execution row, queues first and then second
// behind it, each waiting on the row before the next starts, and
// releases it. It answers both replies.
func (s *executionStack) queued(t *testing.T, first, second func() (reply, error)) (reply, reply) {
	t.Helper()
	return s.queuedBehind(t, `SELECT 1 FROM agent_executions WHERE agent_grn = $1 FOR UPDATE`, s.agent.String(), first, second)
}

// queuedBehind is queued behind the row lock takes for arg.
func (s *executionStack) queuedBehind(t *testing.T, lock string, arg any, first, second func() (reply, error)) (reply, reply) {
	t.Helper()
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, lock, arg)
	require.NoError(t, err)

	type result struct {
		r   reply
		err error
	}
	start := func(f func() (reply, error)) chan result {
		done := make(chan result, 1)
		go func() {
			r, err := f()
			done <- result{r, err}
		}()
		return done
	}
	firstDone := start(first)
	awaitLockWaiters(t, s.pool, 1)
	secondDone := start(second)
	awaitLockWaiters(t, s.pool, 2)
	require.NoError(t, tx.Rollback(ctx))

	a, b := <-firstDone, <-secondDone
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	return a.r, b.r
}

// awaitLockWaiters returns once n sessions of this database wait on a
// lock: each queued request is waiting on the held row.
func awaitLockWaiters(t *testing.T, p *pgxpool.Pool, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting int
		err := p.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting)
		return err == nil && waiting >= n
	}, 10*time.Second, 10*time.Millisecond)
}

// stored counts the agent's messages under key.
func (s *executionStack) stored(t *testing.T, key string) int {
	t.Helper()
	var n int
	require.NoError(t, s.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM messages WHERE sender_grn = $1 AND idempotency_key = $2`, s.agent.String(), key).Scan(&n))
	return n
}

// TestExecutionFence_ALeafPastItsValidityIsRefusedOnTheConnectionItOpened
// opens a connection with a certificate the listener's clock accepts,
// then measures every protected call on that connection by the
// database's clock, which says it has expired. The agent's current
// certificate on the same routes is the control; introspection answers
// the expired one as fenced rather than unproven.
func TestExecutionFence_ALeafPastItsValidityIsRefusedOnTheConnectionItOpened(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	issued, err := s.past.Issue(context.Background(), s.agent)
	require.NoError(t, err)
	expiredLeaf := string(issued.CertificatePEM())
	pair, err := tls.X509KeyPair(issued.CertificatePEM(), issued.PrivateKeyPEM())
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	require.NoError(t, err)
	require.True(t, leaf.NotAfter.Before(time.Now()), "the certificate expired before the test began")
	expiredTLS := machineClient(t, pair)

	s.clock.shift(leaf.NotAfter.Add(-time.Minute).Sub(time.Now()))
	s.open(t, expiredTLS)
	s.clock.shift(0)
	s.open(t, s.agentTLS)

	allRefused(t, s.protectedCalls(t, expiredTLS, ""), http.StatusConflict, "an expired certificate on its open connection")
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, ""), "control: the current certificate")

	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialFenced, s.standing(t, expiredLeaf, nil).Credential)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, s.standing(t, s.leaf, nil).Credential)
	unproven := s.introspect(t, s.hostedTLS, selfSignedFor(t, s.agent), nil)
	assert.Equal(t, http.StatusUnprocessableEntity, unproven.status, "a certificate nobody issued: %s", unproven.body)

	got := s.activate(t, s.hostedTLS, "r-1", generationOf(1), nil, expiredLeaf)
	assert.Equal(t, http.StatusConflict, got.status, "an expired certificate activates nothing: %s", got.body)
	a := s.activated(t, "r-1", generationOf(1), nil, s.leaf)
	allRefused(t, s.protectedCalls(t, expiredTLS, a.Token), http.StatusConflict, "an expired certificate with the current token")
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, a.Token), "control: the current certificate with that token")
}

// TestExecutionFence_EachContractFamilyAnswersItsOwnVersion sends each
// family's routes through the generated router with the version
// header missing, naming another family, and malformed. Each is
// refused with 400 naming the family's own version; the version it
// names is the control.
func TestExecutionFence_EachContractFamilyAnswersItsOwnVersion(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	introspection, err := json.Marshal(map[string]any{"certificatePem": s.leaf, "generation": nil})
	require.NoError(t, err)

	families := []struct {
		path, body, version, other string
		admitted                   int
	}{
		{s.agentPath("/execution/introspection"), string(introspection), agent.ExecutionContractVersion, operator.ContractVersion, http.StatusOK},
		{"/operation-authorities/introspection", `{"authority":"absent"}`, operator.ContractVersion, agent.ExecutionContractVersion, http.StatusNotFound},
		{"/operators/" + s.controller.String() + "/managed-agents", `{"requestId":"r-1","operationRef":"absent"}`,
			agent.EnrollmentContractVersion, agent.ExecutionContractVersion, http.StatusNotFound},
	}
	for _, f := range families {
		for _, sent := range []string{"", f.other, "execution-fence.v2", "not a version"} {
			got := s.send(t, s.hostedTLS, http.MethodPost, f.path, sent, "", f.body)
			assert.Equal(t, http.StatusBadRequest, got.status, "%s with %q: %s", f.path, sent, got.body)
			assert.Equal(t, f.version, got.version, "%s with %q names its own version", f.path, sent)
		}
		got := s.send(t, s.hostedTLS, http.MethodPost, f.path, f.version, "", f.body)
		assert.Equal(t, f.admitted, got.status, "control: %s with its version: %s", f.path, got.body)
		assert.Equal(t, f.version, got.version)
	}
	for path, version := range map[string]string{
		s.agentPath("/activations"):                                  agent.ExecutionContractVersion,
		s.agentPath("/activations/" + absentClaim + "/deactivation"): agent.ExecutionContractVersion,
		s.agentPath("/credential-recovery"):                          agent.ExecutionContractVersion,
		s.agentPath("/initial-certificate"):                          agent.EnrollmentContractVersion,
	} {
		got := s.send(t, s.hostedTLS, http.MethodPost, path, "", "", "{}")
		assert.Equal(t, http.StatusBadRequest, got.status, "%s without a version: %s", path, got.body)
		assert.Equal(t, version, got.version, path)
	}
}

// TestExecutionFence_UnknownFreshnessRefusesAsUnavailable holds that an
// engine unable to answer from current policy, and a database that
// stopped answering, refuse activation as unavailable rather than
// admit it. The restored engine is the control.
func TestExecutionFence_UnknownFreshnessRefusesAsUnavailable(t *testing.T) {
	p := pool(t)
	srv := grpc.NewServer()
	gate := &engineGate{current: authz.NewGRPCServer(authz.NewPolicyCache(authz.NewPostgresStore(p)))}
	authzv1.RegisterAuthzServiceServer(srv, gate)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	engine, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = engine.Close() })
	s := newExecutionStack(t, p, engine)

	gate.checkOnly()
	got := s.activate(t, s.hostedTLS, "r-1", generationOf(1), nil, s.leaf)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, "an engine without CheckCurrent: %s", got.body)
	got = s.introspect(t, s.hostedTLS, s.leaf, nil)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, "an engine without CheckCurrent: %s", got.body)

	gate.restore()
	a := s.activated(t, "r-1", generationOf(1), nil, s.leaf)
	p.Close()
	got = s.activate(t, s.hostedTLS, "r-2", generationOf(2), &a.ActivationId, s.leaf)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, "a database that stopped answering: %s", got.body)
}

// TestExecutionFence_ALifecycleWriteIsBoundedByTheServersDeadline holds
// the agent's execution row past agent.LifecycleDeadline while a
// client that would wait longer asks for an activation, a deactivation
// and a recovery. Each is answered unavailable at the server's
// deadline, writes nothing once the row is released, and the same
// request then succeeds as the control.
func TestExecutionFence_ALifecycleWriteIsBoundedByTheServersDeadline(t *testing.T) {
	acts := []struct {
		name      string
		cutOver   bool
		succeeded int
		do        func(s *executionStack, current *machinegen.Activation, r recovery) (reply, error)
		unwritten func(t *testing.T, s *executionStack, current *machinegen.Activation)
	}{
		{name: "activation", succeeded: http.StatusCreated,
			do: func(s *executionStack, _ *machinegen.Activation, _ recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations"), agent.ExecutionContractVersion, "",
					activationBodyOf("r-1", s.epoch, generationOf(1), nil, s.leaf))
			},
			unwritten: func(t *testing.T, s *executionStack, _ *machinegen.Activation) {
				in := s.standing(t, s.leaf, nil)
				assert.Equal(t, machinegen.ExecutionIntrospectionModeLegacy, in.Mode)
				assert.Nil(t, in.ActivationId)
			}},
		{name: "deactivation", cutOver: true, succeeded: http.StatusOK,
			do: func(s *executionStack, current *machinegen.Activation, _ recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations/"+current.ActivationId+"/deactivation"),
					agent.ExecutionContractVersion, "", "")
			},
			unwritten: func(t *testing.T, s *executionStack, current *machinegen.Activation) {
				assert.Equal(t, machinegen.ExecutionIntrospectionGenerationCurrent, s.standing(t, s.leaf, ptr(generationOf(1))).Generation)
			}},
		{name: "recovery", cutOver: true, succeeded: http.StatusCreated,
			do: func(s *executionStack, _ *machinegen.Activation, r recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/credential-recovery"), agent.ExecutionContractVersion,
					"Garam-Operation "+r.authority, r.body)
			},
			unwritten: func(t *testing.T, s *executionStack, _ *machinegen.Activation) {
				assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, s.standing(t, s.leaf, nil).Credential)
			}},
	}
	for _, a := range acts {
		t.Run(a.name, func(t *testing.T) {
			p := pool(t)
			_, engine := policyEngine(t, p)
			s := newExecutionStack(t, p, engine)
			var current *machinegen.Activation
			if a.cutOver {
				activated := s.activated(t, "r-0", generationOf(1), nil, s.leaf)
				current = &activated
			}
			r := s.prepareRecovery(t, "rec-1")

			ctx := context.Background()
			tx, err := p.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, `SELECT 1 FROM agent_executions WHERE agent_grn = $1 FOR UPDATE`, s.agent.String())
			require.NoError(t, err)

			got, elapsed := s.boundedBy(t, func() (reply, error) { return a.do(s, current, r) }, func() { _ = tx.Rollback(ctx) })
			assert.Equal(t, http.StatusServiceUnavailable, got.status, "the %s waited on the held row: %s", a.name, got.body)
			assert.GreaterOrEqual(t, elapsed, agent.LifecycleDeadline)
			assert.Less(t, elapsed, agent.LifecycleDeadline+5*time.Second, "answered at the server's deadline")
			a.unwritten(t, s, current)

			again, err := a.do(s, current, r)
			require.NoError(t, err)
			assert.Equal(t, a.succeeded, again.status, "control: the same request once the row is free: %s", again.body)
		})
	}
}

// TestExecutionFence_AnAuthorityThatDoesNotAnswerIsBoundedByTheServersDeadline
// holds the policy engine's current check unanswered, before any lock
// is taken. The activation is answered unavailable at the server's
// deadline; the engine answering is the control.
func TestExecutionFence_AnAuthorityThatDoesNotAnswerIsBoundedByTheServersDeadline(t *testing.T) {
	p := pool(t)
	srv := grpc.NewServer()
	engineSrv := &stalledEngine{current: authz.NewGRPCServer(authz.NewPolicyCache(authz.NewPostgresStore(p)))}
	authzv1.RegisterAuthzServiceServer(srv, engineSrv)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	engine, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = engine.Close() })
	s := newExecutionStack(t, p, engine)

	release := engineSrv.stall()
	got, elapsed := s.boundedBy(t, func() (reply, error) {
		return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations"), agent.ExecutionContractVersion, "",
			activationBodyOf("r-1", s.epoch, generationOf(1), nil, s.leaf))
	}, release)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, got.body)
	assert.GreaterOrEqual(t, elapsed, agent.LifecycleDeadline)
	assert.Less(t, elapsed, agent.LifecycleDeadline+5*time.Second, "answered at the server's deadline")

	s.activated(t, "r-1", generationOf(1), nil, s.leaf)
}

// boundedBy sends one request from a client that waits well past the
// server's deadline, releases what it is waiting on once it is
// answered or once that longer wait runs out, and answers the reply and
// how long it took.
func (s *executionStack) boundedBy(t *testing.T, send func() (reply, error), release func()) (reply, time.Duration) {
	t.Helper()
	type result struct {
		r   reply
		err error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		r, err := send()
		done <- result{r, err}
	}()
	var got result
	select {
	case got = <-done:
		release()
	case <-time.After(agent.LifecycleDeadline + 10*time.Second):
		release()
		got = <-done
	}
	elapsed := time.Since(start)
	require.NoError(t, got.err)
	return got.r, elapsed
}

// stalledEngine serves the current engine, except that once stalled
// its current check answers nothing until released or abandoned.
type stalledEngine struct {
	authzv1.UnimplementedAuthzServiceServer
	current authzv1.AuthzServiceServer
	mu      sync.Mutex
	held    chan struct{}
}

func (e *stalledEngine) stall() func() {
	held := make(chan struct{})
	e.mu.Lock()
	e.held = held
	e.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			e.mu.Lock()
			e.held = nil
			e.mu.Unlock()
			close(held)
		})
	}
}

func (e *stalledEngine) Check(ctx context.Context, r *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	return e.current.Check(ctx, r)
}

func (e *stalledEngine) CheckCurrent(ctx context.Context, r *authzv1.CheckCurrentRequest) (*authzv1.CheckCurrentResponse, error) {
	e.mu.Lock()
	held := e.held
	e.mu.Unlock()
	if held != nil {
		select {
		case <-held:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return e.current.CheckCurrent(ctx, r)
}

// TestExecutionFence_AStalledRegistrationReadIsBoundedByTheServersDeadline
// holds the operators table while the control service, on a connection
// already open, asks for an activation: the recheck of its own
// registration is the first read the deadline bounds. It is answered
// unavailable at the deadline, and the same request then succeeds as
// the control.
func TestExecutionFence_AStalledRegistrationReadIsBoundedByTheServersDeadline(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	s.open(t, s.hostedTLS)

	ctx := context.Background()
	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `LOCK TABLE operators IN ACCESS EXCLUSIVE MODE`)
	require.NoError(t, err)

	got, elapsed := s.boundedBy(t, func() (reply, error) {
		return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations"), agent.ExecutionContractVersion, "",
			activationBodyOf("r-1", s.epoch, generationOf(1), nil, s.leaf))
	}, func() { _ = tx.Rollback(ctx) })
	assert.Equal(t, http.StatusServiceUnavailable, got.status, got.body)
	assert.True(t, got.reused, "the request reused the connection the handshake admitted")
	assert.GreaterOrEqual(t, elapsed, agent.LifecycleDeadline)
	assert.Less(t, elapsed, agent.LifecycleDeadline+5*time.Second, "answered at the server's deadline")
	assert.Nil(t, s.standing(t, s.leaf, nil).ActivationId, "nothing was activated")

	s.activated(t, "r-1", generationOf(1), nil, s.leaf)
}
