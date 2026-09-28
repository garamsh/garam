package api_test

import (
	"context"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// templateBody is a create or update request.
func templateBody(name, description, values string) string {
	return `{"name":"` + name + `","description":"` + description + `","values":` + values + `}`
}

// TestAgentTemplate_EachOperationIsItsOwnAction covers the four
// template actions (ADR-0075). The maintainer holds all four and the
// reader holds only the read: the reader's refused writes and the
// maintainer's answered ones are the same requests, so what refuses
// the reader is the action and not the request. Defining agents grants
// none of them, which is what lets a policy tell the two apart.
func TestAgentTemplate_EachOperationIsItsOwnAction(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(
		permission{userGRNOf("maintainer-sub"), org.ActionAgentTemplateRead},
		permission{userGRNOf("maintainer-sub"), org.ActionAgentTemplateCreate},
		permission{userGRNOf("maintainer-sub"), org.ActionAgentTemplateUpdate},
		permission{userGRNOf("maintainer-sub"), org.ActionAgentTemplateDelete},
		permission{userGRNOf("reader-sub"), org.ActionAgentTemplateRead},
		permission{userGRNOf("definer-sub"), org.ActionAgentDefine},
		permission{userGRNOf("definer-sub"), org.ActionAgentRead},
	)
	maintainer := f.sessionCookieFor(t, "maintainer-sub", "Maintainer")
	reader := f.sessionCookieFor(t, "reader-sub", "Reader")
	definer := f.sessionCookieFor(t, "definer-sub", "Definer")
	for _, sub := range []string{"maintainer-sub", "reader-sub", "definer-sub"} {
		require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), userGRNOf(sub), org.RoleMember)))
	}
	base := "/orgs/" + slug + "/agent-templates"

	create := templateBody(" Researcher ", "Reads papers", `{"model":"haiku"}`)
	for _, who := range []*http.Cookie{reader, definer} {
		assert.Equal(t, http.StatusForbidden, f.do(t, "POST", base, create, who).Code)
	}
	rec := f.do(t, "POST", base, create, maintainer)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.AgentTemplate](t, rec)
	assert.Equal(t, "Researcher", created.Name, "trimmed")
	assert.Equal(t, "Reads papers", created.Description)
	assert.Equal(t, apigen.AgentValues{"model": "haiku"}, created.Values)
	assert.Equal(t, userGRNOf("maintainer-sub").String(), created.CreatedBy)
	g, err := grn.Parse(created.Grn)
	require.NoError(t, err)
	assert.Equal(t, slug, g.Organization)
	assert.Equal(t, agent.ServiceTypeTemplate, g.ServiceType)
	one := base + "/" + g.Identifier

	for _, path := range []string{base, one} {
		assert.Equal(t, http.StatusForbidden, f.do(t, "GET", path, "", definer).Code, path)
		assert.Equal(t, http.StatusOK, f.do(t, "GET", path, "", reader).Code, path)
	}

	update := templateBody("Analyst", "", `{}`)
	assert.Equal(t, http.StatusForbidden, f.do(t, "PUT", one, update, reader).Code)
	rec = f.do(t, "PUT", one, update, maintainer)
	require.Equal(t, http.StatusOK, rec.Code)
	updated := decodeBody[apigen.AgentTemplate](t, rec)
	assert.Equal(t, "Analyst", updated.Name)
	assert.Empty(t, updated.Description, "an update replaces the whole template")
	assert.Empty(t, updated.Values)

	assert.Equal(t, http.StatusForbidden, f.do(t, "DELETE", one, "", reader).Code)
	assert.Equal(t, http.StatusNoContent, f.do(t, "DELETE", one, "", maintainer).Code)
	assert.Equal(t, http.StatusNotFound, f.do(t, "GET", one, "", reader).Code)
	assert.Equal(t, http.StatusNotFound, f.do(t, "DELETE", one, "", maintainer).Code)
}

// TestAgentTemplate_IsAuthorizedOnTheTemplateItNames records what each
// template route asks the evaluator about. A route that asked about the
// organization instead would let a policy scoped to one template reach
// every other one; the collection routes, which name no template, ask
// about the organization and are the control.
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
	base := "/orgs/" + slug + "/agent-templates"

	rec := f.do(t, "POST", base, templateBody("Researcher", "", `{}`), maintainer)
	require.Equal(t, http.StatusCreated, rec.Code)
	g, err := grn.Parse(decodeBody[apigen.AgentTemplate](t, rec).Grn)
	require.NoError(t, err)
	one := base + "/" + g.Identifier
	require.Equal(t, http.StatusOK, f.do(t, "GET", one, "", maintainer).Code)
	require.Equal(t, http.StatusOK, f.do(t, "PUT", one, templateBody("Analyst", "", `{}`), maintainer).Code)
	require.Equal(t, http.StatusNoContent, f.do(t, "DELETE", one, "", maintainer).Code)
	require.Equal(t, http.StatusOK, f.do(t, "GET", base, "", maintainer).Code)

	mu.Lock()
	defer mu.Unlock()
	// Which object each action was asked about is the property; the
	// order the evaluator was asked in is not.
	assert.ElementsMatch(t, []question{
		{org.ActionAgentTemplateCreate, orgGRNOf(slug)},
		{org.ActionAgentTemplateRead, g},
		{org.ActionAgentTemplateUpdate, g},
		{org.ActionAgentTemplateDelete, g},
		{org.ActionAgentTemplateRead, orgGRNOf(slug)},
	}, asked)
}

