package integration_test

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/authz"
	authzpg "github.com/garamsh/garam/platform/internal/authz/postgres"
	"github.com/garamsh/garam/platform/internal/cli"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// executionStack is both listeners as the serve commands compose them,
// over one database: acme's owner and member on the browser surface;
// on the machine surface the hosted control service, whose grant
// covers agent:activate, agent:recover and agent:configure for the
// controller, the controller itself, each presenting the token its key
// was exchanged for, and an agent assigned to the controller in a
// lineage its tokens are minted in, with no activation yet. A second
// agent of the controller, activated and holding its token, posts to
// the first.
type executionStack struct {
	pool    *pgxpool.Pool
	browser string
	machine string
	// server is the machine listener serving at machine, composed from
	// config, which restart composes the next one from.
	server     *http.Server
	config     cli.MachineConfig
	acme       grn.GRN
	owner      *http.Cookie
	member     *http.Cookie
	hosted     grn.GRN
	controller grn.GRN
	// client reaches the listener presenting nothing of its own, for a
	// request that names its credentials itself.
	client *http.Client
	// controllerKey is what the controller signs its assertions with,
	// for garam's token or for one shown to the hosted control service.
	controllerKey   *rsa.PrivateKey
	hostedToken     string
	controllerToken string
	// hostedTLS and controlTLS present the hosted control service's
	// and the controller's token on every request.
	hostedTLS  *http.Client
	controlTLS *http.Client
	agent      grn.GRN
	epoch      int
	// forwarded is the agent's token the controller minted to be shown
	// to the hosted control service, which an activation and an
	// introspection forward (ADR-0100).
	forwarded string
	// agentTLS presents the agent's own token, minted for garam.
	agentTLS *http.Client
	peer     grn.GRN
	peerTLS  *http.Client
	keys     atomic.Int64
	// grant is the hosted control service's current grant, which the
	// next delegation replaces.
	grant *string
}

// newExecutionStack is the stack before any activation of its agent.
func newExecutionStack(t *testing.T, p *pgxpool.Pool, engine grpc.ClientConnInterface) *executionStack {
	t.Helper()
	ctx := context.Background()
	orgs := orgpg.New(p)
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
		require.NoError(t, authnpg.NewSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}
	}

	browserSrv := api.NewServer(users, orgs, nil, operatorpg.New(p), nil, nil,
		authnpg.NewSessionStore(p), nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), nil, nil, cli.NewAuthorityService(p, engine), cli.NewStandaloneService(p, engine),
		api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(browserSrv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)

	_, sealer := keyEncryptionKey(t)
	presented := loopbackCertificate(t)
	s := &executionStack{
		pool: p, browser: browser.URL,
		config: cli.MachineConfig{
			Sealer: sealer, Issuer: testIssuer, Certificate: presented, ServerRoot: certificatePEM(presented),
			Delivery: briefDelivery(), Engine: engine,
			Logger:   slog.New(slog.NewJSONHandler(io.Discard, nil)),
			TokenURL: testTokenURL,
		},
		acme: acme, owner: session("ada", org.RoleOwner), member: session("bob", org.RoleMember),
		hosted: scopedGRN("acme", operator.ServiceType, "hosted"), controller: scopedGRN("acme", operator.ServiceType, "controller"),
	}
	s.serve(t)
	s.client, _ = tokenClient(t)
	registerOperator(t, p, s.hosted)
	registerOperator(t, p, s.controller)
	s.hostedToken = s.keyedToken(t, s.hosted)
	s.controllerKey, _ = issueKey(t, operatorpg.New(p), s.controller, operator.KeyRotationFirst)
	s.controllerToken = exchangeKey(t, s.client, s.machine, s.controller, s.controllerKey, nil)
	s.hostedTLS = tokenPresenting(t, s.hostedToken)
	s.controlTLS = tokenPresenting(t, s.controllerToken)
	s.delegate(t, "agent:activate", "agent:recover", "agent:configure")

	// The agent is the controller's, in a lineage of its own and with no
	// activation yet.
	agents := agentpg.New(p)
	s.agent = seedAgent(t, agents, acme)
	assignment, err := agents.Assign(ctx, s.agent, s.controller)
	require.NoError(t, err)
	s.epoch = assignment.Epoch
	openedLineage(t, p, s.agent)
	s.forwarded = s.agentToken(t, s.agent, &s.hosted).Token
	s.agentTLS = tokenPresenting(t, s.agentToken(t, s.agent, nil).Token)
	s.peer, s.peerTLS = provisioned(t, s.machine, s.controlTLS)
	return s
}

