package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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
	return grn.New("root", "default", org.ServiceTypeOrg, slug)
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
	assert.Equal(t, apigen.Pending, got.ProvisioningState)
	assert.Equal(t, apigen.Unknown, got.RuntimeState)
	require.NotNil(t, got.Assignment)
	assert.Equal(t, operator.String(), got.Assignment.OperatorGrn)
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
	assert.True(t, assigned[held.String()])
	assert.False(t, assigned[loose.String()])

	// An identifier this org does not own is not found, whether it names
	// nothing or another org's agent.
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/deadbeefdeadbeef", "", member)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+elsewhere.Identifier, "", member)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// operatorGRNOf names an operator of the org. Nothing registers an
// operator, so a definition names one by GRN alone (ADR-0026).
// registerOperator puts a registered operator of the org in the store
// and returns its GRN. A definition names one the org has registered
// (ADR-0044), so every test that defines an agent needs one first.
func registerOperator(t *testing.T, f *fixture, orgSlug, id string) grn.GRN {
	t.Helper()
	o, err := operator.New(orgGRNOf(orgSlug), id, userGRNOf("owner-sub"))
	require.NoError(t, err)
	require.NoError(t, f.operators.Register(testCtx(t), o))
	return o.GRN()
}

func operatorGRNOf(orgSlug, id string) grn.GRN {
	return grn.New(orgSlug, "default", agent.ServiceTypeOperator, id)
}

// defineBody is the request that defines an agent run by operator.
func defineBody(operator grn.GRN, values string) string {
	return `{"operatorGrn":"` + operator.String() + `","values":` + values + `}`
}

// bodyKeys returns the field names a response body carries, sorted. A
// definition and an agent are told apart by the fields they answer
// with, so the field set is the property the tests below assert.
func bodyKeys(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	fields := decodeBody[map[string]json.RawMessage](t, rec)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestAgentDefine_MemberDefinesWhereOthersAreRefused(t *testing.T) {
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
		permission{userGRNOf("member-sub"), org.ActionAgentDefine},
		permission{userGRNOf("member-sub"), org.ActionAgentRead},
		permission{userGRNOf("outsider-sub"), org.ActionAgentDefine},
		permission{userGRNOf("stranger-sub"), org.ActionAgentRead},
	)
	member := f.sessionCookieFor(t, "member-sub", "Member")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("stranger-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(otherSlug), userGRNOf("outsider-sub"), org.RoleMember)))

	operatorGRN := registerOperator(t, f, slug, "field-one")
	otherOperator := registerOperator(t, f, otherSlug, "field-one")
	rec := f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", defineBody(operatorGRN, `{"model":"haiku"}`), member)
	require.Equal(t, http.StatusCreated, rec.Code)
	defined := decodeBody[apigen.AgentDefinition](t, rec)
	assert.Equal(t, operatorGRN.String(), defined.OperatorGrn)
	assert.Equal(t, orgGRNOf(slug).String(), defined.OrgGrn)
	assert.Equal(t, apigen.AgentValues{"model": "haiku"}, defined.Values)
	assert.Equal(t, userGRNOf("member-sub").String(), defined.DefinedBy)
	g, err := grn.Parse(defined.AgentGrn)
	require.NoError(t, err)
	assert.Equal(t, slug, g.Organization, "the org in the path owns the agent it defines")

	// A member of another org is refused the action it is permitted,
	// and their own org answers the same request.
	rec = f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", defineBody(operatorGRN, `{}`), outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "POST", "/orgs/"+otherSlug+"/agent-definitions", defineBody(otherOperator, `{}`), outsider)
	assert.Equal(t, http.StatusCreated, rec.Code)

	// A member of this org the evaluator denies is refused, so
	// membership alone does not admit. Reading is permitted to that
	// member, so what it lacks is the write.
	rec = f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", defineBody(operatorGRN, `{}`), stranger)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/agent-definitions", "", stranger)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Signed out is refused, and the same request carrying a session is
	// answered.
	rec = f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", defineBody(operatorGRN, `{}`))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", defineBody(operatorGRN, `{}`), member)
	assert.Equal(t, http.StatusCreated, rec.Code)

	// The org's definitions are its own, and the member reads back what
	// it defined.
	rec = f.do(t, "GET", "/orgs/"+slug+"/agent-definitions", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	listed := decodeBody[[]apigen.AgentDefinition](t, rec)
	assert.Len(t, listed, 2, "this org's definitions and no other org's")
	assert.True(t, slices.ContainsFunc(listed, func(d apigen.AgentDefinition) bool {
		return d.AgentGrn == defined.AgentGrn
	}))
}

