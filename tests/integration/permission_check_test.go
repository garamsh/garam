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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
	"github.com/garamsh/garam/internal/wire"
)

// checkStack is the browser-facing listener over real Postgres and a
// real policy engine, with an owner and a member of acme signed in.
type checkStack struct {
	url      string
	acme     grn.GRN
	owner    *http.Cookie
	ownerGRN grn.GRN
	member   *http.Cookie
	policies *authz.PolicyGRPCServer
}

func newCheckStack(t *testing.T) *checkStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	policies, engine := policyEngine(t, p)
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
	owner, ownerGRN := session("ada", org.RoleOwner)
	member, _ := session("bob", org.RoleMember)

	srv := api.NewServer(users, orgs, nil, nil, message.NewPostgresStore(p), nil,
		authn.NewPostgresSessionStore(p), nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), nil, nil, cli.NewAuthorityService(p, engine), nil, api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	return &checkStack{url: browser.URL, acme: acme, owner: owner, ownerGRN: ownerGRN, member: member,
		policies: policies}
}

// send makes one request as cookie and answers the status and body.
func (s *checkStack) send(t *testing.T, method, path string, cookie *http.Cookie, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.url+path, strings.NewReader(body))
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

// check asks about actions on acme itself as cookie and answers one
// decision per action, in order.
func (s *checkStack) check(t *testing.T, cookie *http.Cookie, actions ...string) []apigen.PermissionDecision {
	t.Helper()
	return permissionDecisions(t, s.url, cookie, actions...)
}

// permissionDecisions asks the browser-facing listener at url about
// actions on acme itself as cookie and answers one decision per action,
// in order.
func permissionDecisions(t *testing.T, url string, cookie *http.Cookie, actions ...string) []apigen.PermissionDecision {
	t.Helper()
	checks := make([]apigen.PermissionCheck, len(actions))
	for i, a := range actions {
		checks[i] = apigen.PermissionCheck{Action: a}
	}
	return checkedDecisions(t, url, cookie, checks...)
}

// checkedDecisions asks the browser-facing listener at url about the
// pairs in acme as cookie and answers one decision per pair, in order.
func checkedDecisions(t *testing.T, url string, cookie *http.Cookie, checks ...apigen.PermissionCheck) []apigen.PermissionDecision {
	t.Helper()
	raw, err := json.Marshal(apigen.PermissionCheckRequest{Checks: checks})
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, url+"/orgs/acme/permission-checks", strings.NewReader(string(raw)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var answer apigen.PermissionCheckAnswer
	require.NoError(t, json.Unmarshal(body, &answer))
	decisions := make([]apigen.PermissionDecision, len(answer.Results))
	for i, r := range answer.Results {
		decisions[i] = r.Decision
	}
	return decisions
}

// TestCheckPermissions_AnswersAsEnforcementDecides asks the engine for
// an owner and a member and holds each answer against what the same
// caller is answered when it makes the request. Listing consents is
// the request: owners hold `consent:*` and members nothing of it. The
// owner's allow is the control for the member's deny, so what denies
// the member is the role rather than an operation that refuses all.
func TestCheckPermissions_AnswersAsEnforcementDecides(t *testing.T) {
	s := newCheckStack(t)

	owner := s.check(t, s.owner, org.ActionOperatorRegister, org.ActionConsentRead)
	member := s.check(t, s.member, org.ActionOperatorRegister, org.ActionConsentRead)
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionAllow, apigen.DecisionAllow}, owner, "an owner registers operators and reads consents")
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionDeny}, member, "a member does neither")

	ownerStatus, _ := s.send(t, http.MethodGet, "/orgs/acme/consents", s.owner, "")
	memberStatus, _ := s.send(t, http.MethodGet, "/orgs/acme/consents", s.member, "")
	assert.Equal(t, http.StatusOK, ownerStatus, "enforcement allows what the check allowed")
	assert.Equal(t, http.StatusForbidden, memberStatus, "enforcement refuses what the check denied")
}