// keyedToken issues subject its first key, as the console does, and
// answers the token garam accepts for it.
func (s *executionStack) keyedToken(t *testing.T, subject grn.GRN) string {
	t.Helper()
	private, _ := issueKey(t, operatorpg.New(s.pool), subject, operator.KeyRotationFirst)
	return exchangeKey(t, s.client, s.machine, subject, private, nil)
}

// agentToken is the controller's mint of g's token: garam's where
// audience is nil, and one shown to audience otherwise.
func (s *executionStack) agentToken(t *testing.T, g grn.GRN, audience *grn.GRN) machinegen.AgentToken {
	t.Helper()
	body := `{}`
	if audience != nil {
		body = `{"audience":"` + audience.String() + `"}`
	}
	got := s.send(t, s.controlTLS, http.MethodPost, "/agents/"+g.String()+"/tokens", operator.TokenContractVersion, "", body)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var minted machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(got.body), &minted))
	return minted
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
// credential one credential the request carries; either is left off
// when empty. An execution token or an operation authority travels in
// the header of its own name (ADR-0100), and any other credential in
// Authorization. It reports failure rather than failing the test, so a
// goroutine can call it.
func (s *executionStack) exchange(client *http.Client, method, path, version, credential, body string) (reply, error) {
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
	if credential != "" {
		scheme, value, _ := strings.Cut(credential, " ")
		switch scheme {
		case "Garam-Execution", "Garam-Operation":
			req.Header.Set(scheme, value)
		default:
			req.Header.Set("Authorization", credential)
		}
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

func (s *executionStack) send(t *testing.T, client *http.Client, method, path, version, credential, body string) reply {
	t.Helper()
	got, err := s.exchange(client, method, path, version, credential, body)
	require.NoError(t, err)
	return got
}

// lifecycle sends one execution-fence.v2 request, whose every answer
// names that version.
func (s *executionStack) lifecycle(t *testing.T, client *http.Client, path, credential, body string) reply {
	t.Helper()
	got := s.send(t, client, http.MethodPost, path, agent.TokenExecutionContractVersion, credential, body)
	assert.Equal(t, agent.TokenExecutionContractVersion, got.version, "every execution answer names its contract")
	return got
}

func (s *executionStack) agentPath(suffix string) string {
	return "/agents/" + s.agent.String() + suffix
}

func activationBody(t *testing.T, requestID string, epoch int, generation agent.Generation, replaces *string, forwarded string) string {
	t.Helper()
	return activationBodyOf(requestID, epoch, generation, replaces, forwarded)
}

// activationBodyOf is activationBody for a goroutine, which cannot fail
// the test; a map of strings always marshals.
func activationBodyOf(requestID string, epoch int, generation agent.Generation, replaces *string, forwarded string) string {
	raw, _ := json.Marshal(map[string]any{
		"requestId": requestID, "epoch": strconv.Itoa(epoch), "generation": string(generation),
		"replacesActivationId": replaces, "operationRef": nil, "token": forwarded,
	})
	return string(raw)
}

func (s *executionStack) activate(t *testing.T, client *http.Client, requestID string, g agent.Generation, replaces *string, forwarded string) reply {
	t.Helper()
	return s.lifecycle(t, client, s.agentPath("/activations"), "", activationBody(t, requestID, s.epoch, g, replaces, forwarded))
}

// activated activates g for the agent and answers the activation.
func (s *executionStack) activated(t *testing.T, requestID string, g agent.Generation, replaces *string, forwarded string) machinegen.Activation {
	t.Helper()
	got := s.activate(t, s.hostedTLS, requestID, g, replaces, forwarded)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var a machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	return a
}

func (s *executionStack) introspect(t *testing.T, client *http.Client, forwarded string, g *agent.Generation) reply {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"token": forwarded, "generation": g})
	require.NoError(t, err)
	return s.lifecycle(t, client, s.agentPath("/execution/introspection"), "", string(raw))
}

