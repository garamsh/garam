package machineapi_test

import (
	"fmt"
	"net/http"
	"strings"
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

// TestServer_ReportsTheConfigurationOutcomeTheBodyNames covers what
// this surface adds to an acknowledgement: the caller off the
// connection, the agent off the path, and an outcome read whole with
// the one field it carries. Every refused body sits beside the accepted
// one that differs from it in the refused part alone, so what refuses
// is that part and not the route.
func TestServer_ReportsTheConfigurationOutcomeTheBodyNames(t *testing.T) {
	operator := operatorGRN("acme")
	held := agentGRN("acme", "0a1b2c3d4e5f6071")
	agents := &agentService{}
	url, client := listener(t, agents, nil, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, operator)
	route := url + "/agents/" + held.String() + "/configuration-state"

	status, body := send(t, client, http.MethodPost, route, `{"epoch":3,"revision":2,"outcome":"applied","ignoredKeys":["workspace","model"]}`)
	require.Equal(t, http.StatusNoContent, status, body)
	require.Len(t, agents.acknowledged, 1)
	got := agents.acknowledged[0]
	assert.Equal(t, call{operator: operator, subject: held}, got.call)
	assert.Equal(t, 3, got.epoch)
	assert.Equal(t, agent.Revision(2), got.revision)
	assert.Equal(t, agent.ConfigurationApplied{IgnoredKeys: []string{"model", "workspace"}}, got.outcome)
	assert.Empty(t, agents.provisioning, "an acknowledgement moved the provisioning state")

	status, body = send(t, client, http.MethodPost, route, `{"epoch":3,"revision":2,"outcome":"rejected","reason":"image pull failed"}`)
	require.Equal(t, http.StatusNoContent, status, body)
	require.Len(t, agents.acknowledged, 2)
	assert.Equal(t, agent.ConfigurationRejected{Reason: "image pull failed"}, agents.acknowledged[1].outcome)

	long := strings.Repeat("x", 500)
	keys := func(n int) string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf(`"key-%d"`, i)
		}
		return "[" + strings.Join(out, ",") + "]"
	}
	cases := map[string]struct{ refused, accepted string }{
		"a revision below 1": {
			`{"epoch":3,"revision":0,"outcome":"applied","ignoredKeys":[]}`,
			`{"epoch":3,"revision":1,"outcome":"applied","ignoredKeys":[]}`},
		"an outcome this domain does not name": {
			`{"epoch":3,"revision":1,"outcome":"partial","ignoredKeys":[]}`,
			`{"epoch":3,"revision":1,"outcome":"applied","ignoredKeys":[]}`},
		"an application naming no ignored keys": {
			`{"epoch":3,"revision":1,"outcome":"applied"}`,
			`{"epoch":3,"revision":1,"outcome":"applied","ignoredKeys":[]}`},
		"an application carrying a reason": {
			`{"epoch":3,"revision":1,"outcome":"applied","ignoredKeys":[],"reason":"x"}`,
			`{"epoch":3,"revision":1,"outcome":"applied","ignoredKeys":[]}`},
		"more ignored keys than a definition holds": {
			`{"epoch":3,"revision":1,"outcome":"applied","ignoredKeys":` + keys(33) + `}`,
			`{"epoch":3,"revision":1,"outcome":"applied","ignoredKeys":` + keys(32) + `}`},
		"an ignored key no definition could carry": {
			`{"epoch":3,"revision":1,"outcome":"applied","ignoredKeys":["tools.pins.git"]}`,
			`{"epoch":3,"revision":1,"outcome":"applied","ignoredKeys":["tools-pins-git"]}`},
		"a rejection carrying no reason": {
			`{"epoch":3,"revision":1,"outcome":"rejected"}`,
			`{"epoch":3,"revision":1,"outcome":"rejected","reason":"x"}`},
		"a rejection naming ignored keys": {
			`{"epoch":3,"revision":1,"outcome":"rejected","reason":"x","ignoredKeys":[]}`,
			`{"epoch":3,"revision":1,"outcome":"rejected","reason":"x"}`},
		"a reason longer than 500 characters": {
			`{"epoch":3,"revision":1,"outcome":"rejected","reason":"` + long + `x"}`,
			`{"epoch":3,"revision":1,"outcome":"rejected","reason":"` + long + `"}`},
		"a reason carrying a control character": {
			`{"epoch":3,"revision":1,"outcome":"rejected","reason":"image\npull"}`,
			`{"epoch":3,"revision":1,"outcome":"rejected","reason":"image pull"}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			before := len(agents.acknowledged)
			status, body := send(t, client, http.MethodPost, route, c.refused)
			assert.Equal(t, http.StatusBadRequest, status, body)
			assert.Contains(t, body, `"kind":"invalid_argument"`)
			assert.Len(t, agents.acknowledged, before, "a body the surface refused reached the service")

			status, body = send(t, client, http.MethodPost, route, c.accepted)
			assert.Equal(t, http.StatusNoContent, status, body)
			assert.Len(t, agents.acknowledged, before+1)
		})
	}

	// What the service refuses carries its kind rather than a status
	// invented at the boundary: revisions and epochs are facts this
	// surface does not hold.
	for _, refusal := range []error{agent.ErrStaleEpoch, agent.ErrRevisionAhead, agent.ErrRevisionSuperseded} {
		agents.err = refusal
		status, body = send(t, client, http.MethodPost, route, `{"epoch":1,"revision":1,"outcome":"applied","ignoredKeys":[]}`)
		assert.Equal(t, http.StatusConflict, status)
		assert.JSONEq(t, `{"kind":"failed_precondition","message":"`+refusal.Error()+`"}`, body)
	}
	agents.err = agent.ErrDefinitionNotFound
	status, _ = send(t, client, http.MethodPost, route, `{"epoch":1,"revision":1,"outcome":"applied","ignoredKeys":[]}`)
	assert.Equal(t, http.StatusNotFound, status)
}
