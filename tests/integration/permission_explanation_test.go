package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/grn"
	messagepg "github.com/garamsh/garam/internal/message/postgres"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
	"github.com/garamsh/garam/internal/wire"
)

// explainStack is the browser-facing listener over real Postgres and a
// real policy engine. acme holds an owner, ada, and two members, bob
// and carol; ada and bob belong to the group eng, and acme has one
// agent.
type explainStack struct {
	url                      string
	acme                     grn.GRN
	eng                      grn.GRN
	agent                    grn.GRN
	policies                 *authz.PolicyGRPCServer
	orgs                     *orgpg.Store
	ada, bob, carol          *http.Cookie
	adaGRN, bobGRN, carolGRN grn.GRN
}

func newExplainStack(t *testing.T) *explainStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	policies, engine := policyEngine(t, p)
	orgs := orgpg.New(p)
	users := userpg.New(p)
	agents := agentpg.New(p)
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
	s := &explainStack{acme: acme, policies: policies, orgs: orgs}
	s.ada, s.adaGRN = session("ada", org.RoleOwner)
	s.bob, s.bobGRN = session("bob", org.RoleMember)
	s.carol, s.carolGRN = session("carol", org.RoleMember)
	s.eng = grn.New("acme", grn.DefaultProject, org.ServiceTypeGroup, "eng")
	require.NoError(t, orgs.CreateGroup(ctx, org.NewGroup(s.eng, "eng", acme)))
	require.NoError(t, orgs.AddGroupMember(ctx, s.eng, s.adaGRN))
	require.NoError(t, orgs.AddGroupMember(ctx, s.eng, s.bobGRN))
	s.agent = seedAgent(t, agents, acme)

	srv := api.NewServer(users, orgs, agents, nil, messagepg.New(p), nil,
		authn.NewPostgresSessionStore(p), nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), nil, nil, cli.NewAuthorityService(p, engine), cli.NewStandaloneService(p, engine),
		api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	s.url = browser.URL
	return s
}

// get makes one GET as cookie and answers the status and body.
func (s *explainStack) get(t *testing.T, path string, query url.Values, cookie *http.Cookie) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.url+path+"?"+query.Encode(), nil)
	require.NoError(t, err)
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// explain asks why cookie may or may not take action on acme itself.
func (s *explainStack) explain(t *testing.T, cookie *http.Cookie, action string) apigen.PermissionExplanation {
	t.Helper()
	status, body := s.get(t, "/orgs/acme/permission-explanation", url.Values{"action": {action}}, cookie)
	require.Equal(t, http.StatusOK, status, body)
	var answer apigen.PermissionExplanation
	require.NoError(t, json.Unmarshal([]byte(body), &answer))
	return answer
}

// holders asks one page of who holds action on acme itself, as cookie.
func (s *explainStack) holders(t *testing.T, cookie *http.Cookie, action string, limit, offset string) (int, apigen.PermissionHolders, string) {
	t.Helper()
	q := url.Values{"action": {action}}
	if limit != "" {
		q.Set("limit", limit)
	}
	if offset != "" {
		q.Set("offset", offset)
	}
	status, body := s.get(t, "/orgs/acme/permission-holders", q, cookie)
	var answer apigen.PermissionHolders
	if status == http.StatusOK {
		require.NoError(t, json.Unmarshal([]byte(body), &answer))
	}
	return status, answer, body
}

// grns is each holder's GRN, in the order answered.
func grns(holders []apigen.PermissionHolder) []string {
	out := make([]string, 0, len(holders))
	for _, h := range holders {
		out = append(out, h.Grn)
	}
	return out
}

// TestExplainPermission_NamesTheRoleThatAllows explains
// operator:register to the owner, whose org role allows it, and to a
// member, whose role does not: the owner's answer names the org role,
// and the member's is a deny no statement decided. Each decision is the
// one checkPermissions answers the same caller, so the explanation is
// of the decision enforcement reaches.
func TestExplainPermission_NamesTheRoleThatAllows(t *testing.T) {
	s := newExplainStack(t)

	owner := s.explain(t, s.ada, org.ActionOperatorRegister)
	assert.Equal(t, apigen.DecisionAllow, owner.Decision)
	assert.Equal(t, apigen.BasisStatements, owner.Basis)
	require.Len(t, owner.Statements, 1)
	assert.Equal(t, apigen.SourceOrgRole, owner.Statements[0].Source)
	assert.Equal(t, apigen.StatementEffectAllow, owner.Statements[0].Effect)
	require.NotNil(t, owner.Statements[0].Roles)
	assert.Equal(t, []string{string(org.RoleOwner)}, *owner.Statements[0].Roles)

	member := s.explain(t, s.bob, org.ActionOperatorRegister)
	assert.Equal(t, apigen.DecisionDeny, member.Decision)
	assert.Equal(t, apigen.BasisStatements, member.Basis)
	assert.Empty(t, member.Statements, "nothing allows it, and nothing denied it")

	assert.Equal(t, []apigen.PermissionDecision{owner.Decision}, permissionDecisions(t, s.url, s.ada, org.ActionOperatorRegister))
	assert.Equal(t, []apigen.PermissionDecision{member.Decision}, permissionDecisions(t, s.url, s.bob, org.ActionOperatorRegister))
}