// standing answers what introspection says of the forwarded token and
// g.
func (s *executionStack) standing(t *testing.T, forwarded string, g *agent.Generation) machinegen.ExecutionIntrospection {
	t.Helper()
	got := s.introspect(t, s.hostedTLS, forwarded, g)
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
// one: the exact request body, and the owner's agent:recover handoff
// for that body. Under execution-fence.v2 it signs nothing and carries
// no certificate request (ADR-0100, ADR-0118).
type recovery struct {
	body      string
	authority string
}

func (s *executionStack) prepareRecovery(t *testing.T, requestID string) recovery {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"requestId": requestID, "epoch": strconv.Itoa(s.epoch)})
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	status, got := s.mintHandoff(t, s.owner, hex.EncodeToString(digest[:]), "h-"+requestID)
	require.Equal(t, http.StatusCreated, status, got)
	return recovery{body: string(raw), authority: authorityOf(t, got).Authority}
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

// recovered answers the agent's own client and the token forwarded for
// it in the lineage a recovery opened, once the recovery answered: a
// recovery signs nothing, so what the agent holds next is a token the
// controller mints in that lineage.
func (s *executionStack) recovered(t *testing.T, got reply) (*http.Client, string) {
	t.Helper()
	require.Equal(t, http.StatusCreated, got.status, got.body)
	return tokenPresenting(t, s.agentToken(t, s.agent, nil).Token), s.agentToken(t, s.agent, &s.hosted).Token
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
	return out
}

func (s *executionStack) key() string { return "key-" + strconv.FormatInt(s.keys.Add(1), 10) }

