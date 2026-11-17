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

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/cli"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// checkStack is the browser-facing listener over real Postgres and a
// real policy engine, with an owner and a member of acme signed in,
// and an outsider signed in who belongs to no organization.
type checkStack struct {
	url       string
	acme      grn.GRN
	owner     *http.Cookie
	ownerGRN  grn.GRN
	member    *http.Cookie
	memberGRN grn.GRN
	outsider  *http.Cookie
	policies  *authz.PolicyGRPCServer
	decisions *currentDecisions
}

func newCheckStack(t *testing.T) *checkStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	policies, _ := policyEngine(t, p)
	orgs := orgpg.New(p)
	users := userpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	session := func(id string, role org.Role) (*http.Cookie, grn.GRN) {
		g := grn.New(grn.RootOwner, user.ServiceType, id)
		require.NoError(t, users.Create(ctx, user.New(g, id)))
		if role != "" {
			require.NoError(t, orgs.AddMember(ctx, org.NewMembership(acme, g, role)))
		}
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, authnpg.NewSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}, g
	}
	owner, ownerGRN := session("ada", org.RoleOwner)
	member, memberGRN := session("bob", org.RoleMember)
	outsider, _ := session("eve", "")

	decisions := newCurrentDecisions(p)
	srv := api.NewServer(users, orgs, nil, nil, messagepg.New(p), nil,
		authnpg.NewSessionStore(p), nil, nil, nil, nil, decisions, nil, cli.NewAuthorityService(p, decisions), nil, api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	return &checkStack{url: browser.URL, acme: acme, owner: owner, ownerGRN: ownerGRN, member: member,
		memberGRN: memberGRN, outsider: outsider, policies: policies, decisions: decisions}
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
// the request: owners hold `iam:consent.*` and members nothing of it.
// The owner's allow is the control for the member's deny, so what
// denies the member is the role rather than an operation that refuses
// all.
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

// TestCheckPermissions_AsksThePolicyServiceNotTheRoleAlone holds that
// the answer is the policy service's and not the role's: the service
// forbidding the owner an action its role carries turns an allow into
// a deny, and the route refuses it the same way. The answer before the
// forbid is the control (the guardrail itself is written through the
// relay and shown against the real service in policy_enforcement_test.go).
func TestCheckPermissions_AsksThePolicyServiceNotTheRoleAlone(t *testing.T) {
	s := newCheckStack(t)
	require.Equal(t, []apigen.PermissionDecision{apigen.DecisionAllow}, s.check(t, s.owner, org.ActionOperatorRegister))

	s.decisions.forbid(s.ownerGRN, org.ActionOperatorRegister)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny}, s.check(t, s.owner, org.ActionOperatorRegister),
		"a forbid of the service wins over the owner's role")
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionAllow}, s.check(t, s.owner, org.ActionOrgRead),
		"control: another action is not forbidden")
}

// TestCheckPermissions_DeniesAnUndefinedAction asks the owner, whose
// role allows `iam:org.*`, about a name that pattern matches and
// nothing defines. The defined `iam:org.delete` matches the same
// pattern and is the control.
func TestCheckPermissions_DeniesAnUndefinedAction(t *testing.T) {
	s := newCheckStack(t)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionAllow}, s.check(t, s.owner, "iam:org.frobnicate", org.ActionOrgDelete))
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
		return strings.ReplaceAll(string(raw), `"action":""`, `"action":"iam:org.read"`)
	}

	status, body := s.send(t, http.MethodPost, "/orgs/acme/permission-checks", s.member, pairs(50))
	require.Equal(t, http.StatusOK, status, body)
	status, _ = s.send(t, http.MethodPost, "/orgs/acme/permission-checks", s.member, pairs(51))
	assert.Equal(t, http.StatusBadRequest, status)
}

// TestCheckPermissions_LeavesGrantActionsUnknown asks the owner, whose
// role allows it, about an action a delegation grant decides: it is
// unknown, because enforcement also reads the grant, which this
// operation does not. iam:operator.register, which Check alone decides,
// is the control.
func TestCheckPermissions_LeavesGrantActionsUnknown(t *testing.T) {
	s := newCheckStack(t)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionUnknown, apigen.DecisionAllow},
		s.check(t, s.owner, org.ActionAgentCreate, org.ActionOperatorRegister))
}

