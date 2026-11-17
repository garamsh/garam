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

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/cli"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
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
	orgs                     *orgpg.Store
	ada, bob, carol          *http.Cookie
	adaGRN, bobGRN, carolGRN grn.GRN
	decisions                *currentDecisions
}

func newExplainStack(t *testing.T) *explainStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	orgs := orgpg.New(p)
	users := userpg.New(p)
	agents := agentpg.New(p)
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
	decisions := newCurrentDecisions(p)
	s := &explainStack{acme: acme, orgs: orgs, decisions: decisions}
	s.ada, s.adaGRN = session("ada", org.RoleOwner)
	s.bob, s.bobGRN = session("bob", org.RoleMember)
	s.carol, s.carolGRN = session("carol", org.RoleMember)
	s.eng = grn.New("acme", org.ServiceTypeGroup, "eng")
	require.NoError(t, orgs.CreateGroup(ctx, org.NewGroup(s.eng, "eng", acme)))
	require.NoError(t, orgs.AddGroupMember(ctx, s.eng, s.adaGRN))
	require.NoError(t, orgs.AddGroupMember(ctx, s.eng, s.bobGRN))
	s.agent = seedAgent(t, agents, acme)

	srv := api.NewServer(users, orgs, agents, nil, messagepg.New(p), nil,
		authnpg.NewSessionStore(p), nil, nil, nil, nil, decisions, nil, cli.NewAuthorityService(p, decisions), cli.NewStandaloneService(p, decisions),
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

// TestExplainPermission_LeavesOperationDecidedActionsToTheOperation
// explains to the owner the actions checkPermissions puts to more than
// the policy service's Evaluate — one a delegation grant decides,
// iam:agent.enroll and iam:operator.delegate, each decided by the check
// its writes run — and one garam does not define. Each carries
// checkPermissions' decision and no grounds. iam:operator.register,
// which the service alone decides, is the control: it is explained by
// the service. The grounds the service names are held against the real
// service by the policyservice suite.
func TestExplainPermission_LeavesOperationDecidedActionsToTheOperation(t *testing.T) {
	s := newExplainStack(t)

	for _, action := range []string{org.ActionAgentCreate, org.ActionAgentEnroll, org.ActionOperatorDelegate} {
		got := s.explain(t, s.ada, action)
		assert.Equal(t, apigen.BasisOperation, got.Basis, action)
		assert.Empty(t, got.Grounds, action)
		assert.Equal(t, permissionDecisions(t, s.url, s.ada, action), []apigen.PermissionDecision{got.Decision}, action)
	}
	assert.Equal(t, apigen.DecisionUnknown, s.explain(t, s.ada, org.ActionAgentCreate).Decision)

	undefined := s.explain(t, s.ada, "iam:org.frobnicate")
	assert.Equal(t, apigen.DecisionDeny, undefined.Decision)
	assert.Equal(t, apigen.BasisUndefinedAction, undefined.Basis)

	assert.Equal(t, apigen.BasisPolicyService, s.explain(t, s.ada, org.ActionOperatorRegister).Basis)
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

// TestListPermissionHolders_ListsWhomTheServiceAllows asks a member who
// holds iam:consent.read on acme, two candidates a page. The owner
// holds it by role; the agent by a permit of the double; bob and
// carol, whose member role does not hold it, are the control on both
// pages. The first page asks ada and bob and points at the second,
// which asks carol and the agent and is the last.
func TestListPermissionHolders_ListsWhomTheServiceAllows(t *testing.T) {
	s := newExplainStack(t)
	s.decisions.permit(s.agent, org.ActionConsentRead)

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
// iam:agent.read to bob: the agent holding iam:consent.read is listed
// to carol, the control, and not to bob, so what leaves it out is bob's
// read.
func TestListPermissionHolders_ListsAgentsOnlyToWhoMayReadThem(t *testing.T) {
	s := newExplainStack(t)
	s.decisions.permit(s.agent, org.ActionConsentRead)
	s.decisions.forbid(s.bobGRN, org.ActionAgentRead)

	status, toCarol, body := s.holders(t, s.carol, org.ActionConsentRead, "", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []string{s.adaGRN.String(), s.agent.String()}, grns(toCarol.Holders))
	status, toBob, body := s.holders(t, s.bob, org.ActionConsentRead, "", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []string{s.adaGRN.String()}, grns(toBob.Holders))
}

// TestListPermissionHolders_Refusals holds each refusal beside the
// request that differs from it in the one thing refused: a caller
// denied iam:org.member-list beside one holding it, an action decided
// at its operation beside one Check decides, and a page past fifty
// beside a page of fifty.
func TestListPermissionHolders_Refusals(t *testing.T) {
	s := newExplainStack(t)
	s.decisions.forbid(s.carolGRN, org.ActionOrgMemberList)

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
