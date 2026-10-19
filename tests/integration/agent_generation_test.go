package integration_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	apigen "github.com/garamsh/garam/api"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	userpg "github.com/garamsh/garam/internal/user/postgres"
)

// agentReader is a browser-facing listener over the execution stack's
// database that serves the agent reads, which the stack's own browser
// server holds no agent store for. Sessions are rows, so the stack's
// cookies sign in here too.
func agentReader(t *testing.T, s *executionStack, engine grpc.ClientConnInterface) string {
	t.Helper()
	srv := api.NewServer(userpg.New(s.pool), orgpg.New(s.pool), agentpg.New(s.pool), nil, nil, nil,
		authn.NewPostgresSessionStore(s.pool), nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), nil, nil, nil, nil, api.Config{SessionTTL: time.Hour})
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	return browser.URL
}

// readAgents answers getAgent for s.agent and listAgents, as the
// member.
func readAgents(t *testing.T, url string, s *executionStack) (apigen.Agent, map[string]apigen.Agent) {
	t.Helper()
	get := func(path string, into any) {
		req, err := http.NewRequest(http.MethodGet, url+path, nil)
		require.NoError(t, err)
		req.AddCookie(s.member)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		require.NoError(t, json.Unmarshal(body, into))
	}
	var one apigen.Agent
	get("/orgs/acme/agents/"+s.agent.Identifier, &one)
	var all []apigen.Agent
	get("/orgs/acme/agents", &all)
	byGRN := make(map[string]apigen.Agent, len(all))
	for _, a := range all {
		byGRN[a.Grn] = a
	}
	return one, byGRN
}

// TestAgentRead_AnswersTheGenerationLastActivated reads the stack's
// agent before any activation, after its first and second, and after
// the second is deactivated, through both agent reads. The generation
// is absent until the first activation, then the latest one's, and a
// deactivation leaves it: it names the generation last activated, not
// one still admitted. An agent of the organization never activated is
// the control on the list: it carries none.
func TestAgentRead_AnswersTheGenerationLastActivated(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	url := agentReader(t, s, engine)

	one, all := readAgents(t, url, s)
	assert.Nil(t, one.Generation, "never activated")
	require.Contains(t, all, s.agent.String())
	assert.Nil(t, all[s.agent.String()].Generation)

	first := s.activated(t, "g-1", generationOf(1), nil, s.forwarded)
	one, all = readAgents(t, url, s)
	require.NotNil(t, one.Generation)
	assert.Equal(t, string(generationOf(1)), *one.Generation)
	assert.Equal(t, one.Generation, all[s.agent.String()].Generation, "the list answers what the read does")

	second := s.activated(t, "g-2", generationOf(2), &first.ActivationId, s.forwarded)
	one, _ = readAgents(t, url, s)
	require.NotNil(t, one.Generation)
	assert.Equal(t, string(generationOf(2)), *one.Generation, "the latest activation's")

	got := s.deactivate(t, second.ActivationId)
	require.Equal(t, http.StatusOK, got.status, got.body)
	one, all = readAgents(t, url, s)
	require.NotNil(t, one.Generation)
	assert.Equal(t, string(generationOf(2)), *one.Generation, "a deactivation leaves the generation last activated")

	quiet := seedAgent(t, agentpg.New(p), s.acme)
	_, all = readAgents(t, url, s)
	require.Contains(t, all, quiet.String(), "the agent never activated is listed")
	assert.Nil(t, all[quiet.String()].Generation, "control: an agent never activated carries none")
}