// TestCheckPermissions_AnswersDelegationAsItsWritesDecide holds the
// iam:operator.delegate answer beside the three writes it describes,
// over the authority service both serve commands compose. A member's
// role does not hold it, so the check answers deny and writing the
// grant, withdrawing it and setting the control endpoint are each
// refused. The owner is the control for each: allow, and every write
// lands.
func TestCheckPermissions_AnswersDelegationAsItsWritesDecide(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	endpointPath := s.browser + "/orgs/acme/operators/hosted/control-endpoint"
	grantBody := `{"requestId":"m-1","controllers":["` + s.controller.String() + `"],"operations":["iam:agent.create"],"expiresAt":"` +
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

// TestCheckPermissions_AnswersDelegationOnOperatorDelegateAlone holds
// that what the check and the writes ask for is iam:operator.delegate
// itself. The policy service denies the owner another action: the
// check stays allow and the owner sets the endpoint, the control.
// Denying iam:operator.delegate turns the check to deny and the write
// to a refusal, and every question asked was about that action.
func TestCheckPermissions_AnswersDelegationOnOperatorDelegateAlone(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	endpointPath := s.browser + "/orgs/acme/operators/hosted/control-endpoint"
	put := func() (int, string) {
		return call(t, http.DefaultClient, http.MethodPut, endpointPath, s.owner, `{"url":"https://control.example.com/garam"}`)
	}

	s.decisions.forbid(s.ownerGRN, org.ActionAgentCreate)
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionAllow},
		permissionDecisions(t, s.browser, s.owner, org.ActionOperatorDelegate))
	status, got := put()
	assert.Equal(t, http.StatusOK, status, "another action's denial leaves the write: %s", got)
	for _, q := range s.decisions.currentQuestions() {
		assert.Equal(t, org.ActionOperatorDelegate, q.Action, "the check and the write ask iam:operator.delegate alone")
	}

	s.decisions.forbid(s.ownerGRN, org.ActionOperatorDelegate)
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny},
		permissionDecisions(t, s.browser, s.owner, org.ActionOperatorDelegate))
	status, got = put()
	assert.Equal(t, http.StatusForbidden, status, "the denial refuses the write: %s", got)
}

// TestCheckPermissions_AsksAnAgentActionOnTheAgentNamed holds that an
// agent-targeted action is asked on the agent the pair names, as its
// mint decides it. The owner's check on each of two agents of one
// controller is answered unknown and each is minted, the control; the
// policy service asked about each agent by name. Denying the owner
// iam:agent.configure then answers both deny and refuses both mints.
func TestCheckPermissions_AsksAnAgentActionOnTheAgentNamed(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	s.grant(t, "g-1", nil, org.ActionAgentConfigure)
	allowed, other := s.provisionAgent(t), s.provisionAgent(t)
	on := func(agent string) apigen.PermissionCheck {
		g, err := grn.Parse(agent)
		require.NoError(t, err)
		return apigen.PermissionCheck{Action: org.ActionAgentConfigure,
			Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeAgent, Id: g.Identifier}}
	}

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionUnknown, apigen.DecisionUnknown},
		checkedDecisions(t, s.browser, s.owner, on(allowed), on(other)))
	status, got := s.mint(t, s.owner, org.ActionAgentConfigure, allowed, "configure-allowed")
	assert.Equal(t, http.StatusCreated, status, "the first agent: %s", got)
	var asked []string
	for _, q := range s.decisions.currentQuestions() {
		asked = append(asked, q.Resource.String())
	}
	assert.Contains(t, asked, allowed, "the service was asked about the agent named")
	assert.Contains(t, asked, other, "and about the other agent")

	s.decisions.forbid(s.ownerGRN, org.ActionAgentConfigure)
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionDeny},
		checkedDecisions(t, s.browser, s.owner, on(allowed), on(other)))
	status, got = s.mint(t, s.owner, org.ActionAgentConfigure, other, "configure-other")
	assert.Equal(t, http.StatusForbidden, status, "the denial refuses the mint: %s", got)
}

// TestCheckPermissions_DeniesAGrantActionItsMintDenies holds the check
// beside the mint it describes, over the authority service both serve
// commands compose and a grant covering both operations. A member's
// role does not hold iam:agent-template.publish, so the check answers
// deny and the mint refuses. The owner's is the control: the check
// answers unknown, since it reads no grant, and the mint, which reads
// one, succeeds. iam:agent.create, which a member's role holds, is the
// control for the member: unknown, and minted.
func TestCheckPermissions_DeniesAGrantActionItsMintDenies(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	s.grant(t, "g-1", nil, org.ActionAgentTemplatePublish, org.ActionAgentCreate)
	publish := func(cookie *http.Cookie, requestID string) int {
		status, _ := call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", cookie,
			`{"audience":"`+s.hosted.String()+`","operation":"iam:agent-template.publish","target":"`+s.acme.String()+
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

// TestCheckPermissions_AsksTheServiceAsAProbeAndAnActionDoesNot holds
// that a console check, which only shows what the person may do, asks
// the policy service as a probe so no denial is recorded for it
// (ADR-0148), while the write or the mint it describes asks as an
// action.
func TestCheckPermissions_AsksTheServiceAsAProbeAndAnActionDoesNot(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	counts := func() (probes, actions int) {
		for _, q := range s.decisions.currentQuestions() {
			if q.Probe {
				probes++
			} else {
				actions++
			}
		}
		return probes, actions
	}

	permissionDecisions(t, s.browser, s.owner, org.ActionOperatorDelegate)
	probes, actions := counts()
	assert.Equal(t, 1, probes, "the delegation check asks as a probe")
	assert.Zero(t, actions, "and asks nothing as an action")

	s.grant(t, "g-1", nil, org.ActionAgentCreate)
	_, written := counts()
	assert.Positive(t, written, "writing the grant asks as an action")

	permissionDecisions(t, s.browser, s.member, org.ActionAgentCreate)
	probes, actions = counts()
	assert.Equal(t, 2, probes, "the mint check asks as a probe")
	assert.Equal(t, written, actions, "and adds no action")

	status, got := s.mint(t, s.member, org.ActionAgentCreate, s.controller.String(), "create-member")
	require.Equal(t, http.StatusCreated, status, got)
	probes, actions = counts()
	assert.Equal(t, 2, probes, "the mint adds no probe")
	assert.Greater(t, actions, written, "the mint asks as an action")
}
