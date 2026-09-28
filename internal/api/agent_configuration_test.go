package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// reviseBody is the request saving values against a revision.
func reviseBody(expected int, values string) string {
	return fmt.Sprintf(`{"expectedRevision":%d,"values":%s}`, expected, values)
}

// valuesPath is the route that revises the values of the agent g
// names, under the org in the path.
func valuesPath(orgSlug string, g grn.GRN) string {
	return "/orgs/" + orgSlug + "/agent-definitions/" + g.Identifier + "/values"
}

// defineIn stores a definition of an agent of the org, run by one of
// its operators, and returns the agent it names.
func defineIn(t *testing.T, f *fixture, orgSlug string, values agent.Values) grn.GRN {
	t.Helper()
	d, err := agent.NewDefinition(orgGRNOf(orgSlug), operatorGRNOf(orgSlug, "field-one"), "", values, userGRNOf("owner-sub"))
	require.NoError(t, err)
	require.NoError(t, f.agents.Define(testCtx(t), d))
	return d.Agent()
}

// TestAgentReviseValues_ConfigureIsWhatAdmits isolates each half of the
// check the way the define test does: the outsider is permitted the
// action and belongs to another org, the stranger belongs to this one
// and may only read. The member is the control that stops being
// answered when either half is removed.
func TestAgentReviseValues_ConfigureIsWhatAdmits(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	otherOwner := f.sessionCookieFor(t, "other-owner-sub", "Other Owner")
	otherSlug := createOrg(t, f, otherOwner, "globex")
	f.permit(
		permission{userGRNOf("member-sub"), org.ActionAgentConfigure},
		permission{userGRNOf("outsider-sub"), org.ActionAgentConfigure},
		permission{userGRNOf("stranger-sub"), org.ActionAgentRead},
		permission{userGRNOf("stranger-sub"), org.ActionAgentDefine},
	)
	member := f.sessionCookieFor(t, "member-sub", "Member")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("stranger-sub"), org.RoleMember)))
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(otherSlug), userGRNOf("outsider-sub"), org.RoleMember)))
	defined := defineIn(t, f, slug, agent.Values{"model": "haiku"})
	theirs := defineIn(t, f, otherSlug, agent.Values{})

	rec := f.do(t, "PUT", valuesPath(slug, defined), reviseBody(1, `{"model":"sonnet"}`), outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a member of another org")
	rec = f.do(t, "PUT", valuesPath(slug, defined), reviseBody(1, `{"model":"sonnet"}`), stranger)
	assert.Equal(t, http.StatusForbidden, rec.Code, "a member who may define but not configure")
	rec = f.do(t, "PUT", valuesPath(slug, defined), reviseBody(1, `{"model":"sonnet"}`))
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "signed out")
	assert.Nil(t, f.agents.revised[defined.String()], "a refused save was stored")

	rec = f.do(t, "PUT", valuesPath(slug, defined), reviseBody(1, `{"model":"sonnet"}`), member)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, agent.Values{"model": "sonnet"}, f.agents.revised[defined.String()])

	// Another org's agent is not addressable through this org's path:
	// the org segment scopes the identifier, so it names no definition
	// here. The outsider's own org answers the same identifier.
	rec = f.do(t, "PUT", valuesPath(slug, theirs), reviseBody(1, `{}`), member)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	rec = f.do(t, "PUT", valuesPath(otherSlug, theirs), reviseBody(1, `{}`), outsider)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestAgentReviseValues_AStaleRevisionIsRefusedAndTheSavedOneStands is
// the lost update ADR-0076 closes. Two editors read revision 1; the
// first saves, and the second, still holding 1, is refused under
// `aborted` rather than writing over the first. The control is the
// same request carrying the revision the first save produced.
func TestAgentReviseValues_AStaleRevisionIsRefusedAndTheSavedOneStands(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionAgentConfigure})
	defined := defineIn(t, f, slug, agent.Values{"model": "haiku"})

	rec := f.do(t, "PUT", valuesPath(slug, defined), reviseBody(1, `{"model":"sonnet"}`), owner)
	require.Equal(t, http.StatusOK, rec.Code)
	first := decodeBody[apigen.AgentDefinition](t, rec)
	assert.Equal(t, 2, first.Revision)
	assert.Equal(t, userGRNOf("owner-sub").String(), first.RevisedBy)

	rec = f.do(t, "PUT", valuesPath(slug, defined), reviseBody(1, `{"model":"opus"}`), owner)
	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "aborted", decodeBody[apigen.Error](t, rec).Kind)
	assert.Equal(t, agent.Values{"model": "sonnet"}, f.agents.revised[defined.String()],
		"the stale save wrote over the first")

	rec = f.do(t, "PUT", valuesPath(slug, defined), reviseBody(2, `{"model":"opus"}`), owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 3, decodeBody[apigen.AgentDefinition](t, rec).Revision)
	assert.Equal(t, agent.Values{"model": "opus"}, f.agents.revised[defined.String()])
}