// TestExplainPermission_NamesTheDenyAndThePathItReached attaches a Deny
// of operator:register to eng, a group the owner belongs to: the
// owner, who may read the policy, is answered deny by that statement,
// naming the policy and eng as the path. The owner's role still allows
// it and is not named, since the Deny is what decided. The answer
// before the attachment is the control: allow, by the role.
func TestExplainPermission_NamesTheDenyAndThePathItReached(t *testing.T) {
	s := newExplainStack(t)
	require.Equal(t, apigen.DecisionAllow, s.explain(t, s.ada, org.ActionOperatorRegister).Decision)

	denied := activePolicy(t, s.policies, s.acme, "deny-register",
		statementDoc("deny-register", wire.EffectDeny, org.ActionOperatorRegister, s.acme.String()))
	attachPolicy(t, s.policies, denied, s.eng)
	policy, err := grn.Parse(denied)
	require.NoError(t, err)

	got := s.explain(t, s.ada, org.ActionOperatorRegister)
	assert.Equal(t, apigen.DecisionDeny, got.Decision)
	require.Len(t, got.Statements, 1)
	st := got.Statements[0]
	assert.Equal(t, apigen.SourcePolicy, st.Source)
	assert.Equal(t, apigen.StatementEffectDeny, st.Effect)
	assert.Equal(t, ptr("deny-register"), st.Sid)
	assert.Equal(t, ptr(policy.Identifier), st.Policy)
	assert.Equal(t, ptr(apigen.AttachmentGroup), st.Attachment)
	assert.Equal(t, ptr("eng"), st.Group)
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny}, permissionDecisions(t, s.url, s.ada, org.ActionOperatorRegister))
}

// TestExplainPermission_HidesAPolicyTheCallerCannotRead attaches an
// Allow of consent:read, which no member role holds, to a member and
// to the owner. The member is allowed by it and may not read policies,
// so the statement is answered as an unreadable policy and nothing of
// it — sid or policy — appears in the answer. The owner, who may read
// it, is the control: the same statement comes back named, attached to
// the caller, so what hides it from the member is the read check.
func TestExplainPermission_HidesAPolicyTheCallerCannotRead(t *testing.T) {
	s := newExplainStack(t)
	granted := activePolicy(t, s.policies, s.acme, "secret-consent-reader",
		statementDoc("secret-consent-reader", wire.EffectAllow, org.ActionConsentRead, s.acme.String()))
	attachPolicy(t, s.policies, granted, s.bobGRN)
	attachPolicy(t, s.policies, granted, s.adaGRN)
	policy, err := grn.Parse(granted)
	require.NoError(t, err)

	status, body := s.get(t, "/orgs/acme/permission-explanation", url.Values{"action": {org.ActionConsentRead}}, s.bob)
	require.Equal(t, http.StatusOK, status, body)
	var member apigen.PermissionExplanation
	require.NoError(t, json.Unmarshal([]byte(body), &member))
	assert.Equal(t, apigen.DecisionAllow, member.Decision)
	assert.Equal(t, []apigen.ExplainedStatement{{Effect: apigen.StatementEffectAllow, Source: apigen.SourceUnreadablePolicy}}, member.Statements)
	assert.NotContains(t, body, "secret-consent-reader")
	assert.NotContains(t, body, policy.Identifier)

	owner := s.explain(t, s.ada, org.ActionConsentRead)
	var named []apigen.ExplainedStatement
	for _, st := range owner.Statements {
		if st.Source == apigen.SourcePolicy {
			named = append(named, st)
		}
	}
	require.Len(t, named, 1, "the owner reads the policy: %+v", owner.Statements)
	assert.Equal(t, ptr("secret-consent-reader"), named[0].Sid)
	assert.Equal(t, ptr(policy.Identifier), named[0].Policy)
	assert.Equal(t, ptr(apigen.AttachmentCaller), named[0].Attachment)
}

// TestExplainPermission_LeavesOperationDecidedActionsToTheOperation
// explains to the owner the actions checkPermissions puts to more than
// the engine's Check — one a delegation grant decides, agent:enroll
// and operator:delegate, each decided by the check its writes run — and
// one garam does not define. Each carries checkPermissions' decision
// and no statement. operator:register, which Check alone decides, is
// the control: it is explained by statements.
func TestExplainPermission_LeavesOperationDecidedActionsToTheOperation(t *testing.T) {
	s := newExplainStack(t)

	for _, action := range []string{org.ActionAgentCreate, org.ActionAgentEnroll, org.ActionOperatorDelegate} {
		got := s.explain(t, s.ada, action)
		assert.Equal(t, apigen.BasisOperation, got.Basis, action)
		assert.Empty(t, got.Statements, action)
		assert.Equal(t, permissionDecisions(t, s.url, s.ada, action), []apigen.PermissionDecision{got.Decision}, action)
	}
	assert.Equal(t, apigen.DecisionUnknown, s.explain(t, s.ada, org.ActionAgentCreate).Decision)

	undefined := s.explain(t, s.ada, "org:frobnicate")
	assert.Equal(t, apigen.DecisionDeny, undefined.Decision)
	assert.Equal(t, apigen.BasisUndefinedAction, undefined.Basis)

	assert.Equal(t, apigen.BasisStatements, s.explain(t, s.ada, org.ActionOperatorRegister).Basis)
}

