package machineapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machineapi"
)

// confined is what ConfineSupervisors answers a supervisor outside its
// routes.
const confined = "a supervisor reaches its lifecycle routes alone"

// TestConfineSupervisors_ASupervisorReachesItsLifecycleRoutesAlone sends
// every operation api/machine.yaml declares, read off the generated
// registration, as a supervisor's certificate and as an agent's. The
// supervisor is refused everywhere but its identity, its renewal and
// the three execution routes it holds authority on; the agent, the
// control, is refused by the boundary nowhere, so what refused the
// supervisor was the kind of identity and not the route.
func TestConfineSupervisors_ASupervisorReachesItsLifecycleRoutesAlone(t *testing.T) {
	routes := machineRoutes(t, map[string]string{
		"agent":      agentGRN("acme", "0a1b2c3d4e5f6071").String(),
		"claim":      "c-1",
		"controller": operatorGRN("acme").String(),
		"message":    "m-1",
		"ref":        "ref-1",
		"activation": "00000000-0000-0000-0000-000000000001",
	})
	reached := map[string]bool{
		http.MethodGet + " /identity":                                              true,
		http.MethodPost + " /supervisors/self/certificate":                         true,
		http.MethodPost + " /agents/{agent}/activations":                           true,
		http.MethodPost + " /agents/{agent}/activations/{activation}/deactivation": true,
		http.MethodPost + " /agents/{agent}/execution/introspection":               true,
	}
	middlewares := []machinegen.MiddlewareFunc{machineapi.ConfineSupervisors, machineapi.RecoverPanics}
	// No service stands behind the server: a request the boundary
	// admits reaches a handler that cannot answer, which is what tells
	// it apart from one the boundary refused.
	srv := machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	supervisorURL, supervisor := serveTLS(t, srv, grn.New("acme", "default", "supervisor", "0a1b2c3d4e5f6071"), middlewares...)
	agentURL, agentClient := serveTLS(t, srv, agentGRN("acme", "0a1b2c3d4e5f6071"), middlewares...)

	for pattern, route := range routes {
		t.Run(pattern, func(t *testing.T) {
			status, body := send(t, supervisor, route.method, supervisorURL+route.path, "")
			if reached[pattern] {
				assert.NotContains(t, body, confined, "a supervisor's own route")
			} else {
				assert.Equal(t, http.StatusForbidden, status)
				assert.Contains(t, body, confined)
			}
			_, body = send(t, agentClient, route.method, agentURL+route.path, "")
			assert.NotContains(t, body, confined, "the control: an agent is not confined")
		})
	}
}