// admittedStatus is what each protected call answers when admitted.
var admittedStatus = map[string]int{
	"claim": http.StatusOK, "renewal": http.StatusOK, "completion": http.StatusNoContent,
	"post": http.StatusCreated, "status": http.StatusOK, "runtime-state": http.StatusNoContent,
	"turn-event": http.StatusAccepted,
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
// carries a GRN from before any activation through its first, a
// retried activation, a replacement and a deactivation, with the agent on one
// TLS connection throughout. Every protected route is refused the
// token each step supersedes and admits the one it issues.
func TestExecutionFence_AnActivationFencesEveryProtectedRouteOnAnOpenConnection(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	s.open(t, s.agentTLS)

	// An agent never activated writes nothing: not without a token, and
	// not with one it was never activated with (ADR-0118).
	allRefused(t, s.protectedCalls(t, s.agentTLS, ""), http.StatusUnauthorized, "no token before any activation")
	allRefused(t, s.protectedCalls(t, s.agentTLS, strings.Repeat("A", 43)), http.StatusForbidden, "a token never activated")
	in := s.standing(t, s.forwarded, nil)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, in.Credential)
	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationNone, in.Generation)
	assert.Nil(t, in.ActivationId)

	// Only the hosted control service activates: not the agent's own
	// token, not the controller, and not another organization's
	// operator.
	globexTLS := operatorClient(t, p, s.machine, scopedGRN("globex", operator.ServiceType, "hosted"))
	for name, client := range map[string]*http.Client{"agent": s.agentTLS, "controller": s.controlTLS, "globex": globexTLS} {
		got := s.activate(t, client, "r-1", generationOf(1), nil, s.forwarded)
		assert.Equal(t, http.StatusForbidden, got.status, "%s: %s", name, got.body)
	}

	first := s.activated(t, "r-1", generationOf(1), nil, s.forwarded)
	assert.Equal(t, s.agent.String(), first.Grn)
	assert.Equal(t, strconv.Itoa(s.epoch), first.Epoch)
	require.NotNil(t, first.Lineage, "the activation names the lineage the agent's token was minted in")
	allRefused(t, s.protectedCalls(t, s.agentTLS, ""), http.StatusUnauthorized, "no token once activated")
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, first.Token), "the activation's token")

	// A retry whose answer was lost gets the same activation and a new
	// token; only the newest is admitted.
	retried := s.activate(t, s.hostedTLS, "r-1", generationOf(1), nil, s.forwarded)
	require.Equal(t, http.StatusOK, retried.status, retried.body)
	var again machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(retried.body), &again))
	assert.Equal(t, first.ActivationId, again.ActivationId)
	assert.Greater(t, again.TokenVersion, first.TokenVersion)
	allRefused(t, s.protectedCalls(t, s.agentTLS, first.Token), http.StatusForbidden, "the retried activation's earlier token")
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, again.Token), "the retry's token")

	// A replacement fences the generation it replaces; a replay of the
	// first activation's placement does not win.
	second := s.activated(t, "r-2", generationOf(2), &first.ActivationId, s.forwarded)
	allRefused(t, s.protectedCalls(t, s.agentTLS, again.Token), http.StatusForbidden, "the replaced generation's token")
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, second.Token), "the replacement's token")
	got := s.activate(t, s.hostedTLS, "r-3", generationOf(3), nil, s.forwarded)
	assert.Equal(t, http.StatusConflict, got.status, "a replay naming no predecessor: %s", got.body)
	got = s.activate(t, s.hostedTLS, "r-3", generationOf(2), &second.ActivationId, s.forwarded)
	assert.Equal(t, http.StatusConflict, got.status, "a generation already active in this lineage: %s", got.body)

	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationNotCurrent, s.standing(t, s.forwarded, ptr(generationOf(1))).Generation)
	in = s.standing(t, s.forwarded, ptr(generationOf(2)))
	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationCurrent, in.Generation)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, in.Credential)
	assert.Equal(t, s.controller.String(), in.Assignee)
	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationNotSupplied, s.standing(t, s.forwarded, nil).Generation)

	// Deactivation ends the current token and is answered the same when
	// replayed; the next activation still replaces the deactivated one.
	got = s.deactivate(t, second.ActivationId)
	require.Equal(t, http.StatusOK, got.status, got.body)
	allRefused(t, s.protectedCalls(t, s.agentTLS, second.Token), http.StatusForbidden, "a deactivated token")
	got = s.deactivate(t, second.ActivationId)
	assert.Equal(t, http.StatusOK, got.status, "a replayed deactivation: %s", got.body)
	got = s.deactivate(t, absentClaim)
	assert.Equal(t, http.StatusNotFound, got.status, "an activation nobody made: %s", got.body)
	third := s.activated(t, "r-4", generationOf(4), &second.ActivationId, s.forwarded)
	got = s.deactivate(t, first.ActivationId)
	assert.Equal(t, http.StatusOK, got.status, "an ended activation replayed after its successor: %s", got.body)
	allAdmitted(t, s.protectedCalls(t, s.agentTLS, third.Token), "the successor survives the replay")
	got = s.activate(t, s.hostedTLS, "r-5", generationOf(5), &first.ActivationId, s.forwarded)
	assert.Equal(t, http.StatusConflict, got.status, "the replayed ID is not the anchor: %s", got.body)
	s.activated(t, "r-5", generationOf(5), &third.ActivationId, s.forwarded)
}