// TestExplainPermission_RefusesAHalfNamedResource names a resource type
// without its identifier, which names neither the organization nor a
// resource. The same type with an identifier is the control.
func TestExplainPermission_RefusesAHalfNamedResource(t *testing.T) {
	s := newExplainStack(t)

	status, body := s.get(t, "/orgs/acme/permission-explanation",
		url.Values{"action": {org.ActionGroupUpdate}, "resourceType": {"group"}}, s.ada)
	assert.Equal(t, http.StatusBadRequest, status, body)
	status, body = s.get(t, "/orgs/acme/permission-explanation",
		url.Values{"action": {org.ActionGroupUpdate}, "resourceType": {"group"}, "resourceId": {"eng"}}, s.ada)
	assert.Equal(t, http.StatusOK, status, body)
}

// TestListPermissionHolders_ListsWhomTheEngineAllows asks a member who
// holds consent:read on acme, two candidates a page. The owner holds it
// by role; the agent by an Allow attached to it; bob and carol, whose
// member role does not hold it, are the control on both pages. The
// first page asks ada and bob and points at the second, which asks
// carol and the agent and is the last.
func TestListPermissionHolders_ListsWhomTheEngineAllows(t *testing.T) {
	s := newExplainStack(t)
	granted := activePolicy(t, s.policies, s.acme, "agent-consent",
		statementDoc("agent-consent", wire.EffectAllow, org.ActionConsentRead, s.acme.String()))
	attachPolicy(t, s.policies, granted, s.agent)

	status, first, body := s.holders(t, s.bob, org.ActionConsentRead, "2", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []string{s.adaGRN.String()}, grns(first.Holders))
	assert.Equal(t, apigen.HolderUser, first.Holders[0].Type)
	assert.Equal(t, ptr("ada"), first.Holders[0].Nickname)
	require.NotNil(t, first.NextOffset)
	assert.Equal(t, 2, *first.NextOffset)

	status, second, body := s.holders(t, s.bob, org.ActionConsentRead, "2", "2")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []string{s.agent.String()}, grns(second.Holders))
	assert.Equal(t, apigen.HolderAgent, second.Holders[0].Type)
	assert.Nil(t, second.NextOffset, "the second page is the last")
}

// TestListPermissionHolders_ListsAgentsOnlyToWhoMayReadThem denies
// agent:read to bob: the agent holding consent:read is listed to carol,
// the control, and not to bob, so what leaves it out is bob's read.
func TestListPermissionHolders_ListsAgentsOnlyToWhoMayReadThem(t *testing.T) {
	s := newExplainStack(t)
	granted := activePolicy(t, s.policies, s.acme, "agent-consent",
		statementDoc("agent-consent", wire.EffectAllow, org.ActionConsentRead, s.acme.String()))
	attachPolicy(t, s.policies, granted, s.agent)
	blind := activePolicy(t, s.policies, s.acme, "no-agents",
		statementDoc("no-agents", wire.EffectDeny, org.ActionAgentRead, s.acme.String()))
	attachPolicy(t, s.policies, blind, s.bobGRN)

	status, toCarol, body := s.holders(t, s.carol, org.ActionConsentRead, "", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []string{s.adaGRN.String(), s.agent.String()}, grns(toCarol.Holders))
	status, toBob, body := s.holders(t, s.bob, org.ActionConsentRead, "", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []string{s.adaGRN.String()}, grns(toBob.Holders))
}

// TestListPermissionHolders_Refusals holds each refusal beside the
// request that differs from it in the one thing refused: a caller
// denied org:member-list beside one holding it, an action decided at
// its operation beside one Check decides, and a page past fifty beside
// a page of fifty.
func TestListPermissionHolders_Refusals(t *testing.T) {
	s := newExplainStack(t)
	unlisted := activePolicy(t, s.policies, s.acme, "no-member-list",
		statementDoc("no-member-list", wire.EffectDeny, org.ActionOrgMemberList, s.acme.String()))
	attachPolicy(t, s.policies, unlisted, s.carolGRN)

	status, _, body := s.holders(t, s.carol, org.ActionConsentRead, "", "")
	assert.Equal(t, http.StatusForbidden, status, body)
	status, _, body = s.holders(t, s.bob, org.ActionConsentRead, "", "")
	assert.Equal(t, http.StatusOK, status, body)

	status, _, body = s.holders(t, s.bob, org.ActionAgentCreate, "", "")
	assert.Equal(t, http.StatusBadRequest, status, body)

	status, _, body = s.holders(t, s.bob, org.ActionConsentRead, "51", "")
	assert.Equal(t, http.StatusBadRequest, status, body)
	status, _, body = s.holders(t, s.bob, org.ActionConsentRead, "50", "")
	assert.Equal(t, http.StatusOK, status, body)
}
