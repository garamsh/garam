package api_test

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// retiredTemplate is a template of the organization slug names, as one
// kept before templates were retired read-only (ADR-0088).
func retiredTemplate(slug, id, name string) *agent.Template {
	at := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	return &agent.Template{
		GRN:    grn.New(slug, grn.DefaultProject, agent.ServiceTypeTemplate, id),
		OrgGRN: orgGRNOf(slug),
		TemplateContent: agent.TemplateContent{
			Name: agent.Name(name), Description: "Reads papers", Values: agent.Values{"model": "haiku"},
		},
		CreatedBy: userGRNOf("owner-sub"),
		CreatedAt: at,
		UpdatedAt: at,
	}
}

// TestAgentTemplate_WritesAreClosedAndReadsStay holds the retirement at
// the routes (ADR-0088). An owner whom the evaluator allows every
// action sends the create, the replacement and the delete the
// specification served before, and each is answered 405 without
// reaching the store: the template reads back unchanged. The control
// is the same owner reading the same paths, which are answered.
func TestAgentTemplate_WritesAreClosedAndReadsStay(t *testing.T) {
	f := newFixture(t)
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).Return(true, nil)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	kept := retiredTemplate(slug, "7c1e2a9b3d4f5a60", "Researcher")
	f.agents.keepTemplate(kept)
	base := "/orgs/" + slug + "/agent-templates"
	one := base + "/" + kept.GRN.Identifier
	body := `{"name":"Analyst","description":"","values":{}}`

	for _, write := range []struct{ method, path string }{
		{"POST", base}, {"PUT", one}, {"DELETE", one},
	} {
		rec := f.do(t, write.method, write.path, body, owner)
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, write.method+" "+write.path)
	}

	rec := f.do(t, "GET", one, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	read := decodeBody[apigen.AgentTemplate](t, rec)
	assert.Equal(t, kept.GRN.String(), read.Grn)
	assert.Equal(t, "Researcher", read.Name, "no write reached the template")
	assert.Equal(t, apigen.AgentValues{"model": "haiku"}, read.Values)
	rec = f.do(t, "GET", base, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeBody[[]apigen.AgentTemplate](t, rec), 1)
}

// TestAgentTemplate_ReadingIsItsOwnAction covers the one template
// action left (ADR-0075, ADR-0088). The reader holds it and reads both
// paths; defining agents does not grant it, so the definer's refusals
// of the same requests are what the action decides.
func TestAgentTemplate_ReadingIsItsOwnAction(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("reader-sub"), org.ActionAgentTemplateRead},
		permission{userGRNOf("definer-sub"), org.ActionAgentDefine},
		permission{userGRNOf("definer-sub"), org.ActionAgentRead},
	)
	reader := f.sessionCookieFor(t, "reader-sub", "Reader")
	definer := f.sessionCookieFor(t, "definer-sub", "Definer")
	for _, sub := range []string{"reader-sub", "definer-sub"} {
		require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf(sub), org.RoleMember)))
	}
	kept := retiredTemplate(slug, "7c1e2a9b3d4f5a60", "Researcher")
	f.agents.keepTemplate(kept)
	base := "/orgs/" + slug + "/agent-templates"

	for _, path := range []string{base, base + "/" + kept.GRN.Identifier} {
		assert.Equal(t, http.StatusForbidden, f.do(t, "GET", path, "", definer).Code, path)
		assert.Equal(t, http.StatusOK, f.do(t, "GET", path, "", reader).Code, path)
	}
}

// TestAgentTemplate_IsAuthorizedOnTheTemplateItNames records what each
// template read asks the evaluator about. A read that asked about the
// organization instead would let a policy scoped to one template reach
// every other one; the list, which names no template, asks about the
// organization and is the control.
func TestAgentTemplate_IsAuthorizedOnTheTemplateItNames(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	type question struct {
		action string
		object grn.GRN
	}
	var mu sync.Mutex
	var asked []question
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in api.EvalInput) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			asked = append(asked, question{in.Action, in.Object})
			return in.Subject == userGRNOf("maintainer-sub"), nil
		})
	maintainer := f.sessionCookieFor(t, "maintainer-sub", "Maintainer")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf("maintainer-sub"), org.RoleMember)))
	kept := retiredTemplate(slug, "7c1e2a9b3d4f5a60", "Researcher")
	f.agents.keepTemplate(kept)
	base := "/orgs/" + slug + "/agent-templates"

	require.Equal(t, http.StatusOK, f.do(t, "GET", base+"/"+kept.GRN.Identifier, "", maintainer).Code)
	require.Equal(t, http.StatusOK, f.do(t, "GET", base, "", maintainer).Code)

	mu.Lock()
	defer mu.Unlock()
	// Which object each action was asked about is the property; the
	// order the evaluator was asked in is not.
	assert.ElementsMatch(t, []question{
		{org.ActionAgentTemplateRead, kept.GRN},
		{org.ActionAgentTemplateRead, orgGRNOf(slug)},
	}, asked)
}

