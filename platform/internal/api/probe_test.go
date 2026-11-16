package api_test

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
	"github.com/garamsh/garam/platform/internal/wire"
)

// TestExplainPermission_AsksWhetherThePolicyIsReadableAsAProbe holds
// that the read of a deciding policy, which only decides how the
// statement is named, is not recorded as an attempt when it is denied.
// The caller's own enforcement of a route is the control, asked as an
// attempt in the same test.
func TestExplainPermission_AsksWhetherThePolicyIsReadableAsAProbe(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	policy := grn.New(slug, "policy", "p-1")
	f.evaluator.EXPECT().Explain(mock.Anything, mock.Anything).
		Return(api.Explanation{Allowed: false, Traces: []api.Trace{
			{Sid: "s", Matched: true, Effect: wire.EffectDeny, Policy: policy},
		}}, nil)
	a := f.record(func(policydecision.Question) bool { return false })

	rec := f.do(t, "GET", "/orgs/"+slug+"/permission-explanation?action="+url.QueryEscape(org.ActionOrgRead), "", owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	reads := a.about(org.ActionPolicyRead)
	require.Len(t, reads, 1)
	assert.True(t, reads[0].Probe, "naming a statement is not acted on")
	assert.Equal(t, policy, reads[0].Resource)

	require.Equal(t, http.StatusForbidden, f.do(t, "GET", "/orgs/"+slug, "", owner).Code)
	enforced := a.about(org.ActionOrgRead)
	require.Len(t, enforced, 1)
	assert.False(t, enforced[0].Probe, "control: the route's own enforcement is an attempt")
}

// TestExplainPermission_AsksAnOperationDecidedActionAsAProbe holds that
// an action an operation decides, which reaches the authority service
// through s.decide, is asked with a probe context through the
// explanation. The authority's own methods mark the probe as well, so
// this holds with or without the explanation's mark; it pins the
// answer's path, and the test above holds the mark itself.
func TestExplainPermission_AsksAnOperationDecidedActionAsAProbe(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	var probes []bool
	f.authority.EXPECT().AuthorizeDelegation(mock.Anything, userGRNOf("owner-sub"), orgGRNOf(slug)).
		RunAndReturn(func(ctx context.Context, _, _ grn.GRN) error {
			probes = append(probes, policydecision.IsProbe(ctx))
			return nil
		}).Once()

	rec := f.do(t, "GET", "/orgs/"+slug+"/permission-explanation?action="+url.QueryEscape(org.ActionOperatorDelegate), "", owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []bool{true}, probes, "the explanation asks it as a probe")
	assert.Equal(t, apigen.DecisionAllow, decodeBody[apigen.PermissionExplanation](t, rec).Decision)
}

// asked records the questions the policy service is put and answers
// each as decide says.
type asked struct {
	mu sync.Mutex
	qs []policydecision.Question
}

func (f *fixture) record(decide func(policydecision.Question) bool) *asked {
	a := &asked{}
	f.answers(func(q policydecision.Question) (bool, error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.qs = append(a.qs, q)
		return decide(q), nil
	})
	return a
}

func (a *asked) about(action string) []policydecision.Question {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []policydecision.Question
	for _, q := range a.qs {
		if q.Action == action {
			out = append(out, q)
		}
	}
	return out
}

// TestCheckPermissions_AsksEachPairAsAProbe holds ADR-0148 item 2 for
// the pairs: a question nobody acts on is not recorded as an attempt
// when it is denied. The handler's own enforcement of the same action
// is the control, asked as an attempt, so the probe is the pair's and
// not a flag every question carries.
func TestCheckPermissions_AsksEachPairAsAProbe(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	eng := grn.New(slug, org.ServiceTypeGroup, "eng")
	require.NoError(t, f.orgs.CreateGroup(testCtx(t), org.NewGroup(eng, "eng", orgGRNOf(slug))))
	a := f.record(func(policydecision.Question) bool { return true })

	pairs := f.do(t, "POST", "/orgs/"+slug+"/permission-checks", checksBody(t,
		apigen.PermissionCheck{Action: org.ActionGroupUpdate,
			Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeGroup, Id: "eng"}},
		apigen.PermissionCheck{Action: org.ActionOrgRead},
	), owner)
	require.Equal(t, http.StatusOK, pairs.Code, pairs.Body.String())
	require.Len(t, a.qs, 2)
	for _, q := range a.qs {
		assert.True(t, q.Probe, "%s on %s", q.Action, q.Resource)
		assert.False(t, q.Current, "%s on %s", q.Action, q.Resource)
	}

	update := f.do(t, "PATCH", "/orgs/"+slug+"/groups/eng", `{"name":"renamed"}`, owner)
	require.Equal(t, http.StatusOK, update.Code, update.Body.String())
	acted := a.about(org.ActionGroupUpdate)
	require.Len(t, acted, 2)
	assert.False(t, acted[1].Probe, "the operation asks as an attempt")
}

// TestListPermissionHolders_AsksTheCheckAsAnAttemptAndTheCandidatesAsProbes
// holds which of the questions the holders page puts are probes: the
// caller's right to list is a decision the handler acts on, and each
// candidate and the agent read that admits agents are questions nobody
// acts on. The first is the control for the rest.
func TestListPermissionHolders_AsksTheCheckAsAnAttemptAndTheCandidatesAsProbes(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.sessionCookieFor(t, "member-sub", "Member")
	memberGRN := userGRNOf("member-sub")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), memberGRN, org.RoleMember)))
	held := seedAgent(t, f, slug)
	ownerGRN := userGRNOf("owner-sub")
	a := f.record(func(q policydecision.Question) bool {
		return q.Subject == ownerGRN || q.Subject == memberGRN && q.Action == org.ActionOrgRead
	})

	rec := f.do(t, "GET", "/orgs/"+slug+"/permission-holders?action="+url.QueryEscape(org.ActionOrgRead), "", owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	lists := a.about(org.ActionOrgMemberList)
	require.Len(t, lists, 1)
	assert.False(t, lists[0].Probe, "the right to list is acted on")
	reads := a.about(org.ActionAgentRead)
	require.Len(t, reads, 1)
	assert.True(t, reads[0].Probe, "the agent read only decides whom to ask about")
	asked := a.about(org.ActionOrgRead)
	require.Len(t, asked, 3, "the owner, the member and the agent")
	for _, q := range asked {
		assert.True(t, q.Probe, "candidate %s", q.Subject)
		assert.Equal(t, orgGRNOf(slug), q.Resource)
	}
	holders := decodeBody[apigen.PermissionHolders](t, rec).Holders
	var got []string
	for _, h := range holders {
		got = append(got, h.Grn)
	}
	assert.ElementsMatch(t, []string{ownerGRN.String(), memberGRN.String()}, got)
	assert.NotContains(t, got, held.String(), "the agent the service denies is not a holder")
}

// TestSearch_AsksEachKindAsAProbe holds that the kinds a search offers
// are decided by probes.
func TestSearch_AsksEachKindAsAProbe(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	a := f.record(func(policydecision.Question) bool { return false })

	rec := f.do(t, "GET", "/orgs/"+slug+"/search?q=eng", "", owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotEmpty(t, a.qs)
	for _, q := range a.qs {
		assert.True(t, q.Probe, q.Action)
	}
}