// TestCheckPermissions_ReadsAttachedPolicies holds that the answer
// comes from the engine's policies and not from the role alone: a Deny
// attached to the owner turns an allow into a deny. The answer before
// the attachment is the control.
func TestCheckPermissions_ReadsAttachedPolicies(t *testing.T) {
	s := newCheckStack(t)
	require.Equal(t, []apigen.PermissionDecision{apigen.DecisionAllow}, s.check(t, s.owner, org.ActionOperatorRegister))

	denied := activePolicy(t, s.policies, s.acme, "deny-register",
		statementDoc("deny-register", wire.EffectDeny, org.ActionOperatorRegister, s.acme.String()))
	attachPolicy(t, s.policies, denied, s.ownerGRN)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny}, s.check(t, s.owner, org.ActionOperatorRegister),
		"a Deny attached to the owner wins over the owner's role")
}

// TestCheckPermissions_DeniesAnUndefinedAction asks the owner, whose
// role allows `org:*`, about a name that pattern matches and nothing
// defines. The defined `org:delete` matches the same pattern and is
// the control.
func TestCheckPermissions_DeniesAnUndefinedAction(t *testing.T) {
	s := newCheckStack(t)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionAllow}, s.check(t, s.owner, "org:frobnicate", org.ActionOrgDelete))
}

// TestCheckPermissions_AsksAtMostFifty answers fifty pairs and refuses
// fifty-one: the same pairs, one fewer, are the control.
func TestCheckPermissions_AsksAtMostFifty(t *testing.T) {
	s := newCheckStack(t)
	pairs := func(n int) string {
		raw, err := json.Marshal(apigen.PermissionCheckRequest{
			Checks: make([]apigen.PermissionCheck, n),
		})
		require.NoError(t, err)
		return strings.ReplaceAll(string(raw), `"action":""`, `"action":"org:read"`)
	}

	status, body := s.send(t, http.MethodPost, "/orgs/acme/permission-checks", s.member, pairs(50))
	require.Equal(t, http.StatusOK, status, body)
	status, _ = s.send(t, http.MethodPost, "/orgs/acme/permission-checks", s.member, pairs(51))
	assert.Equal(t, http.StatusBadRequest, status)
}

// TestCheckPermissions_LeavesGrantAndCurrentActionsUnknown asks the
// owner, whose role allows each of them, about an action a delegation
// grant decides and one that current policy decides: each is unknown,
// because what enforcement asks for them is more than this operation
// reads — the grant, or current policy.
// operator:register, which Check alone decides, is the control.
func TestCheckPermissions_LeavesGrantAndCurrentActionsUnknown(t *testing.T) {
	s := newCheckStack(t)

	assert.Equal(t, []apigen.PermissionDecision{
		apigen.DecisionUnknown, apigen.DecisionUnknown, apigen.DecisionAllow,
	}, s.check(t, s.owner, org.ActionAgentCreate, org.ActionAgentEnroll, org.ActionOperatorRegister))
}

// TestCheckPermissions_AnswersDelegationAsItsWritesDecide holds the
// operator:delegate answer beside the three writes it describes, over
// the authority service both serve commands compose. A member's role
// does not hold it, so the check answers deny and writing the grant,
// withdrawing it and setting the control endpoint are each refused.
// The owner is the control for each: allow, and every write lands.
func TestCheckPermissions_AnswersDelegationAsItsWritesDecide(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	endpointPath := s.browser + "/orgs/acme/operators/hosted/control-endpoint"
	grantBody := `{"requestId":"m-1","controllers":["` + s.controller.String() + `"],"operations":["agent:create"],"expiresAt":"` +
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny},
		permissionDecisions(t, s.browser, s.member, org.ActionOperatorDelegate))
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionAllow},
		permissionDecisions(t, s.browser, s.owner, org.ActionOperatorDelegate))

	status, got := call(t, http.DefaultClient, http.MethodPut, s.delegationPath(), s.member, grantBody)
	assert.Equal(t, http.StatusForbidden, status, "the member writes no grant: %s", got)
	d := s.grant(t, "g-1", nil, org.ActionAgentCreate)

	status, got = call(t, http.DefaultClient, http.MethodPut, endpointPath, s.member, `{"url":"https://control.example.com/garam"}`)
	assert.Equal(t, http.StatusForbidden, status, "the member sets no endpoint: %s", got)
	status, got = call(t, http.DefaultClient, http.MethodPut, endpointPath, s.owner, `{"url":"https://control.example.com/garam"}`)
	assert.Equal(t, http.StatusOK, status, "the owner sets it: %s", got)

	status, got = call(t, http.DefaultClient, http.MethodDelete, s.delegationPath(), s.member, `{"grantId":"`+d.GrantId+`"}`)
	assert.Equal(t, http.StatusForbidden, status, "the member withdraws nothing: %s", got)
	status, got = call(t, http.DefaultClient, http.MethodDelete, s.delegationPath(), s.owner, `{"grantId":"`+d.GrantId+`"}`)
	assert.Equal(t, http.StatusNoContent, status, "the owner withdraws it: %s", got)
}