// TestAgentTemplate_StaysInsideItsOrganization reads one
// organization's template through another's paths. The identifier is
// the same; only the organization segment differs, so a store keyed on
// the identifier alone would answer. The owning organization answering
// the same read is the control.
func TestAgentTemplate_StaysInsideItsOrganization(t *testing.T) {
	f := newFixture(t)
	f.permit(permission{userGRNOf("owner-sub"), org.ActionAgentTemplateRead})
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	acme := createOrg(t, f, owner, "acme")
	globex := createOrg(t, f, owner, "globex")
	theirs := retiredTemplate(globex, "7c1e2a9b3d4f5a60", "Theirs")
	f.agents.keepTemplate(theirs)

	elsewhere := "/orgs/" + acme + "/agent-templates/" + theirs.GRN.Identifier
	assert.Equal(t, http.StatusNotFound, f.do(t, "GET", elsewhere, "", owner).Code)
	rec := f.do(t, "GET", "/orgs/"+acme+"/agent-templates", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[[]apigen.AgentTemplate](t, rec))

	rec = f.do(t, "GET", "/orgs/"+globex+"/agent-templates/"+theirs.GRN.Identifier, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Theirs", decodeBody[apigen.AgentTemplate](t, rec).Name)
}

// TestAgentDefinition_RenameIsItsOwnActionOnTheAgent covers renaming a
// defined agent (ADR-0075): it is authorized on `agent:rename` against
// the agent's GRN, and defining agents does not grant it. A member of
// another organization is refused on this organization's path, and a
// definition this organization never wrote is not found.
func TestAgentDefinition_RenameIsItsOwnActionOnTheAgent(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	var mu sync.Mutex
	var renameObjects []grn.GRN
	allowed := []permission{
		{userGRNOf("renamer-sub"), org.ActionAgentRename},
		{userGRNOf("renamer-sub"), org.ActionAgentRead},
		{userGRNOf("definer-sub"), org.ActionAgentDefine},
		{userGRNOf("definer-sub"), org.ActionAgentRead},
	}
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in api.EvalInput) (bool, error) {
			mu.Lock()
			defer mu.Unlock()
			if in.Action == org.ActionAgentRename {
				renameObjects = append(renameObjects, in.Object)
			}
			return slices.Contains(allowed, permission{in.Subject, in.Action}), nil
		})
	renamer := f.sessionCookieFor(t, "renamer-sub", "Renamer")
	definer := f.sessionCookieFor(t, "definer-sub", "Definer")
	for _, sub := range []string{"renamer-sub", "definer-sub"} {
		require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf(sub), org.RoleMember)))
	}

	rec := f.do(t, "POST", "/orgs/"+slug+"/agent-definitions", defineBody(registerOperator(t, f, slug, "field-one"), `{"model":"haiku"}`), definer)
	require.Equal(t, http.StatusCreated, rec.Code)
	defined := decodeBody[apigen.AgentDefinition](t, rec)
	assert.Equal(t, "Field agent", defined.Name)
	agentGRN, err := grn.Parse(defined.AgentGrn)
	require.NoError(t, err)
	path := "/orgs/" + slug + "/agent-definitions/" + agentGRN.Identifier

	rec = f.do(t, "GET", path, "", definer)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, defined, decodeBody[apigen.AgentDefinition](t, rec))

	assert.Equal(t, http.StatusForbidden, f.do(t, "PATCH", path, `{"name":"Scout"}`, definer).Code)
	assert.Equal(t, http.StatusBadRequest, f.do(t, "PATCH", path, `{"name":"  "}`, renamer).Code)
	rec = f.do(t, "PATCH", path, `{"name":" Scout "}`, renamer)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Scout", decodeBody[apigen.AgentDefinition](t, rec).Name, "trimmed")
	rec = f.do(t, "GET", path, "", renamer)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Scout", decodeBody[apigen.AgentDefinition](t, rec).Name)
	assert.Equal(t, http.StatusNotFound,
		f.do(t, "PATCH", "/orgs/"+slug+"/agent-definitions/0000000000000000", `{"name":"Scout"}`, renamer).Code)

	f.agents.mu.Lock()
	defer f.agents.mu.Unlock()
	assert.Equal(t, []rename{{agent: agentGRN, name: "Scout"}}, f.agents.renames,
		"one rename, trimmed, of the agent the path names")
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, renameObjects, agentGRN, "authorized on the agent, not the organization")
	assert.NotContains(t, renameObjects, orgGRNOf(slug))
}
