package machineapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
)

// TestServer_ReportsTheStateTheBodyNamesAndParsesItFirst covers what
// this surface adds to a state report: it reads the caller off the
// connection, the agent off the path, and the state out of the body as
// a state this domain recognises. A word the domain does not name is
// refused here and never reaches the service, which is what the
// accepted report beside it shows — without it the refusal would hold
// as readily for a surface that stores nothing.
//
// Both routes are driven, because what separates them is which of the
// two facts a body may move and nothing else would catch them crossed.
func TestServer_ReportsTheStateTheBodyNamesAndParsesItFirst(t *testing.T) {
	operator := operatorGRN("acme")
	held := agentGRN("acme", "0a1b2c3d4e5f6071")
	agents := &agentService{}
	url, client := listener(t, agents, nil, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, operator)
	provisioning := url + "/agents/" + held.String() + "/provisioning-state"
	runtime := url + "/agents/" + held.String() + "/runtime-state"

	status, body := send(t, client, http.MethodPost, provisioning, `{"epoch":3,"state":"ready"}`)
	require.Equal(t, http.StatusNoContent, status, body)
	assert.Empty(t, body, "a recorded report answers with nothing")
	assert.Equal(t, []stateReport{{call: call{operator: operator, subject: held}, epoch: 3, state: "ready"}},
		agents.provisioning, "the caller is what the certificate named and the agent is what the path named")

	status, body = send(t, client, http.MethodPost, runtime, `{"epoch":3,"state":"connected"}`)
	require.Equal(t, http.StatusNoContent, status, body)
	assert.Equal(t, []stateReport{{call: call{operator: operator, subject: held}, epoch: 3, state: "connected"}},
		agents.runtime)

	// Each route carries one of the two facts. The other's vocabulary
	// is not a state it recognises, so it is refused rather than
	// written into the wrong field.
	refused := map[string]struct {
		url  string
		body string
	}{
		"a provisioning state this domain does not name": {provisioning, `{"epoch":3,"state":"running"}`},
		"a runtime state on the provisioning route":      {provisioning, `{"epoch":3,"state":"connected"}`},
		"a runtime state this domain does not name":      {runtime, `{"epoch":3,"state":"alive"}`},
		"a provisioning state on the runtime route":      {runtime, `{"epoch":3,"state":"ready"}`},
		"a body that is not an object":                   {runtime, `"connected"`},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			status, body := send(t, client, http.MethodPost, tt.url, tt.body)
			assert.Equal(t, http.StatusBadRequest, status)
			assert.Contains(t, body, `"kind":"invalid_argument"`)
		})
	}
	assert.Len(t, agents.provisioning, 1, "a report the surface could not parse reached the service")
	assert.Len(t, agents.runtime, 1, "a report the surface could not parse reached the service")

	// What the service refuses carries its kind rather than a status
	// invented at the boundary. A stale epoch is the one this surface
	// has no way to detect itself.
	agents.err = agent.ErrStaleEpoch
	status, body = send(t, client, http.MethodPost, provisioning, `{"epoch":1,"state":"ready"}`)
	assert.Equal(t, http.StatusConflict, status)
	assert.JSONEq(t, `{"kind":"failed_precondition","message":"`+agent.ErrStaleEpoch.Error()+`"}`, body)

	status, body = send(t, client, http.MethodPost, runtime, `{"epoch":1,"state":"connected"}`)
	assert.Equal(t, http.StatusConflict, status)
	assert.JSONEq(t, `{"kind":"failed_precondition","message":"`+agent.ErrStaleEpoch.Error()+`"}`, body)
}
