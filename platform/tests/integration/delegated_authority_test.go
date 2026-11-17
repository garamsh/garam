package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/cli"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policydecision"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// authorityStack is both listeners as the serve commands compose them,
// over one database and one policy engine: the browser-facing surface
// over HTTP and the machine surface over TLS, where each operator
// presents its token. The delegated authority
// on each is cli.NewAuthorityService, the composition both commands
// run.
type authorityStack struct {
	pool       *pgxpool.Pool
	decisions  *currentDecisions
	browser    string
	machine    string
	acme       grn.GRN
	owner      *http.Cookie
	ownerGRN   grn.GRN
	member     *http.Cookie
	memberGRN  grn.GRN
	hosted     grn.GRN
	controller grn.GRN
	hostedTLS  *http.Client
	controlTLS *http.Client
}

func newAuthorityStack(t *testing.T, p *pgxpool.Pool) *authorityStack {
	t.Helper()
	decisions := newCurrentDecisions(p)
	s := newAuthorityStackDeciding(t, p, decisions)
	s.decisions = decisions
	return s
}

// newAuthorityStackDeciding is newAuthorityStack where decisions is
// the policy service both listeners ask, which the suite tagged
// policyservice gives the real one.
func newAuthorityStackDeciding(t *testing.T, p *pgxpool.Pool, decisions policydecision.Service) *authorityStack {
	t.Helper()
	ctx := context.Background()
	orgs := orgpg.New(p)
	users := userpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	session := func(id string, role org.Role) (*http.Cookie, grn.GRN) {
		g := grn.New(grn.RootOwner, user.ServiceType, id)
		require.NoError(t, users.Create(ctx, user.New(g, id)))
		require.NoError(t, orgs.AddMember(ctx, org.NewMembership(acme, g, role)))
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, authnpg.NewSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}, g
	}
	owner, ownerGRN := session("ada", org.RoleOwner)
	member, memberGRN := session("bob", org.RoleMember)

	srv := api.NewServer(users, orgs, nil, operatorpg.New(p), nil, nil,
		authnpg.NewSessionStore(p), nil, nil, nil, nil, decisions, nil, cli.NewAuthorityService(p, decisions), nil, api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)

	_, sealer := keyEncryptionKey(t)
	hosted := scopedGRN("acme", operator.ServiceType, "hosted")
	controller := scopedGRN("acme", operator.ServiceType, "controller")
	machine := machineListenerDeciding(t, p, sealer, nil, decisions)
	return &authorityStack{
		pool: p, browser: browser.URL, machine: machine,
		acme: acme, owner: owner, ownerGRN: ownerGRN, member: member, memberGRN: memberGRN, hosted: hosted, controller: controller,
		hostedTLS:  operatorClient(t, p, machine, hosted),
		controlTLS: operatorClient(t, p, machine, controller),
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

// machineCall is call on the machine listener, which serves
// operation-authority.v2 alone (ADR-0118); every answer names it.
func machineCall(t *testing.T, client *http.Client, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(operator.ContractVersionHeader, operator.TokenAuthorityContractVersion)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, operator.TokenAuthorityContractVersion, resp.Header.Get(operator.ContractVersionHeader),
		"every contract answer carries the version")
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
	return machineCall(t, client, http.MethodPost, s.machine+"/operation-authorities/introspection",
		`{"authority":"`+authority+`"}`)
}

