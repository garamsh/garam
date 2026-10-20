package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// seedAgent puts an agent of the org in the store. It is seeded rather
// than posted because an operator provisions an agent from outside
// garam (ADR-0012) and this listener creates none.
func seedAgent(t *testing.T, f *fixture, orgSlug string) grn.GRN {
	t.Helper()
	g, err := agent.NewGRN(orgGRNOf(orgSlug))
	require.NoError(t, err)
	a, err := agent.New(g, orgGRNOf(orgSlug))
	require.NoError(t, err)
	require.NoError(t, f.agents.Create(testCtx(t), a))
	return g
}

func orgGRNOf(slug string) grn.GRN {
	return grn.Org(slug)
}

func userGRNOf(subject string) grn.GRN {
	return grn.New("root", "default", user.ServiceType, subject)
}

func TestAgentRead_MemberReadsWhereOthersAreRefused(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")

	// The outsider is permitted the action and belongs to another org;
	// the stranger belongs to this one and is permitted nothing. Each
	// isolates one half of the check, and the member is the case that
	// stops being answered when either half is removed.
	f.permit(
		permission{userGRNOf("member-sub"), org.ActionAgentRead},
		permission{userGRNOf("outsider-sub"), org.ActionAgentRead},
	)
	member := f.sessionCookieFor(t, "member-sub", "Member")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("stranger-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(otherSlug), userGRNOf("outsider-sub"), org.RoleMember)))

	held := seedAgent(t, f, slug)
	operator := grn.New(slug, "default", agent.ServiceTypeOperator, "field-one")
	_, err := f.agents.Assign(testCtx(t), held, operator)
	require.NoError(t, err)
	reassigned, err := f.agents.Assign(testCtx(t), held, operator)
	require.NoError(t, err)
	require.Equal(t, 2, reassigned.Epoch())

	rec := f.do(t, "GET", "/orgs/"+slug+"/agents", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	listed := decodeBody[[]apigen.Agent](t, rec)
	require.Len(t, listed, 1)
	assert.Equal(t, held.String(), listed[0].Grn)
	require.NotNil(t, listed[0].Assignment)
	assert.Equal(t, 2, listed[0].Assignment.Epoch)

	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+held.Identifier, "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeBody[apigen.Agent](t, rec)
	assert.Equal(t, held.String(), got.Grn)
	assert.Equal(t, orgGRNOf(slug).String(), got.OrgGrn)
	// What the operator reports and what the agent reports are separate
	// fields carrying separate values (ADR-0012).
	assert.Equal(t, apigen.AgentProvisioningStatePending, got.ProvisioningState)
	assert.Equal(t, apigen.Unknown, got.RuntimeState)
	require.NotNil(t, got.Assignment)
	assert.Equal(t, operator.String(), got.Assignment.AssigneeGrn)
	assert.NotContains(t, rec.Body.String(), "operatorGrn",
		"the assignee is named once, by assigneeGrn, and by no legacy field (ADR-0117)")
	assert.Equal(t, 2, got.Assignment.Epoch, "the current epoch, not the one it replaced")

	// A member of another org is refused the action it is permitted.
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents", "", outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+held.Identifier, "", outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// Their own org answers, and it holds no agent of this one.
	rec = f.do(t, "GET", "/orgs/"+otherSlug+"/agents", "", outsider)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[[]apigen.Agent](t, rec))

	// A member of this org the evaluator denies is refused too, so
	// membership alone does not admit.
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents", "", stranger)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+held.Identifier, "", stranger)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestAgentRead_SignedOutIsRefused(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("member-sub"), org.ActionAgentRead})
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))
	held := seedAgent(t, f, slug)

	rec := f.do(t, "GET", "/orgs/"+slug+"/agents", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+held.Identifier, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// The same two requests carrying a session are answered, so the
	// refusal is the missing session and not the paths.
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeBody[[]apigen.Agent](t, rec), 1)
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+held.Identifier, "", member)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAgentRead_AssignmentIsAbsentWhileNoOperatorHolds(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	f.permit(permission{userGRNOf("member-sub"), org.ActionAgentRead})
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))

	held := seedAgent(t, f, slug)
	_, err := f.agents.Assign(testCtx(t), held, grn.New(slug, "default", agent.ServiceTypeOperator, "field-one"))
	require.NoError(t, err)
	loose := seedAgent(t, f, slug)
	elsewhere := seedAgent(t, f, otherSlug)

	// The assigned agent carries the field and the unassigned one does
	// not, so its absence is the missing operator and not the endpoint.
	rec := f.do(t, "GET", "/orgs/"+slug+"/agents/"+held.Identifier, "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotNil(t, decodeBody[apigen.Agent](t, rec).Assignment)
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+loose.Identifier, "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, decodeBody[apigen.Agent](t, rec).Assignment)

	rec = f.do(t, "GET", "/orgs/"+slug+"/agents", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	listed := decodeBody[[]apigen.Agent](t, rec)
	require.Len(t, listed, 2, "the org's own agents and no other org's")
	assigned := map[string]bool{}
	for _, a := range listed {
		assigned[a.Grn] = a.Assignment != nil
	}
	// Both GRNs have to be in the listing before their assignment is
	// read: a missing key reports false exactly as a listed agent that
	// carries no operator does.
	require.Contains(t, assigned, held.String())
	require.Contains(t, assigned, loose.String())
	assert.True(t, assigned[held.String()])
	assert.False(t, assigned[loose.String()])

	// An identifier this org does not own is not found, whether it names
	// nothing or another org's agent.
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/deadbeefdeadbeef", "", member)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+elsewhere.Identifier, "", member)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// registerOperator puts a registered operator of the org in the store
// and returns its GRN.
func registerOperator(t *testing.T, f *fixture, orgSlug, id string) grn.GRN {
	t.Helper()
	o, err := operator.New(orgGRNOf(orgSlug), id, userGRNOf("owner-sub"), operator.DetailsPatch{})
	require.NoError(t, err)
	_, err = f.operators.Register(testCtx(t), o)
	require.NoError(t, err)
	return o.GRN
}

// operatorGRNOf names an operator of the org.
func operatorGRNOf(orgSlug, id string) grn.GRN {
	return grn.New(orgSlug, "default", agent.ServiceTypeOperator, id)
}
