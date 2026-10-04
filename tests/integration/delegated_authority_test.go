package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apigen "github.com/garamsh/garam/api"
	machinegen "github.com/garamsh/garam/api/machine"
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
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// authorityStack is both listeners as the serve commands compose them,
// over one database and one policy engine: the browser-facing surface
// over HTTP and the machine surface over mTLS. The delegated authority
// on each is cli.NewAuthorityService, the composition both commands
// run.
type authorityStack struct {
	pool       *pgxpool.Pool
	engine     *grpc.ClientConn
	browser    string
	machine    string
	acme       grn.GRN
	owner      *http.Cookie
	member     *http.Cookie
	memberGRN  grn.GRN
	hosted     grn.GRN
	controller grn.GRN
	hostedTLS  *http.Client
	controlTLS *http.Client
}

func newAuthorityStack(t *testing.T, engine *grpc.ClientConn, p *pgxpool.Pool) *authorityStack {
	t.Helper()
	ctx := context.Background()
	orgs := org.NewPostgresStore(p)
	users := userpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	session := func(id string, role org.Role) (*http.Cookie, grn.GRN) {
		g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
		require.NoError(t, users.Create(ctx, user.New(g, id)))
		require.NoError(t, orgs.AddMember(ctx, org.NewMembership(acme, g, role)))
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, authn.NewPostgresSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}, g
	}
	owner, _ := session("ada", org.RoleOwner)
	member, memberGRN := session("bob", org.RoleMember)

	srv := api.NewServer(users, orgs, nil, operator.NewPostgresStore(p), nil, nil,
		authn.NewPostgresSessionStore(p), nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), nil, nil, cli.NewAuthorityService(p, engine), api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)

	_, sealer := keyEncryptionKey(t)
	issuer, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	hosted := scopedGRN("acme", operator.ServiceType, "hosted")
	controller := scopedGRN("acme", operator.ServiceType, "controller")
	return &authorityStack{
		pool: p, engine: engine, browser: browser.URL, machine: machineListener(t, p, sealer, engine),
		acme: acme, owner: owner, member: member, memberGRN: memberGRN, hosted: hosted, controller: controller,
		hostedTLS:  machineClient(t, installed(t, p, issuer, hosted)),
		controlTLS: machineClient(t, installed(t, p, issuer, controller)),
	}
}

// call sends one versioned contract request and answers the status and
// body.
func call(t *testing.T, client *http.Client, method, url string, cookie *http.Cookie, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(operator.ContractVersionHeader, operator.ContractVersion)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, operator.ContractVersion, resp.Header.Get(operator.ContractVersionHeader), "every contract answer carries the version")
	return resp.StatusCode, string(got)
}

func (s *authorityStack) delegationPath() string {
	return s.browser + "/orgs/acme/operators/hosted/delegation"
}