// TestCheckPermissions_AsksAnAgentActionOnTheAgentNamed holds that an
// agent-targeted action is asked on the agent the pair names, as its
// mint decides it. A policy allows the member agent:configure on one
// agent alone: that agent is answered unknown and minted, and another
// agent of the same controller, the control, is answered deny and
// refused.
func TestCheckPermissions_AsksAnAgentActionOnTheAgentNamed(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	s.grant(t, "g-1", nil, org.ActionAgentConfigure)
	allowed, other := s.provisionAgent(t), s.provisionAgent(t)
	configurer := activePolicy(t, policies, s.acme, "configure-one",
		statementDoc("configure-one", wire.EffectAllow, org.ActionAgentConfigure, allowed))
	attachPolicy(t, policies, configurer, s.memberGRN)
	on := func(agent string) apigen.PermissionCheck {
		g, err := grn.Parse(agent)
		require.NoError(t, err)
		return apigen.PermissionCheck{Action: org.ActionAgentConfigure,
			Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeAgent, Id: g.Identifier}}
	}

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionUnknown, apigen.DecisionDeny},
		checkedDecisions(t, s.browser, s.member, on(allowed), on(other)))
	status, got := s.mint(t, s.member, org.ActionAgentConfigure, allowed, "configure-allowed")
	assert.Equal(t, http.StatusCreated, status, "the agent the policy names: %s", got)
	status, got = s.mint(t, s.member, org.ActionAgentConfigure, other, "configure-other")
	assert.Equal(t, http.StatusForbidden, status, "another agent: %s", got)
}

// TestCheckPermissions_DeniesAGrantActionItsMintDenies holds the check
// beside the mint it describes, over the authority service both serve
// commands compose and a grant covering both operations. A member's
// role does not hold agent-template:publish, so the check answers deny
// and the mint refuses. The owner's is the control: the check answers
// unknown, since it reads no grant, and the mint, which reads one,
// succeeds. agent:create, which a member's role holds, is the control
// for the member: unknown, and minted.
func TestCheckPermissions_DeniesAGrantActionItsMintDenies(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	s.grant(t, "g-1", nil, org.ActionAgentTemplatePublish, org.ActionAgentCreate)
	publish := func(cookie *http.Cookie, requestID string) int {
		status, _ := call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", cookie,
			`{"audience":"`+s.hosted.String()+`","operation":"agent-template:publish","target":"`+s.acme.String()+
				`","requestId":"`+requestID+`","bodySha256":"`+strings.Repeat("c", 64)+
				`","requestTarget":"/agent-templates/web/versions"}`)
		return status
	}

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionUnknown},
		permissionDecisions(t, s.browser, s.member, org.ActionAgentTemplatePublish, org.ActionAgentCreate))
	assert.Equal(t, http.StatusForbidden, publish(s.member, "pub-member"), "the mint refuses what the check denied")
	status, got := s.mint(t, s.member, org.ActionAgentCreate, s.controller.String(), "create-member")
	assert.Equal(t, http.StatusCreated, status, "the mint the check left to the grant: %s", got)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionUnknown},
		permissionDecisions(t, s.browser, s.owner, org.ActionAgentTemplatePublish))
	assert.Equal(t, http.StatusCreated, publish(s.owner, "pub-owner"), "the grant the check did not read allows the owner")
}
