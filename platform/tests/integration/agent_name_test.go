package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
	"github.com/garamsh/garam/platform/internal/wire"
)

// namedCreationBody is a managed-enrollment.v2 creation asking for
// name.
func namedCreationBody(requestID, ref, name string) string {
	raw, _ := json.Marshal(map[string]string{"requestId": requestID, "operationRef": ref, "name": name})
	return string(raw)
}

// browse makes one browser API request as cookie's user and answers
// the status and body.
func browse(t *testing.T, method, url string, cookie *http.Cookie, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
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

// storedName reads the agent's name off its row, "" where it has none.
func storedName(t *testing.T, p *pgxpool.Pool, g grn.GRN) string {
	t.Helper()
	var name *string
	require.NoError(t, p.QueryRow(context.Background(), "SELECT name FROM agents WHERE grn = $1", g.String()).Scan(&name))
	if name == nil {
		return ""
	}
	return *name
}

// agentName reads the agent's name off the browser API as cookie's
// user, "" where it carries none.
func agentName(t *testing.T, browser string, cookie *http.Cookie, g grn.GRN) string {
	t.Helper()
	status, body := browse(t, http.MethodGet, browser+"/orgs/acme/agents/"+g.Identifier, cookie, "")
	require.Equal(t, http.StatusOK, status, body)
	var a apigen.Agent
	require.NoError(t, json.Unmarshal([]byte(body), &a))
	if a.Name == nil {
		return ""
	}
	return *a.Name
}

// TestAgentName_AManagedCreationNamesTheAgentAndARetryAsksForTheSameName:
// the name is stored trimmed and read back on the browser surface. A
// retry asking for it again answers the creation, and one asking for
// another name or none is a conflict. A creation asking for no name is
// the control: its agent carries none.
func TestAgentName_AManagedCreationNamesTheAgentAndARetryAsksForTheSameName(t *testing.T) {
	s := newManagedStack(t)
	ref := s.creationRef(t, s.member, "named-1")
	path := "/operators/" + s.controller.String() + "/managed-agents"
	got := s.enrollment(t, s.hostedTLS, path, namedCreationBody("named-1", ref, "  sherlock  "))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var m machinegen.ManagedAgent
	require.NoError(t, json.Unmarshal([]byte(got.body), &m))
	named, err := grn.Parse(m.Grn)
	require.NoError(t, err)
	assert.Equal(t, "sherlock", storedName(t, s.pool, named))

	again := s.enrollment(t, s.hostedTLS, path, namedCreationBody("named-1", ref, "sherlock"))
	assert.Equal(t, http.StatusOK, again.status, "the same name is the same request: %s", again.body)
	other := s.enrollment(t, s.hostedTLS, path, namedCreationBody("named-1", ref, "watson"))
	assert.Equal(t, http.StatusConflict, other.status, "another name is another request: %s", other.body)
	none := s.createManaged(t, s.hostedTLS, s.controller, "named-1", ref)
	assert.Equal(t, http.StatusConflict, none.status, "and so is no name: %s", none.body)

	unnamed := s.created(t, "unnamed-1")
	assert.Empty(t, storedName(t, s.pool, unnamed.agent))

	ref = s.creationRef(t, s.member, "named-2")
	blank := s.enrollment(t, s.hostedTLS, path, namedCreationBody("named-2", ref, "   "))
	assert.Equal(t, http.StatusBadRequest, blank.status, "a name left empty once trimmed is refused: %s", blank.body)
}

// namedBindingBody is an agent-enrollment.v1 managed binding asking for
// name.
func namedBindingBody(requestID string, assignee grn.GRN, ref, name string) string {
	raw, _ := json.Marshal(map[string]string{
		"requestId": requestID, "placement": "managed", "assignee": assignee.String(), "operationRef": ref, "name": name,
	})
	return string(raw)
}

// TestAgentName_AnApprovalBindsTheNameAndAdmissionWritesIt: the
// approval echoes the name, admitting it names the agent, and preparing
// again under the request identifier with another name conflicts. The
// identical retry is the control.
func TestAgentName_AnApprovalBindsTheNameAndAdmissionWritesIt(t *testing.T) {
	s := newTokenStack(t)
	ref := s.creationRef(t, s.member, "enrol-named")
	body := namedBindingBody("enrol-named", s.controller, ref, "sherlock")
	got := s.prepare(t, body)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var approval machinegen.AgentEnrollmentApproval
	require.NoError(t, json.Unmarshal([]byte(got.body), &approval))
	require.NotNil(t, approval.Name)
	assert.Equal(t, "sherlock", *approval.Name)

	again := s.prepare(t, body)
	assert.Equal(t, http.StatusOK, again.status, again.body)
	other := s.prepare(t, namedBindingBody("enrol-named", s.controller, ref, "watson"))
	assert.Equal(t, http.StatusConflict, other.status, "another name under the request identifier: %s", other.body)
	assert.Equal(t, "binding_conflict", reasonOf(t, other))

	admitted := s.admit(t, approval.ApprovalId.String(), body)
	require.Equal(t, http.StatusCreated, admitted.status, admitted.body)
	g, err := grn.Parse(admissionOf(t, admitted).Grn)
	require.NoError(t, err)
	assert.Equal(t, "sherlock", storedName(t, s.pool, g))
}

// TestAgentName_AReplacementCarriesNoName: a replacement moves an
// agent that keeps the name it has, so a binding naming one is refused
// before anything is written.
func TestAgentName_AReplacementCarriesNoName(t *testing.T) {
	s := newTokenStack(t)
	raw, _ := json.Marshal(map[string]any{
		"requestId": "replace-named", "placement": "managed", "assignee": s.controller.String(),
		"operationRef": "ref", "name": "sherlock",
		"replaces": map[string]string{"agent": s.agent.String(), "expectedAssignee": s.controller.String(), "expectedEpoch": "1"},
	})
	got := s.prepare(t, string(raw))
	assert.Equal(t, http.StatusBadRequest, got.status, got.body)
	assert.Contains(t, got.body, "keeps the agent's name")
}

// TestAgentName_RenameIsAuthorizedOnTheAgentsOwnGRN: an owner renames
// an agent and clears the name again. A Deny of agent:rename on one
// agent refuses ada that agent alone; her rename of its sibling and
// dan's rename of the denied one are the controls.
func TestAgentName_RenameIsAuthorizedOnTheAgentsOwnGRN(t *testing.T) {
	s := newListStack(t)
	denied, sibling := seedAgent(t, s.agents, s.acme), seedAgent(t, s.agents, s.acme)
	rename := func(cookie *http.Cookie, g grn.GRN, body string) (int, string) {
		return browse(t, http.MethodPatch, s.url+"/orgs/acme/agents/"+g.Identifier, cookie, body)
	}

	status, body := rename(s.ada, denied, `{"name":" sherlock "}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "sherlock", agentName(t, s.url, s.ada, denied))
	status, body = rename(s.ada, denied, `{"name":"`+strings.Repeat("n", 101)+`"}`)
	assert.Equal(t, http.StatusBadRequest, status, "a name over 100 characters is refused: %s", body)
	assert.Equal(t, "sherlock", agentName(t, s.url, s.ada, denied), "and changes nothing")

	deny := activePolicy(t, s.policies, s.acme, "deny-rename",
		statementDoc("deny-rename", wire.EffectDeny, org.ActionAgentRename, denied.String()))
	attachPolicy(t, s.policies, deny, s.adaGRN)
	status, body = rename(s.ada, denied, `{"name":"watson"}`)
	assert.Equal(t, http.StatusForbidden, status, "the Deny on the agent refuses its rename: %s", body)
	assert.Equal(t, "sherlock", agentName(t, s.url, s.dan, denied))
	status, body = rename(s.ada, sibling, `{"name":"watson"}`)
	assert.Equal(t, http.StatusOK, status, "control: the sibling renames: %s", body)
	status, body = rename(s.dan, denied, `{"name":null}`)
	require.Equal(t, http.StatusOK, status, "control: the Deny does not reach dan: %s", body)
	assert.Empty(t, agentName(t, s.url, s.dan, denied), "null clears the name")
}

// TestAgentName_AMemberRenamesAnAgent: agent:rename is the member's as
// it is every role's.
func TestAgentName_AMemberRenamesAnAgent(t *testing.T) {
	s := newListStack(t)
	ctx := context.Background()
	g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "mel")
	require.NoError(t, userpg.New(s.pool).Create(ctx, user.New(g, "mel")))
	require.NoError(t, s.orgs.AddMember(ctx, org.NewMembership(s.acme, g, org.RoleMember)))
	token, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, authnpg.NewSessionStore(s.pool).Create(ctx, token, authn.NewSession(g, time.Hour)))
	member := &http.Cookie{Name: "garam_session", Value: token}

	a := seedAgent(t, s.agents, s.acme)
	status, body := browse(t, http.MethodPatch, s.url+"/orgs/acme/agents/"+a.Identifier, member, `{"name":"sherlock"}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "sherlock", agentName(t, s.url, member, a))
}

// TestAgentName_SearchFindsAnAgentByItsName: the org search matches the
// name and answers it; the identifier still matches, which is the
// control that the name widened the match rather than replaced it.
func TestAgentName_SearchFindsAnAgentByItsName(t *testing.T) {
	s := newListStack(t)
	g := seedAgent(t, s.agents, s.acme)
	status, body := browse(t, http.MethodPatch, s.url+"/orgs/acme/agents/"+g.Identifier, s.ada, `{"name":"Sherlock"}`)
	require.Equal(t, http.StatusOK, status, body)

	for _, q := range []string{"herloc", g.Identifier[:6]} {
		status, body = s.get(t, "/orgs/acme/search", url.Values{"q": {q}}, s.ada)
		require.Equal(t, http.StatusOK, status, body)
		var found apigen.OrgSearch
		require.NoError(t, json.Unmarshal([]byte(body), &found))
		var agents []apigen.FoundObject
		for _, k := range found.Found {
			if k.Kind == apigen.FoundAgent {
				agents = k.Objects
			}
		}
		require.Len(t, agents, 1, "%q: %s", q, body)
		assert.Equal(t, g.String(), agents[0].Grn)
		require.NotNil(t, agents[0].Name)
		assert.Equal(t, "Sherlock", *agents[0].Name)
	}
}