// grant writes the hosted operator's grant as the owner.
func (s *authorityStack) grant(t *testing.T, requestID string, expected *string, operations ...string) apigen.Delegation {
	t.Helper()
	body := map[string]any{
		"requestId": requestID, "controllers": []string{s.controller.String()},
		"operations": operations, "expiresAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano),
	}
	if expected != nil {
		body["expectedGrantId"] = *expected
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	status, got := call(t, http.DefaultClient, http.MethodPut, s.delegationPath(), s.owner, string(raw))
	require.Equal(t, http.StatusCreated, status, got)
	var d apigen.Delegation
	require.NoError(t, json.Unmarshal([]byte(got), &d))
	return d
}

// mint asks for an authority as cookie's user and answers the status
// and body.
func (s *authorityStack) mint(t *testing.T, cookie *http.Cookie, operation, target, requestID string) (int, string) {
	t.Helper()
	body := `{"audience":"` + s.hosted.String() + `","operation":"` + operation + `","target":"` + target +
		`","requestId":"` + requestID + `","bodySha256":"` + strings.Repeat("c", 64) + `"}`
	return call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", cookie, body)
}

func authorityOf(t *testing.T, body string) apigen.OperationAuthority {
	t.Helper()
	var a apigen.OperationAuthority
	require.NoError(t, json.Unmarshal([]byte(body), &a))
	return a
}

func (s *authorityStack) introspect(t *testing.T, client *http.Client, authority string) (int, string) {
	t.Helper()
	return call(t, client, http.MethodPost, s.machine+"/operation-authorities/introspection", nil,
		`{"authority":"`+authority+`"}`)
}

// TestDelegatedAuthority_EndToEnd crosses both listeners, the policy
// engine and Postgres: an owner delegates, a member hands the hosted
// operator one creation, and the hosted operator reads it back over its
// own certificate. Each refusal sits beside the call that succeeds.
func TestDelegatedAuthority_EndToEnd(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)

	body := `{"requestId":"m-1","controllers":["` + s.controller.String() + `"],"operations":["agent:create"],"expiresAt":"` +
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`
	status, got := call(t, http.DefaultClient, http.MethodPut, s.delegationPath(), s.member, body)
	assert.Equal(t, http.StatusForbidden, status, "a member does not delegate: %s", got)
	d := s.grant(t, "g-1", nil, "agent:create", "agent:configure")

	status, got = s.mint(t, s.member, "agent:create", s.controller.String(), "req-1")
	require.Equal(t, http.StatusCreated, status, got)
	minted := authorityOf(t, got)
	assert.Equal(t, d.GrantId, minted.GrantId)

	status, got = s.introspect(t, s.hostedTLS, minted.Authority)
	require.Equal(t, http.StatusOK, status, got)
	var binding machinegen.OperationBinding
	require.NoError(t, json.Unmarshal([]byte(got), &binding))
	assert.Equal(t, s.memberGRN.String(), binding.ActorGrn)
	assert.Equal(t, s.controller.String(), binding.TargetGrn)
	assert.Equal(t, minted.OperationRef, binding.OperationRef)

	status, _ = s.introspect(t, s.controlTLS, minted.Authority)
	assert.Equal(t, http.StatusNotFound, status, "another operator of the same organization learns nothing")
	status, _ = call(t, s.controlTLS, http.MethodGet, s.machine+"/operation-references/"+minted.OperationRef, nil, "")
	assert.Equal(t, http.StatusNotFound, status)
	status, _ = call(t, s.hostedTLS, http.MethodGet, s.machine+"/operation-references/"+minted.OperationRef, nil, "")
	assert.Equal(t, http.StatusOK, status)

	agentGRN := s.provisionAgent(t)
	status, got = s.mint(t, s.member, "agent:configure", agentGRN, "req-2")
	assert.Equal(t, http.StatusForbidden, status, "a member does not configure: %s", got)
	status, got = s.mint(t, s.owner, "agent:configure", agentGRN, "req-2")
	require.Equal(t, http.StatusCreated, status, got)
	configure := authorityOf(t, got)

	// Membership is read when introspection asks: the member who left
	// is refused, and the owner's authority is the control.
	require.NoError(t, org.NewPostgresStore(s.pool).RemoveMember(context.Background(), s.acme, s.memberGRN))
	status, _ = s.introspect(t, s.hostedTLS, minted.Authority)
	assert.Equal(t, http.StatusForbidden, status, "the actor left the organization")
	status, got = s.introspect(t, s.hostedTLS, configure.Authority)
	require.Equal(t, http.StatusOK, status, got)
	assert.Contains(t, got, `"assignment":{"epoch":"`)

	status, got = call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", s.member,
		`{"audience":"grn:globex:default:operator:hosted","operation":"agent:create","target":"`+s.controller.String()+
			`","requestId":"req-3","bodySha256":"`+strings.Repeat("c", 64)+`"}`)
	assert.Equal(t, http.StatusBadRequest, status, "another organization's audience: %s", got)

	status, got = call(t, http.DefaultClient, http.MethodDelete, s.delegationPath(), s.owner, `{"grantId":"`+d.GrantId+`"}`)
	require.Equal(t, http.StatusNoContent, status, got)
	status, _ = s.introspect(t, s.hostedTLS, minted.Authority)
	assert.Equal(t, http.StatusForbidden, status, "withdrawn")
	status, _ = call(t, http.DefaultClient, http.MethodDelete, s.delegationPath(), s.owner, `{"grantId":"`+d.GrantId+`"}`)
	assert.Equal(t, http.StatusNoContent, status, "a retried withdrawal")
}