// TestDelegatedAuthority_EndToEnd crosses both listeners, the policy
// engine and Postgres: an owner delegates, a member hands the hosted
// operator one creation, and the hosted operator reads it back over its
// own certificate. Each refusal sits beside the call that succeeds.
func TestDelegatedAuthority_EndToEnd(t *testing.T) {
	p := pool(t)
	s := newAuthorityStack(t, p)

	body := `{"requestId":"m-1","controllers":["` + s.controller.String() + `"],"operations":["iam:agent.create"],"expiresAt":"` +
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`
	status, got := call(t, http.DefaultClient, http.MethodPut, s.delegationPath(), s.member, body)
	assert.Equal(t, http.StatusForbidden, status, "a member does not delegate: %s", got)
	d := s.grant(t, "g-1", nil, "iam:agent.create", "iam:agent.configure")

	status, got = s.mint(t, s.member, "iam:agent.create", s.controller.String(), "req-1")
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
	status, _ = machineCall(t, s.controlTLS, http.MethodGet, s.machine+"/operation-references/"+minted.OperationRef, "")
	assert.Equal(t, http.StatusNotFound, status)
	status, _ = machineCall(t, s.hostedTLS, http.MethodGet, s.machine+"/operation-references/"+minted.OperationRef, "")
	assert.Equal(t, http.StatusOK, status)

	agentGRN := s.provisionAgent(t)
	status, got = s.mint(t, s.member, "iam:agent.configure", agentGRN, "req-2")
	assert.Equal(t, http.StatusForbidden, status, "a member does not configure: %s", got)
	status, got = s.mint(t, s.owner, "iam:agent.configure", agentGRN, "req-2")
	require.Equal(t, http.StatusCreated, status, got)
	configure := authorityOf(t, got)

	// Membership is read when introspection asks: the member who left
	// is refused, and the owner's authority is the control.
	require.NoError(t, orgpg.New(s.pool).RemoveMember(context.Background(), s.acme, s.memberGRN))
	status, _ = s.introspect(t, s.hostedTLS, minted.Authority)
	assert.Equal(t, http.StatusForbidden, status, "the actor left the organization")
	status, got = s.introspect(t, s.hostedTLS, configure.Authority)
	require.Equal(t, http.StatusOK, status, got)
	assert.Contains(t, got, `"assignment":{"epoch":"`)

	status, got = call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", s.member,
		`{"audience":"grn:globex/iam/operator/hosted","operation":"iam:agent.create","target":"`+s.controller.String()+
			`","requestId":"req-3","bodySha256":"`+strings.Repeat("c", 64)+`"}`)
	assert.Equal(t, http.StatusBadRequest, status, "another organization's audience: %s", got)

	// The owner's configuration authority answered just above, so a
	// refusal after the withdrawal is the withdrawal's.
	status, got = call(t, http.DefaultClient, http.MethodDelete, s.delegationPath(), s.owner, `{"grantId":"`+d.GrantId+`"}`)
	require.Equal(t, http.StatusNoContent, status, got)
	status, _ = s.introspect(t, s.hostedTLS, configure.Authority)
	assert.Equal(t, http.StatusForbidden, status, "withdrawn")
	status, _ = call(t, http.DefaultClient, http.MethodDelete, s.delegationPath(), s.owner, `{"grantId":"`+d.GrantId+`"}`)
	assert.Equal(t, http.StatusNoContent, status, "a retried withdrawal")
}

// provisionAgent stands an agent up assigned to the controller.
func (s *authorityStack) provisionAgent(t *testing.T) string {
	t.Helper()
	agents := agentpg.New(s.pool)
	g := seedAgent(t, agents, grn.Org(s.controller.Organization))
	_, err := agents.Assign(context.Background(), g, s.controller)
	require.NoError(t, err)
	return g.String()
}

// TestDelegatedAuthority_RefusesWhatTheServiceNowDenies holds that the
// authority a member minted is decided by the policy service when
// introspection and minting ask, read current (ADR-0146 item 14): a
// denial the service gives after the authority was minted refuses
// both, and every question went to the service as read-current. The
// first mint and the owner's authority are the controls.
func TestDelegatedAuthority_RefusesWhatTheServiceNowDenies(t *testing.T) {
	p := pool(t)
	s := newAuthorityStack(t, p)
	s.grant(t, "g-1", nil, "iam:agent.create")
	status, got := s.mint(t, s.member, "iam:agent.create", s.controller.String(), "req-1")
	require.Equal(t, http.StatusCreated, status, got)
	minted := authorityOf(t, got)
	status, got = s.mint(t, s.owner, "iam:agent.create", s.controller.String(), "req-2")
	require.Equal(t, http.StatusCreated, status, got)
	ownerMinted := authorityOf(t, got)

	s.decisions.forbid(s.memberGRN, "iam:agent.create")

	status, _ = s.introspect(t, s.hostedTLS, minted.Authority)
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = s.mint(t, s.member, "iam:agent.create", s.controller.String(), "req-1")
	assert.Equal(t, http.StatusForbidden, status)
	status, got = s.introspect(t, s.hostedTLS, ownerMinted.Authority)
	assert.Equal(t, http.StatusOK, status, "the owner is not forbidden: %s", got)

	asked := s.decisions.currentQuestions()
	require.NotEmpty(t, asked)
	for _, q := range asked {
		assert.True(t, q.Current, "%s asked for %s was not read current", q.Subject, q.Action)
	}
}