func TestAgentDefine_ADefinitionIsNotAnAgent(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("member-sub"), org.ActionAgentDefine},
		permission{userGRNOf("member-sub"), org.ActionAgentRead},
	)
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))

	// One agent an operator reported, one an organization defined.
	reported := seedAgent(t, f, slug)
	rec := f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", defineBody(registerOperator(t, f, slug, "field-one"), `{"model":"haiku"}`), member)
	require.Equal(t, http.StatusCreated, rec.Code)
	definitionFields := bodyKeys(t, rec)
	defined, err := grn.Parse(decodeBody[apigen.AgentDefinition](t, rec).AgentGrn)
	require.NoError(t, err)

	// Defining an agent puts none among the agents: garam learns an
	// agent exists from the operator that reports it (ADR-0026).
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	agents := decodeBody[[]apigen.Agent](t, rec)
	require.Len(t, agents, 1)
	assert.Equal(t, reported.String(), agents[0].Grn, "the reported agent, not the defined one")

	// The defined agent's own GRN is not addressable there either,
	// while the reported one is — so the 404 is the missing agent and
	// not the path.
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+defined.Identifier, "", member)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+reported.Identifier, "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	agentFields := bodyKeys(t, rec)

	// The definitions answer the one that was defined and not the one
	// that was reported.
	rec = f.do(t, "GET", "/orgs/"+slug+"/agent-definitions", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	definitions := decodeBody[[]apigen.AgentDefinition](t, rec)
	require.Len(t, definitions, 1)
	assert.Equal(t, defined.String(), definitions[0].AgentGrn)

	// The two are answered under different shapes, so a reader holding
	// one body knows which it holds. A definition carries no state,
	// because nobody has reported any.
	assert.Equal(t, []string{"agentGrn", "createdAt", "definedBy", "operatorGrn", "orgGrn", "values"}, definitionFields)
	assert.Equal(t, []string{"createdAt", "grn", "orgGrn", "provisioningState", "runtimeState", "updatedAt"}, agentFields)
}

func TestAgentDefine_RefusesWhatItCannotStore(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("member-sub"), org.ActionAgentDefine})
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))
	registered := registerOperator(t, f, slug, "field-one")

	tests := []struct {
		name string
		body string
	}{
		{
			name: "operator that is not a grn",
			body: `{"operatorGrn":"field-one"}`,
		},
		{
			name: "operator of another organization",
			body: defineBody(operatorGRNOf("globex", "field-one"), `{}`),
		},
		{
			name: "grn that names an agent rather than an operator",
			body: `{"operatorGrn":"` + grn.New(slug, "default", agent.ServiceType, "9f2ac1b40d8e7a35").String() + `"}`,
		},
		{
			name: "value key outside the segment character set",
			body: defineBody(registered, `{"Model_Name":"haiku"}`),
		},
		{
			name: "body that is not an object",
			body: `[]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", tt.body, member)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}

	// The same request naming this org's operator is accepted, so each
	// refusal above is what it names and not the endpoint.
	rec := f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", defineBody(registered, `{"model":"haiku"}`), member)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

// TestAgentDefine_RefusesAnOperatorTheOrgHasNotRegistered is the defect
// #510 exists to close. Before it, a definition could name an operator
// garam held no record of, nothing reported it, and the definition sat
// there for an operator that would never read it.
//
// The refusal names the GRN that named no record, so the person who
// mistyped it is told which string was wrong rather than that something
// was. Registering that same operator is the control: it changes
// nothing else about the request, and the request that was refused is
// then answered — so what refused it was the registry and not the body,
// the caller, or the path.
func TestAgentDefine_RefusesAnOperatorTheOrgHasNotRegistered(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("member-sub"), org.ActionAgentDefine},
		permission{userGRNOf("member-sub"), org.ActionAgentRead},
		permission{userGRNOf("member-sub"), org.ActionOperatorRegister},
	)
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))

	unregistered := operatorGRNOf(slug, "gagent-dev")
	body := defineBody(unregistered, `{"model":"haiku"}`)
	rec := f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", body, member)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	refusal := decodeBody[apigen.Error](t, rec)
	assert.Contains(t, refusal.Message, "registered no operator")
	assert.Contains(t, refusal.Message, unregistered.Identifier,
		"the refusal names the identifier that named no record")
	assert.NotContains(t, refusal.Message, "grn:",
		"the console renders this sentence, and it prints no GRN (ADR-0011)")

	// A refused definition is not a definition: nothing was written on
	// the way to the refusal.
	rec = f.do(t, "GET", "/orgs/"+slug+"/agent-definitions", "", member)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[[]apigen.AgentDefinition](t, rec))

	rec = f.do(t, "POST", "/orgs/"+slug+"/operators", `{"identifier":"gagent-dev"}`, member)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, unregistered.String(), decodeBody[apigen.Operator](t, rec).Grn)

	rec = f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", body, member)
	assert.Equal(t, http.StatusCreated, rec.Code)
}