// TestExecutionFence_RecoveryFencesTheOldLineageOnAnOpenConnection
// holds that an explicit recovery replaces the credential lineage: the
// agent's token of the old lineage, still inside its validity and on
// the connection it opened, is refused even with a current execution
// token, while one minted in the new lineage is admitted with that same
// execution token.
func TestExecutionFence_RecoveryFencesTheOldLineageOnAnOpenConnection(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	s.open(t, s.agentTLS)
	first := s.activated(t, "r-1", generationOf(1), nil, s.forwarded)

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
	recoveredTLS, recoveredToken := s.recovered(t, answer)
	retry := s.recover(t, r, r.body)
	assert.Equal(t, http.StatusCreated, retry.status, retry.body)
	assert.JSONEq(t, answer.body, retry.body, "a retry answers the recorded lineage")
	second := s.prepareRecovery(t, "rec-2")
	got2 := s.recover(t, r, second.body)
	assert.Equal(t, http.StatusConflict, got2.status, "a second recovery under a spent handoff: %s", got2.body)

	// The recovery ended the activation, so its token admits nothing,
	// even beside the agent's token of the new lineage.
	s.open(t, recoveredTLS)
	allRefused(t, s.protectedCalls(t, recoveredTLS, first.Token), http.StatusForbidden, "the token the recovery ended")

	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialFenced, s.standing(t, s.forwarded, nil).Credential)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, s.standing(t, recoveredToken, nil).Credential)
	stale := s.activate(t, s.hostedTLS, "r-2", generationOf(2), &first.ActivationId, s.forwarded)
	assert.Equal(t, http.StatusConflict, stale.status, "the old lineage activates nothing: %s", stale.body)

	// The same generation activates again in the new lineage.
	next := s.activated(t, "r-3", generationOf(1), &first.ActivationId, recoveredToken)
	require.NotNil(t, next.Lineage)
	allRefused(t, s.protectedCalls(t, s.agentTLS, next.Token), http.StatusUnauthorized, "the old lineage's token with the current execution token")
	s.open(t, recoveredTLS)
	allAdmitted(t, s.protectedCalls(t, recoveredTLS, next.Token), "the new lineage's token with the same execution token")
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
		do        func(t *testing.T, s *executionStack, current *machinegen.Activation, r recovery) (reply, error)
		succeeded int
		refused   int
	}
	acts := []act{
		{name: "replacement", refused: http.StatusForbidden, succeeded: http.StatusCreated,
			do: func(t *testing.T, s *executionStack, current *machinegen.Activation, _ recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations"), agent.TokenExecutionContractVersion, "",
					activationBody(t, "race", s.epoch, generationOf(9), &current.ActivationId, s.forwarded))
			}},
		{name: "deactivation", refused: http.StatusForbidden, succeeded: http.StatusOK,
			do: func(_ *testing.T, s *executionStack, current *machinegen.Activation, _ recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations/"+current.ActivationId+"/deactivation"),
					agent.TokenExecutionContractVersion, "", "")
			}},
		{name: "recovery", refused: http.StatusForbidden, succeeded: http.StatusCreated,
			do: func(_ *testing.T, s *executionStack, _ *machinegen.Activation, r recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/credential-recovery"), agent.TokenExecutionContractVersion,
					"Garam-Operation "+r.authority, r.body)
			}},
		{name: "reassignment", refused: http.StatusForbidden, succeeded: http.StatusOK,
			do: func(_ *testing.T, s *executionStack, _ *machinegen.Activation, _ recovery) (reply, error) {
				_, err := agentpg.New(s.pool).Assign(context.Background(), s.agent, scopedGRN("acme", operator.ServiceType, "other"))
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
				activated := s.activated(t, "r-1", generationOf(1), nil, s.forwarded)
				current, token := &activated, activated.Token
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

// TestExecutionFence_EachContractFamilyAnswersItsOwnVersion sends each
// family's routes through the generated router with the version
// header missing, naming another family, and malformed. Each is
// refused with 400 naming the family's own version; the version it
// names is the control.
func TestExecutionFence_EachContractFamilyAnswersItsOwnVersion(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	introspection, err := json.Marshal(map[string]any{"token": s.forwarded, "generation": nil})
	require.NoError(t, err)

	families := []struct {
		path, body, version, other string
		admitted                   int
	}{
		{s.agentPath("/execution/introspection"), string(introspection), agent.TokenExecutionContractVersion, operator.TokenAuthorityContractVersion, http.StatusOK},
		{"/operation-authorities/introspection", `{"authority":"absent"}`, operator.TokenAuthorityContractVersion, agent.TokenExecutionContractVersion, http.StatusNotFound},
		{"/operators/" + s.controller.String() + "/managed-agents", `{"requestId":"r-1","operationRef":"absent"}`,
			agent.TokenEnrollmentContractVersion, agent.TokenExecutionContractVersion, http.StatusNotFound},
	}
	for _, f := range families {
		for _, sent := range []string{"", f.other, "execution-fence.v1", "not a version"} {
			got := s.send(t, s.hostedTLS, http.MethodPost, f.path, sent, "", f.body)
			assert.Equal(t, http.StatusBadRequest, got.status, "%s with %q: %s", f.path, sent, got.body)
			assert.Equal(t, f.version, got.version, "%s with %q names its own version", f.path, sent)
		}
		got := s.send(t, s.hostedTLS, http.MethodPost, f.path, f.version, "", f.body)
		assert.Equal(t, f.admitted, got.status, "control: %s with its version: %s", f.path, got.body)
		assert.Equal(t, f.version, got.version)
	}
	for path, version := range map[string]string{
		s.agentPath("/activations"):                                  agent.TokenExecutionContractVersion,
		s.agentPath("/activations/" + absentClaim + "/deactivation"): agent.TokenExecutionContractVersion,
		s.agentPath("/credential-recovery"):                          agent.TokenExecutionContractVersion,
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
	gate := &engineGate{current: authz.NewGRPCServer(authz.NewPolicyCache(authzpg.New(p)))}
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
	got := s.activate(t, s.hostedTLS, "r-1", generationOf(1), nil, s.forwarded)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, "an engine without CheckCurrent: %s", got.body)
	got = s.introspect(t, s.hostedTLS, s.forwarded, nil)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, "an engine without CheckCurrent: %s", got.body)

	gate.restore()
	a := s.activated(t, "r-1", generationOf(1), nil, s.forwarded)
	p.Close()
	got = s.activate(t, s.hostedTLS, "r-2", generationOf(2), &a.ActivationId, s.forwarded)
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
		activated bool
		succeeded int
		do        func(s *executionStack, current *machinegen.Activation, r recovery) (reply, error)
		unwritten func(t *testing.T, s *executionStack, current *machinegen.Activation)
	}{
		{name: "activation", succeeded: http.StatusCreated,
			do: func(s *executionStack, _ *machinegen.Activation, _ recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations"), agent.TokenExecutionContractVersion, "",
					activationBodyOf("r-1", s.epoch, generationOf(1), nil, s.forwarded))
			},
			unwritten: func(t *testing.T, s *executionStack, _ *machinegen.Activation) {
				assert.Nil(t, s.standing(t, s.forwarded, nil).ActivationId)
			}},
		{name: "deactivation", activated: true, succeeded: http.StatusOK,
			do: func(s *executionStack, current *machinegen.Activation, _ recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations/"+current.ActivationId+"/deactivation"),
					agent.TokenExecutionContractVersion, "", "")
			},
			unwritten: func(t *testing.T, s *executionStack, current *machinegen.Activation) {
				assert.Equal(t, machinegen.ExecutionIntrospectionGenerationCurrent, s.standing(t, s.forwarded, ptr(generationOf(1))).Generation)
			}},
		{name: "recovery", activated: true, succeeded: http.StatusCreated,
			do: func(s *executionStack, _ *machinegen.Activation, r recovery) (reply, error) {
				return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/credential-recovery"), agent.TokenExecutionContractVersion,
					"Garam-Operation "+r.authority, r.body)
			},
			unwritten: func(t *testing.T, s *executionStack, _ *machinegen.Activation) {
				assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, s.standing(t, s.forwarded, nil).Credential)
			}},
	}
	for _, a := range acts {
		t.Run(a.name, func(t *testing.T) {
			p := pool(t)
			_, engine := policyEngine(t, p)
			s := newExecutionStack(t, p, engine)
			var current *machinegen.Activation
			if a.activated {
				activated := s.activated(t, "r-0", generationOf(1), nil, s.forwarded)
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
	engineSrv := &stalledEngine{current: authz.NewGRPCServer(authz.NewPolicyCache(authzpg.New(p)))}
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
		return s.exchange(s.hostedTLS, http.MethodPost, s.agentPath("/activations"), agent.TokenExecutionContractVersion, "",
			activationBodyOf("r-1", s.epoch, generationOf(1), nil, s.forwarded))
	}, release)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, got.body)
	assert.GreaterOrEqual(t, elapsed, agent.LifecycleDeadline)
	assert.Less(t, elapsed, agent.LifecycleDeadline+5*time.Second, "answered at the server's deadline")

	s.activated(t, "r-1", generationOf(1), nil, s.forwarded)
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
