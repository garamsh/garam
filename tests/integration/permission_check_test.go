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
		api.NewGRPCEvaluator(engine), nil, nil, nil, nil, api.Config{SessionTTL: time.Hour})
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
	checks := make([]apigen.PermissionCheck, len(actions))
	for i, a := range actions {
		checks[i] = apigen.PermissionCheck{Action: a}
	}
	raw, err := json.Marshal(apigen.PermissionCheckRequest{Checks: checks})
	require.NoError(t, err)
	status, body := s.send(t, http.MethodPost, "/orgs/acme/permission-checks", cookie, string(raw))
	require.Equal(t, http.StatusOK, status, body)
	var answer apigen.PermissionCheckAnswer
	require.NoError(t, json.Unmarshal([]byte(body), &answer))
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
// grant decides and two that current policy decides: each is unknown,
// because the engine's Check is not what enforcement asks for them.
// operator:register, which Check alone decides, is the control.
func TestCheckPermissions_LeavesGrantAndCurrentActionsUnknown(t *testing.T) {
	s := newCheckStack(t)

	assert.Equal(t, []apigen.PermissionDecision{
		apigen.DecisionUnknown, apigen.DecisionUnknown, apigen.DecisionUnknown, apigen.DecisionAllow,
	}, s.check(t, s.owner,
		org.ActionAgentCreate, org.ActionOperatorDelegate, org.ActionAgentEnroll, org.ActionOperatorRegister))
}
