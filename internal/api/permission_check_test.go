package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// checksBody is a request asking about the given pairs.
func checksBody(t *testing.T, checks ...apigen.PermissionCheck) string {
	t.Helper()
	raw, err := json.Marshal(apigen.PermissionCheckRequest{Checks: checks})
	require.NoError(t, err)
	return string(raw)
}

// TestCheckPermissions_AnswersEachPairInOrder asks two actions the
// engine decides differently and reads both answers back in the order
// asked, each carrying the pair it answers.
func TestCheckPermissions_AnswersEachPairInOrder(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{grn.New("root", "default", user.ServiceType, "owner-sub"), org.ActionOperatorRead})

	rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks", checksBody(t,
		apigen.PermissionCheck{Action: org.ActionOperatorRegister},
		apigen.PermissionCheck{Action: org.ActionOperatorRead},
	), owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []apigen.PermissionCheckResult{
		{Action: org.ActionOperatorRegister, Decision: apigen.DecisionDeny},
		{Action: org.ActionOperatorRead, Decision: apigen.DecisionAllow},
	}, decodeBody[apigen.PermissionCheckAnswer](t, rec).Results)
}

// TestCheckPermissions_DeniesAnActionGaramDoesNotDefine holds that a
// name no operation checks is answered false even where the engine
// would allow it, as a role's `org:*` would. The engine here allows
// both names; the defined one is the control, answered true, so what
// denies the other is the vocabulary rather than the engine.
func TestCheckPermissions_DeniesAnActionGaramDoesNotDefine(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	subject := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{subject, "org:frobnicate"}, permission{subject, org.ActionOrgUpdate})

	rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks", checksBody(t,
		apigen.PermissionCheck{Action: "org:frobnicate"},
		apigen.PermissionCheck{Action: org.ActionOrgUpdate},
	), owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	results := decodeBody[apigen.PermissionCheckAnswer](t, rec).Results
	require.Len(t, results, 2)
	assert.Equal(t, apigen.DecisionDeny, results[0].Decision, "an undefined action is denied")
	assert.Equal(t, apigen.DecisionAllow, results[1].Decision, "a defined action the engine allows is allowed")
}

// TestCheckPermissions_AsksAboutTheNamedResource holds that a resource
// is checked as the GRN its own operation checks: the engine here
// allows one group and not another, and the same action on each is
// answered apart.
func TestCheckPermissions_AsksAboutTheNamedResource(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	allowedGroup := grn.New(slug, grn.DefaultProject, org.ServiceTypeGroup, "eng")
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in api.EvalInput) (bool, error) {
			return in.Object == allowedGroup, nil
		})

	rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks", checksBody(t,
		apigen.PermissionCheck{Action: org.ActionGroupUpdate,
			Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeGroup, Id: "eng"}},
		apigen.PermissionCheck{Action: org.ActionGroupUpdate,
			Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeGroup, Id: "ops"}},
	), owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	results := decodeBody[apigen.PermissionCheckAnswer](t, rec).Results
	require.Len(t, results, 2)
	assert.Equal(t, apigen.DecisionAllow, results[0].Decision)
	assert.Equal(t, "eng", results[0].Resource.Id, "an answer carries the resource it answers")
	assert.Equal(t, apigen.DecisionDeny, results[1].Decision)
}