// provisionAgent has the controller create an agent assigned to it.
func (s *authorityStack) provisionAgent(t *testing.T) string {
	t.Helper()
	status, body := machineRequest(t, s.controlTLS, http.MethodPost, s.machine+"/agents")
	require.Equal(t, http.StatusCreated, status, string(body))
	var created machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal(body, &created))
	return created.Grn
}

// TestDelegatedAuthority_RefusesPastAStaleCachedAllow holds that a Deny
// the engine's cache has not seen still refuses: another process writes
// it, the engine's ordinary Check goes on answering the allow it
// cached, and introspection and minting, which ask CheckCurrent,
// refuse.
func TestDelegatedAuthority_RefusesPastAStaleCachedAllow(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	s.grant(t, "g-1", nil, "agent:create")
	status, got := s.mint(t, s.member, "agent:create", s.controller.String(), "req-1")
	require.Equal(t, http.StatusCreated, status, got)
	minted := authorityOf(t, got)

	check := &authzv1.CheckRequest{
		SubjectGrn: s.memberGRN.String(), ObjectGrn: s.acme.String(), Action: "agent:create",
		Statements: []*authzv1.Statement{{Sid: "member", Effect: authzv1.Effect_EFFECT_ALLOW,
			Action: []string{"agent:create"}, Resource: []string{s.acme.String(), "grn:acme:*"}}},
		SubjectOrg: s.acme.String(),
	}
	client := authzv1.NewAuthzServiceClient(engine)
	cached, err := client.Check(ctx, check)
	require.NoError(t, err)
	require.True(t, cached.GetAllowed())

	// Another process's write: it drops its own cache, not the engine's.
	store := authz.NewPostgresStore(p)
	activePolicy(t, authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store)), s.acme, "deny-create",
		statementDoc("deny-create", wire.EffectDeny, "agent:create", s.acme.String()))
	stale, err := client.Check(ctx, check)
	require.NoError(t, err)
	require.True(t, stale.GetAllowed(), "the control: the ordinary check still answers its cached allow")

	status, _ = s.introspect(t, s.hostedTLS, minted.Authority)
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = s.mint(t, s.member, "agent:create", s.controller.String(), "req-1")
	assert.Equal(t, http.StatusForbidden, status)
}

// checkOnlyEngine is an engine predating CheckCurrent: it answers Check
// and nothing else.
type checkOnlyEngine struct {
	authzv1.UnimplementedAuthzServiceServer
	check authzv1.AuthzServiceServer
}

func (e checkOnlyEngine) Check(ctx context.Context, r *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	return e.check.Check(ctx, r)
}

// TestDelegatedAuthority_AnEngineWithoutCheckCurrentRefuses holds that
// an older engine, which would answer Check with a cached allow, makes
// every privileged call unavailable rather than answered from Check.
// Reading the grant, an ordinary cached read, is the control: the
// engine answers it.
func TestDelegatedAuthority_AnEngineWithoutCheckCurrentRefuses(t *testing.T) {
	p := pool(t)
	srv := grpc.NewServer()
	authzv1.RegisterAuthzServiceServer(srv, checkOnlyEngine{check: authz.NewGRPCServer(authz.NewPolicyCache(authz.NewPostgresStore(p)))})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	engine, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = engine.Close() })
	s := newAuthorityStack(t, engine, p)

	body := `{"requestId":"g-1","controllers":["` + s.controller.String() + `"],"operations":["agent:create"],"expiresAt":"` +
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`
	status, got := call(t, http.DefaultClient, http.MethodPut, s.delegationPath(), s.owner, body)
	assert.Equal(t, http.StatusServiceUnavailable, status, got)
	assert.Contains(t, got, `"message":"internal error"`)

	status, got = call(t, http.DefaultClient, http.MethodGet, s.delegationPath(), s.member, "")
	assert.Equal(t, http.StatusNotFound, status, "the ordinary read reached the engine and the store: %s", got)
}