// TestAgentTemplate_StaysInsideItsOrganization reads, updates and
// deletes one organization's template through another's paths. The
// identifier is the same; only the organization segment differs, so a
// store keyed on the identifier alone would answer. The owner's own
// organization answering the same requests is the control.
func TestAgentTemplate_StaysInsideItsOrganization(t *testing.T) {
	f := newFixture(t)
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionAgentTemplateRead},
		permission{userGRNOf("owner-sub"), org.ActionAgentTemplateCreate},
		permission{userGRNOf("owner-sub"), org.ActionAgentTemplateUpdate},
		permission{userGRNOf("owner-sub"), org.ActionAgentTemplateDelete},
	)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	acme := createOrg(t, f, owner, "acme")
	globex := createOrg(t, f, owner, "globex")

	rec := f.do(t, "POST", "/orgs/"+globex+"/agent-templates", templateBody("Theirs", "", `{}`), owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	g, err := grn.Parse(decodeBody[apigen.AgentTemplate](t, rec).Grn)
	require.NoError(t, err)

	elsewhere := "/orgs/" + acme + "/agent-templates/" + g.Identifier
	assert.Equal(t, http.StatusNotFound, f.do(t, "GET", elsewhere, "", owner).Code)
	assert.Equal(t, http.StatusNotFound, f.do(t, "PUT", elsewhere, templateBody("Mine", "", `{}`), owner).Code)
	assert.Equal(t, http.StatusNotFound, f.do(t, "DELETE", elsewhere, "", owner).Code)
	rec = f.do(t, "GET", "/orgs/"+acme+"/agent-templates", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, decodeBody[[]apigen.AgentTemplate](t, rec))

	home := "/orgs/" + globex + "/agent-templates/" + g.Identifier
	rec = f.do(t, "GET", home, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Theirs", decodeBody[apigen.AgentTemplate](t, rec).Name)
}

func TestAgentTemplate_RefusesWhatItCannotStore(t *testing.T) {
	f := newFixture(t)
	f.permit(permission{userGRNOf("owner-sub"), org.ActionAgentTemplateCreate})
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	base := "/orgs/" + slug + "/agent-templates"

	for name, body := range map[string]string{
		"no name":             `{"values":{}}`,
		"name of whitespace":  templateBody("   ", "", `{}`),
		"value key refused":   templateBody("Researcher", "", `{"Model":"haiku"}`),
		"body not an object":  `[]`,
		"values not a string": templateBody("Researcher", "", `{"model":1}`),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusBadRequest, f.do(t, "POST", base, body, owner).Code)
		})
	}
	// The same request with a name and a key garam accepts is stored.
	assert.Equal(t, http.StatusCreated, f.do(t, "POST", base, templateBody("Researcher", "", `{"model":"haiku"}`), owner).Code)
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

// TestAgentTemplate_RefusesNullValuesAndKeepsOmissionClearing holds the
// write boundary of a template's values. An explicit null, for the
// object or for one entry, is refused rather than read as empty, and a
// refused update leaves the stored template as it was, timestamps
// included. The controls are the writes the specification admits:
// values left out (which clears them, since an update replaces the
// whole template), an empty object, and an empty string.
func TestAgentTemplate_RefusesNullValuesAndKeepsOmissionClearing(t *testing.T) {
	f := newFixture(t)
	f.permit(
		permission{userGRNOf("owner-sub"), org.ActionAgentTemplateRead},
		permission{userGRNOf("owner-sub"), org.ActionAgentTemplateCreate},
		permission{userGRNOf("owner-sub"), org.ActionAgentTemplateUpdate},
	)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	base := "/orgs/" + slug + "/agent-templates"

	for name, body := range map[string]string{
		"null values object": `{"name":"Researcher","values":null}`,
		"null value entry":   `{"name":"Researcher","values":{"workspace":null}}`,
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusBadRequest, f.do(t, "POST", base, body, owner).Code)
		})
	}
	for name, tc := range map[string]struct {
		body string
		want apigen.AgentValues
	}{
		"values left out":   {`{"name":"Researcher"}`, apigen.AgentValues{}},
		"empty object":      {`{"name":"Researcher","values":{}}`, apigen.AgentValues{}},
		"empty string kept": {`{"name":"Researcher","values":{"workspace":""}}`, apigen.AgentValues{"workspace": ""}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(t, "POST", base, tc.body, owner)
			require.Equal(t, http.StatusCreated, rec.Code)
			assert.Equal(t, tc.want, decodeBody[apigen.AgentTemplate](t, rec).Values)
		})
	}

	rec := f.do(t, "POST", base, templateBody("Researcher", "", `{"workspace":"papers"}`), owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.AgentTemplate](t, rec)
	g, err := grn.Parse(created.Grn)
	require.NoError(t, err)
	one := base + "/" + g.Identifier

	assert.Equal(t, http.StatusBadRequest,
		f.do(t, "PUT", one, `{"name":"Researcher","values":{"workspace":null}}`, owner).Code)
	rec = f.do(t, "GET", one, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, created, decodeBody[apigen.AgentTemplate](t, rec), "the refused update changed nothing")

	// The control: the same update with values left out is accepted and
	// clears them, as the whole-template replacement says it does.
	rec = f.do(t, "PUT", one, `{"name":"Researcher"}`, owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, apigen.AgentValues{}, decodeBody[apigen.AgentTemplate](t, rec).Values)
}