// TestCheckPermissions_BoundsTheRequest accepts fifty pairs and
// refuses fifty-one and none. Fifty is the control: the same pairs,
// one fewer, are answered, so the refusal is the bound's.
func TestCheckPermissions_BoundsTheRequest(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit()
	pairs := func(n int) []apigen.PermissionCheck {
		out := make([]apigen.PermissionCheck, n)
		for i := range out {
			out[i] = apigen.PermissionCheck{Action: org.ActionOrgRead}
		}
		return out
	}
	path := "/orgs/" + slug + "/permission-checks"

	rec := f.do(t, "POST", path, checksBody(t, pairs(50)...), owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, decodeBody[apigen.PermissionCheckAnswer](t, rec).Results, 50)

	rec = f.do(t, "POST", path, checksBody(t, pairs(51)...), owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = f.do(t, "POST", path, `{"checks":[]}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestCheckPermissions_RefusesAMalformedResource refuses a resource
// type the operation does not name and an identifier no GRN can hold.
// The well-formed pair is the control.
func TestCheckPermissions_RefusesAMalformedResource(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit()
	path := "/orgs/" + slug + "/permission-checks"

	rec := f.do(t, "POST", path, `{"checks":[{"action":"group:read","resource":{"type":"group","id":"eng"}}]}`, owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	for _, body := range []string{
		`{"checks":[{"action":"group:read","resource":{"type":"team","id":"eng"}}]}`,
		`{"checks":[{"action":"group:read","resource":{"type":"group","id":"*"}}]}`,
	} {
		rec = f.do(t, "POST", path, body, owner)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}

// TestCheckPermissions_RefusesANonMember holds that membership is the
// precondition here as on every operation inside an org. The member
// asking the same question is the control.
func TestCheckPermissions_RefusesANonMember(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	slug := createOrg(t, f, owner, "acme")
	f.permit()
	body := checksBody(t, apigen.PermissionCheck{Action: org.ActionOrgRead})
	path := "/orgs/" + slug + "/permission-checks"

	assert.Equal(t, http.StatusOK, f.do(t, "POST", path, body, owner).Code)
	rec := f.do(t, "POST", path, body, outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, strings.Contains(rec.Body.String(), "results"))
}

// TestCheckPermissions_AnswersUnknownWhereCheckIsNotTheDecision holds
// that an action enforcement decides on more than the engine's Check is
// answered unknown where everything this operation reads allows it:
// one case for a delegation grant, whose mint's check allows here, and
// one for current policy. The engine here allows everything, and the
// plainly checked action is the control.
func TestCheckPermissions_AnswersUnknownWhereCheckIsNotTheDecision(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).Return(true, nil).Maybe()
	f.authority.EXPECT().AuthorizeOperation(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()

	rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks", checksBody(t,
		apigen.PermissionCheck{Action: org.ActionAgentCreate},
		apigen.PermissionCheck{Action: org.ActionOperatorDelegate},
		apigen.PermissionCheck{Action: org.ActionOperatorRegister},
	), owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	results := decodeBody[apigen.PermissionCheckAnswer](t, rec).Results
	require.Len(t, results, 3)
	assert.Equal(t, apigen.DecisionUnknown, results[0].Decision, "a delegation grant decides agent:create")
	assert.Equal(t, apigen.DecisionUnknown, results[1].Decision, "current policy decides operator:delegate")
	assert.Equal(t, apigen.DecisionAllow, results[2].Decision, "Check alone decides operator:register")
}

// TestCheckPermissions_AsksAGrantActionAsItsMintDoes holds that every
// action a delegation grant decides is put to the check its mint runs,
// with the mint's actor, organization, operation and object: the
// organization, or the agent the operation acts on. That check denying
// answers deny, and allowing answers unknown, since the grant is left.
func TestCheckPermissions_AsksAGrantActionAsItsMintDoes(t *testing.T) {
	agentX := &apigen.PermissionResource{Type: apigen.PermissionResourceTypeAgent, Id: "x"}
	tests := []struct {
		action   string
		resource *apigen.PermissionResource
		onAgent  bool
	}{
		{org.ActionAgentCreate, nil, false},
		{org.ActionAgentTemplatePublish, nil, false},
		{org.ActionExecutionProfileRead, nil, false},
		{org.ActionAgentConfigure, agentX, true},
		{org.ActionAgentRecover, agentX, true},
		{org.ActionAgentCutover, agentX, true},
		{org.ActionAgentExecutionRead, agentX, true},
	}
	for _, tc := range tests {
		t.Run(tc.action, func(t *testing.T) {
			f := newFixture(t)
			owner := f.sessionCookieFor(t, "owner-sub", "Owner")
			slug := createOrg(t, f, owner, "acme")
			object := orgGRNOf(slug)
			if tc.onAgent {
				object = grn.New(slug, grn.DefaultProject, agent.ServiceType, "x")
			}
			denied := errorx.New(errorx.KindPermissionDenied, "insufficient role")
			f.authority.EXPECT().AuthorizeOperation(mock.Anything, userGRNOf("owner-sub"), orgGRNOf(slug), object,
				operator.Operation(tc.action)).Return(denied).Once()
			f.authority.EXPECT().AuthorizeOperation(mock.Anything, userGRNOf("owner-sub"), orgGRNOf(slug), object,
				operator.Operation(tc.action)).Return(nil).Once()
			ask := func() apigen.PermissionDecision {
				rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks",
					checksBody(t, apigen.PermissionCheck{Action: tc.action, Resource: tc.resource}), owner)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				return decodeBody[apigen.PermissionCheckAnswer](t, rec).Results[0].Decision
			}

			assert.Equal(t, apigen.DecisionDeny, ask(), "the mint's check denies")
			assert.Equal(t, apigen.DecisionUnknown, ask(), "the mint's check allows, and the grant is left")
		})
	}
}

// TestCheckPermissions_FailsWholeWhenAMintCheckCannotDecide holds that
// a mint's check that fails to decide fails the request, as an engine
// error does, rather than reading as a deny. The same request answered
// is the control.
func TestCheckPermissions_FailsWholeWhenAMintCheckCannotDecide(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.authority.EXPECT().AuthorizeOperation(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(errorx.New(errorx.KindUnavailable, "current authorization")).Once()
	f.authority.EXPECT().AuthorizeOperation(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()
	body := checksBody(t, apigen.PermissionCheck{Action: org.ActionAgentTemplatePublish})
	path := "/orgs/" + slug + "/permission-checks"

	rec := f.do(t, "POST", path, body, owner)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "results", "no pair is answered")

	rec = f.do(t, "POST", path, body, owner)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// TestCheckPermissions_RefusesAPairNoOperationChecks refuses an action
// paired with a kind none of its operations address, naming the pair.
// The same actions on the kinds they are checked on are the control.
func TestCheckPermissions_RefusesAPairNoOperationChecks(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit()
	path := "/orgs/" + slug + "/permission-checks"
	eng := &apigen.PermissionResource{Type: apigen.PermissionResourceTypeGroup, Id: "eng"}

	rec := f.do(t, "POST", path, checksBody(t,
		apigen.PermissionCheck{Action: org.ActionOperatorRegister},
		apigen.PermissionCheck{Action: org.ActionGroupUpdate, Resource: eng},
	), owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	rec = f.do(t, "POST", path, checksBody(t, apigen.PermissionCheck{Action: org.ActionOperatorRegister, Resource: eng}), owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "operator:register is not checked on a resource of type group")

	rec = f.do(t, "POST", path, checksBody(t, apigen.PermissionCheck{Action: org.ActionGroupUpdate}), owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "group:update is not checked on the organization")
}

// TestCheckPermissions_EveryActionPairsWithSomething asks every action
// garam defines on the organization and on each resource kind, and
// holds that at least one of those pairs is answered. An action left
// out of the pairing rules is refused on all of them.
func TestCheckPermissions_EveryActionPairsWithSomething(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit()
	path := "/orgs/" + slug + "/permission-checks"
	resources := []*apigen.PermissionResource{nil}
	for _, kind := range []apigen.PermissionResourceType{
		apigen.PermissionResourceTypeGroup, apigen.PermissionResourceTypeProject,
		apigen.PermissionResourceTypeAgent, apigen.PermissionResourceTypeAgentTemplate,
		apigen.PermissionResourceTypePolicy,
	} {
		resources = append(resources, &apigen.PermissionResource{Type: kind, Id: "x"})
	}
	f.authority.EXPECT().AuthorizeOperation(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Maybe()
	actions := org.Actions()
	require.NotEmpty(t, actions)

	for _, action := range actions {
		answered := 0
		for _, resource := range resources {
			rec := f.do(t, "POST", path, checksBody(t, apigen.PermissionCheck{Action: action, Resource: resource}), owner)
			if rec.Code == http.StatusOK {
				answered++
			}
		}
		assert.Positive(t, answered, "%s pairs with nothing", action)
	}
}

// TestCheckPermissions_FailsWholeWhenTheEngineCannotDecide holds that
// an engine error on one pair fails the request rather than answering
// the pairs around it. The same request with an engine that answers is
// the control.
func TestCheckPermissions_FailsWholeWhenTheEngineCannotDecide(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	failing := true
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in api.EvalInput) (bool, error) {
			if failing && in.Action == org.ActionOrgUpdate {
				return false, errorx.New(errorx.KindUnavailable, "engine unreachable")
			}
			return true, nil
		})
	body := checksBody(t,
		apigen.PermissionCheck{Action: org.ActionOrgRead},
		apigen.PermissionCheck{Action: org.ActionOrgUpdate},
		apigen.PermissionCheck{Action: org.ActionOrgDelete},
	)
	path := "/orgs/" + slug + "/permission-checks"

	rec := f.do(t, "POST", path, body, owner)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "results", "no pair is answered")

	failing = false
	rec = f.do(t, "POST", path, body, owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, decodeBody[apigen.PermissionCheckAnswer](t, rec).Results, 3)
}

// TestCheckPermissions_AMissingResourceAnswersAsADeniedOne holds that
// a group or project that does not exist is answered as one that
// exists and is denied, so the answer tells no one what exists. The
// existing resource beside it is the control for each kind.
func TestCheckPermissions_AMissingResourceAnswersAsADeniedOne(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	orgG := orgGRNForTest(slug)
	require.NoError(t, f.orgs.CreateGroup(testCtx(t),
		org.NewGroup(grn.New(slug, grn.DefaultProject, org.ServiceTypeGroup, "eng"), "Engineering", orgG)))
	require.NoError(t, f.orgs.CreateProject(testCtx(t),
		org.NewProject(grn.New(slug, grn.DefaultProject, org.ServiceTypeProject, "web"), "Web", orgG)))
	f.permit()
	pair := func(action string, kind apigen.PermissionResourceType, id string) apigen.PermissionCheck {
		return apigen.PermissionCheck{Action: action, Resource: &apigen.PermissionResource{Type: kind, Id: id}}
	}

	rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks", checksBody(t,
		pair(org.ActionGroupUpdate, apigen.PermissionResourceTypeGroup, "eng"),
		pair(org.ActionGroupUpdate, apigen.PermissionResourceTypeGroup, "ghost"),
		pair(org.ActionProjectUpdate, apigen.PermissionResourceTypeProject, "web"),
		pair(org.ActionProjectUpdate, apigen.PermissionResourceTypeProject, "ghost"),
	), owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	results := decodeBody[apigen.PermissionCheckAnswer](t, rec).Results
	require.Len(t, results, 4)
	for _, r := range results {
		assert.Equal(t, apigen.DecisionDeny, r.Decision, "%s on %s", r.Action, r.Resource.Id)
	}
}

// TestCheckPermissions_DecidesOperatorReadByCheck pins operator:read as
// an action Check decides, though a console may also hand a hosted
// operator an authority for it (#1374): garam's own operator reads are
// enforced by Check alone, as agent-template:read's are, and a mint
// the grant does not cover is refused at the mint. The engine allowing
// the read and then refusing it are both answered as decided.
func TestCheckPermissions_DecidesOperatorReadByCheck(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	allowed := true
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(context.Context, api.EvalInput) (bool, error) { return allowed, nil })
	ask := func() apigen.PermissionDecision {
		rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks",
			checksBody(t, apigen.PermissionCheck{Action: org.ActionOperatorRead}), owner)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return decodeBody[apigen.PermissionCheckAnswer](t, rec).Results[0].Decision
	}

	assert.Equal(t, apigen.DecisionAllow, ask())
	allowed = false
	assert.Equal(t, apigen.DecisionDeny, ask())
}
