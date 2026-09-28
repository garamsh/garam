package machineapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
)

// defined is a definition of an agent operator is to run, as the
// console records one.
func defined(t *testing.T, operator grn.GRN, values agent.Values, id string) *agent.Definition {
	t.Helper()
	d, err := agent.NewDefinition(
		grn.New(grn.RootOwner, grn.DefaultProject, "org", operator.Organization),
		operator, values, grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada"))
	require.NoError(t, err)
	return d
}

// TestServer_AnswersTheDefinitionsNamingTheCaller covers the read's
// rendering: the agent GRN each definition minted, the values and their
// revision, and the claim state — absent while unclaimed, carrying the
// epoch once claimed. The two sit side by side so each shape is read
// against the other.
func TestServer_AnswersTheDefinitionsNamingTheCaller(t *testing.T) {
	operator := operatorGRN("acme")
	unclaimed := defined(t, operator, agent.Values{"model": "haiku"}, "0a1b2c3d4e5f6071")
	claimed := defined(t, operator, agent.Values{}, "9f8e7d6c5b4a3021")
	claimed.Revision = 4
	agents := &agentService{states: []*agent.DefinitionState{
		agent.NewDefinitionState(unclaimed, nil),
		agent.NewDefinitionState(claimed, agent.NewAssignment(claimed.Agent(), operator, 1)),
	}}
	url, client := listener(t, agents, nil, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, operator)

	status, body := do(t, client, http.MethodGet, url+"/definitions")
	require.Equal(t, http.StatusOK, status)
	assert.JSONEq(t, `[
		{"agentGrn": "`+unclaimed.Agent().String()+`", "values": {"model": "haiku"}, "revision": 1},
		{"agentGrn": "`+claimed.Agent().String()+`", "values": {}, "revision": 4, "claim": {"epoch": 1}}
	]`, body)
	assert.Equal(t, []call{{operator: operator}}, agents.calls,
		"the caller is what the certificate named")
}

// TestServer_ClaimsTheDefinitionThePathNames covers the claim's
// rendering: the assignment the claim wrote, and the refusals the
// service answers with carried to their statuses rather than invented
// at the boundary.
func TestServer_ClaimsTheDefinitionThePathNames(t *testing.T) {
	operator := operatorGRN("acme")
	d := defined(t, operator, agent.Values{}, "0a1b2c3d4e5f6071")
	agents := &agentService{assignment: agent.NewAssignment(d.Agent(), operator, 1)}
	url, client := listener(t, agents, nil, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, operator)

	status, body := do(t, client, http.MethodPost, url+"/definitions/"+d.Agent().String()+"/claim")
	require.Equal(t, http.StatusCreated, status)
	assert.JSONEq(t, `{
		"grn": "`+d.Agent().String()+`",
		"operator": "`+operator.String()+`",
		"epoch": 1
	}`, body)
	assert.Equal(t, []call{{operator: operator, subject: d.Agent()}}, agents.calls,
		"the caller is what the certificate named and the definition is what the path named")

	refused := map[string]struct {
		err    error
		status int
		kind   string
	}{
		"a definition the caller is not named in": {agent.ErrDefinitionNotFound, http.StatusNotFound, "not_found"},
		"a definition already claimed":            {agent.ErrDefinitionClaimed, http.StatusConflict, "already_exists"},
		"a caller that is not an operator":        {agent.ErrNotAnOperator, http.StatusForbidden, "permission_denied"},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			agents.err = tt.err
			defer func() { agents.err = nil }()
			status, body := do(t, client, http.MethodPost, url+"/definitions/"+d.Agent().String()+"/claim")
			assert.Equal(t, tt.status, status)
			assert.JSONEq(t, `{"kind":"`+tt.kind+`","message":"`+tt.err.Error()+`"}`, body)
		})
	}

	// A path segment that is not a GRN names no definition, and the
	// service is never asked about one.
	before := len(agents.calls)
	status, body = do(t, client, http.MethodPost, url+"/definitions/bot-1/claim")
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, body, `"kind":"invalid_argument"`)
	assert.Len(t, agents.calls, before, "an unparsed subject reached the service")
}