// TestAgentReviseValues_OnlyTheValuesMove reads back what a save
// answers with beside what the definition said before it: the
// identity, the operator and who defined the agent are unchanged, and
// only the revision and who saved it move.
func TestAgentReviseValues_OnlyTheValuesMove(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("member-sub"), org.ActionAgentConfigure})
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("member-sub"), org.RoleMember)))
	defined := defineIn(t, f, slug, agent.Values{"model": "haiku"})
	before := f.agents.definitions[defined.String()]
	createdAt := before.CreatedAt()

	rec := f.do(t, "PUT", valuesPath(slug, defined), reviseBody(1, `{"model":"sonnet"}`), member)
	require.Equal(t, http.StatusOK, rec.Code)
	saved := decodeBody[apigen.AgentDefinition](t, rec)
	assert.Equal(t, defined.String(), saved.AgentGrn)
	assert.Equal(t, operatorGRNOf(slug, "field-one").String(), saved.OperatorGrn)
	assert.Equal(t, userGRNOf("owner-sub").String(), saved.DefinedBy, "who defined the agent moved")
	assert.True(t, createdAt.Equal(saved.CreatedAt))
	assert.Equal(t, 2, saved.Revision)
	assert.Equal(t, userGRNOf("member-sub").String(), saved.RevisedBy)
}

// TestAgentReviseValues_RefusesWhatItCannotStore sets every refused
// body beside the accepted one that differs from it in the refused
// part alone, and reads the definition after each refusal: a refused
// save moves no revision and stores no values. An absent or null
// `values` would otherwise decode as no settings and clear them, so
// `{}` — clearing them on purpose — is the accepted control for both.
func TestAgentReviseValues_RefusesWhatItCannotStore(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionAgentConfigure})

	cases := map[string]struct {
		refused, accepted string
		saved             agent.Values
	}{
		"a revision below 1": {
			reviseBody(0, `{}`), reviseBody(1, `{}`), agent.Values{}},
		"a key outside the character set": {
			reviseBody(1, `{"Model_Name":"haiku"}`), reviseBody(1, `{"model-name":"haiku"}`), agent.Values{"model-name": "haiku"}},
		"a body that is not an object": {
			`[]`, reviseBody(1, `{}`), agent.Values{}},
		"no values at all": {
			`{"expectedRevision":1}`, reviseBody(1, `{}`), agent.Values{}},
		"values that are null": {
			reviseBody(1, `null`), reviseBody(1, `{}`), agent.Values{}},
		"a value that is null": {
			reviseBody(1, `{"model":null}`), reviseBody(1, `{"model":""}`), agent.Values{"model": ""}},
		"a value that is not a string": {
			reviseBody(1, `{"model":1}`), reviseBody(1, `{"model":"1"}`), agent.Values{"model": "1"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			defined := defineIn(t, f, slug, agent.Values{"model": "haiku"})
			stored := f.agents.definitions[defined.String()]

			rec := f.do(t, "PUT", valuesPath(slug, defined), c.refused, owner)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, agent.Revision(1), stored.Revision, "a refused save moved the revision")
			assert.Equal(t, userGRNOf("owner-sub"), stored.RevisedBy)
			assert.NotContains(t, f.agents.revised, defined.String(), "a refused save stored values")

			rec = f.do(t, "PUT", valuesPath(slug, defined), c.accepted, owner)
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, c.saved, f.agents.revised[defined.String()])
		})
	}
}

// TestAgentRead_ConfigurationAndHealthAreSeparateFacts renders what
// an agent carries: an acknowledgement with the epoch it was made at
// and its own time, a health report time beside it, and an agent
// carrying neither answers without both rather than with invented
// ones.
func TestAgentRead_ConfigurationAndHealthAreSeparateFacts(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{userGRNOf("owner-sub"), org.ActionAgentRead})
	silent := seedAgent(t, f, slug)
	reported := seedAgent(t, f, slug)
	health := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	acknowledged := health.Add(time.Minute)
	applied, err := agent.NewConfigurationApplied([]string{"workspace"})
	require.NoError(t, err)
	stored := f.agents.agents[reported.String()]
	stored.HealthReportedAt = &health
	stored.Configuration = &agent.ConfigurationReport{Epoch: 2, Revision: 3, Outcome: applied, ReportedAt: acknowledged}

	rec := f.do(t, "GET", "/orgs/"+slug+"/agents/"+silent.Identifier, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, bodyKeys(t, rec), "configuration")
	assert.NotContains(t, bodyKeys(t, rec), "healthReportedAt")

	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+reported.Identifier, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	got := decodeBody[apigen.Agent](t, rec)
	require.NotNil(t, got.HealthReportedAt)
	assert.True(t, health.Equal(*got.HealthReportedAt))
	require.NotNil(t, got.Configuration)
	assert.Equal(t, 2, got.Configuration.Epoch)
	assert.Equal(t, 3, got.Configuration.Revision)
	assert.Equal(t, apigen.Applied, got.Configuration.Outcome)
	assert.Equal(t, &[]string{"workspace"}, got.Configuration.IgnoredKeys)
	assert.Nil(t, got.Configuration.Reason)
	assert.True(t, acknowledged.Equal(got.Configuration.ReportedAt))

	reason, err := agent.ParseRejectionReason("image pull failed")
	require.NoError(t, err)
	stored.Configuration = &agent.ConfigurationReport{Epoch: 2, Revision: 3, Outcome: agent.ConfigurationRejected{Reason: reason}, ReportedAt: acknowledged}
	rec = f.do(t, "GET", "/orgs/"+slug+"/agents/"+reported.Identifier, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	got = decodeBody[apigen.Agent](t, rec)
	require.NotNil(t, got.Configuration)
	assert.Equal(t, apigen.Rejected, got.Configuration.Outcome)
	assert.Equal(t, "image pull failed", *got.Configuration.Reason)
	assert.Nil(t, got.Configuration.IgnoredKeys)
}