// TestDelegatedAuthority_RechecksRoles holds that the role an
// introspection decides on is read when it asks: a high-trust authority
// minted by an admin is refused once the membership is demoted. The
// owner's authority, which the change does not reach, is the control.
func TestDelegatedAuthority_RechecksRoles(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := newAuthorityStack(t, p)
	orgs := orgpg.New(p)
	users := userpg.New(p)
	s.grant(t, "g-1", nil, "iam:agent.create", "iam:agent.configure")
	agentGRN := s.provisionAgent(t)

	cookieFor := func(g grn.GRN) *http.Cookie {
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, authnpg.NewSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}
	}
	admin := grn.New(grn.RootOwner, user.ServiceType, "carol")
	require.NoError(t, users.Create(ctx, user.New(admin, "carol")))
	require.NoError(t, orgs.AddMember(ctx, org.NewMembership(s.acme, admin, org.RoleAdmin)))

	minted := func(cookie *http.Cookie, operation, target, requestID string) string {
		status, got := s.mint(t, cookie, operation, target, requestID)
		require.Equal(t, http.StatusCreated, status, got)
		return authorityOf(t, got).Authority
	}
	byAdmin := minted(cookieFor(admin), "iam:agent.configure", agentGRN, "admin-1")
	ownerConfigure := minted(s.owner, "iam:agent.configure", agentGRN, "owner-1")
	for _, a := range []string{byAdmin, ownerConfigure} {
		status, got := s.introspect(t, s.hostedTLS, a)
		require.Equal(t, http.StatusOK, status, got)
	}

	require.NoError(t, orgs.RemoveMember(ctx, s.acme, admin))
	require.NoError(t, orgs.AddMember(ctx, org.NewMembership(s.acme, admin, org.RoleMember)))
	status, _ := s.introspect(t, s.hostedTLS, byAdmin)
	assert.Equal(t, http.StatusForbidden, status, "membership demoted to member")
	status, got := s.introspect(t, s.hostedTLS, ownerConfigure)
	assert.Equal(t, http.StatusOK, status, "the owner is not demoted: %s", got)
}

// TestDelegatedAuthority_AServiceFailureRefuses holds that a policy
// service that cannot answer makes every privileged call unavailable
// and never an allow, and that the failure's own kind does not turn it
// into a denial or into the caller's fault (ADR-0146 item 2). The
// browser listener's own enforcement asks the same service (ADR-0171),
// so reading the grant is unavailable too once it fails; the read
// before the failure is the control, answered by the store.
func TestDelegatedAuthority_AServiceFailureRefuses(t *testing.T) {
	p := pool(t)
	s := newAuthorityStack(t, p)
	status, got := call(t, http.DefaultClient, http.MethodGet, s.delegationPath(), s.member, "")
	require.Equal(t, http.StatusNotFound, status, "control: with the service answering, the read reaches the store: %s", got)
	s.decisions.failWith(errorx.New(errorx.KindPermissionDenied, "the service refused this caller"))

	body := `{"requestId":"g-1","controllers":["` + s.controller.String() + `"],"operations":["iam:agent.create"],"expiresAt":"` +
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`
	status, got = call(t, http.DefaultClient, http.MethodPut, s.delegationPath(), s.owner, body)
	assert.Equal(t, http.StatusServiceUnavailable, status, got)
	assert.Contains(t, got, `"message":"internal error"`)

	status, got = call(t, http.DefaultClient, http.MethodGet, s.delegationPath(), s.member, "")
	assert.Equal(t, http.StatusServiceUnavailable, status, "the read's enforcement asks the service: %s", got)
}
