package machineapi_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/machineapi"
)

// confined is what ConfineSupervisors answers a supervisor outside its
// routes.
const confined = "a supervisor reaches its lifecycle routes alone"

// TestConfineSupervisors_ASupervisorReachesItsLifecycleRoutesAlone sends
// every operation api/machine.yaml declares, read off the generated
// registration, as a supervisor's token and as an agent's. The
// supervisor is refused everywhere but its identity, the three
// execution routes it holds authority on and its agent's token mint;
// the agent, the control, is refused by the boundary nowhere, so what
// refused the supervisor was the kind of identity and not the route.
func TestConfineSupervisors_ASupervisorReachesItsLifecycleRoutesAlone(t *testing.T) {
	routes := machineRoutes(t, map[string]string{
		"agent":      agentGRN("acme", "0a1b2c3d4e5f6071").String(),
		"claim":      "c-1",
		"controller": operatorGRN("acme").String(),
		"message":    "m-1",
		"ref":        "ref-1",
		"activation": "00000000-0000-0000-0000-000000000001",
		"approval":   "00000000-0000-0000-0000-000000000002",
	})
	reached := map[string]bool{
		http.MethodGet + " /identity":                                              true,
		http.MethodPost + " /agents/{agent}/activations":                           true,
		http.MethodPost + " /agents/{agent}/tokens":                                true,
		http.MethodPost + " /agents/{agent}/activations/{activation}/deactivation": true,
		http.MethodPost + " /agents/{agent}/execution/introspection":               true,
		// The client assertion exchange refuses every credential
		// beside its assertion itself, a supervisor's included.
		http.MethodPost + " /oauth/token": true,
	}
	middlewares := []machinegen.MiddlewareFunc{machineapi.ConfineSupervisors, machineapi.RecoverPanics}
	// No service stands behind the server: a request the boundary
	// admits reaches a handler that cannot answer, which is what tells
	// it apart from one the boundary refused.
	srv := machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	supervisorURL, supervisor := serveTLS(t, srv, grn.New("acme", "supervisor", "0a1b2c3d4e5f6071"), middlewares...)
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

// TestConfineApps_AnInstanceReachesNoMachineRouteItsAppDoesNotList
// sends every operation api/machine.yaml declares as the connector's
// instance's token and as an agent's. The instance is refused on each
// (ADR-0192 decision 7, AUTH-4): its app lists the cluster-connection
// routes alone, which this router does not serve. The agent, the
// control, is refused by the boundary nowhere.
func TestConfineApps_AnInstanceReachesNoMachineRouteItsAppDoesNotList(t *testing.T) {
	const refused = "the caller's app does not reach this route"
	routes := machineRoutes(t, map[string]string{
		"agent":      agentGRN("acme", "0a1b2c3d4e5f6071").String(),
		"claim":      "c-1",
		"controller": operatorGRN("acme").String(),
		"message":    "m-1",
		"ref":        "ref-1",
		"activation": "00000000-0000-0000-0000-000000000001",
		"approval":   "00000000-0000-0000-0000-000000000002",
	})
	middlewares := []machinegen.MiddlewareFunc{machineapi.ConfineApps, machineapi.RecoverPanics}
	srv := machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	instanceURL, instanceClient := serveTLS(t, srv, connectedGRN, middlewares...)
	agentURL, agentClient := serveTLS(t, srv, agentGRN("acme", "0a1b2c3d4e5f6071"), middlewares...)

	for pattern, route := range routes {
		t.Run(pattern, func(t *testing.T) {
			status, body := send(t, instanceClient, route.method, instanceURL+route.path, "")
			assert.Equal(t, http.StatusForbidden, status)
			assert.Contains(t, body, refused)
			_, body = send(t, agentClient, route.method, agentURL+route.path, "")
			assert.NotContains(t, body, refused, "the control: an agent is not confined")
		})
	}
}

// TestConfineApps_AnInstanceReachesTheMachineOperationsItsAppLists
// holds that the confinement is read off the record and not off the
// caller's kind: an instance whose app lists `GET /identity` reaches
// it, and is still refused a route its app does not list.
func TestConfineApps_AnInstanceReachesTheMachineOperationsItsAppLists(t *testing.T) {
	listing := grn.New("acme", app.InstanceType, "00000000000000aa")
	instanceReach[listing] = machine.ReachOf([]string{http.MethodGet + " /identity"})
	t.Cleanup(func() { delete(instanceReach, listing) })
	middlewares := []machinegen.MiddlewareFunc{machineapi.ConfineApps, machineapi.RecoverPanics}
	srv := machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	base, client := serveTLS(t, srv, listing, middlewares...)

	status, body := send(t, client, http.MethodGet, base+"/identity", "")
	assert.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, listing.String())

	status, body = send(t, client, http.MethodPost, base+"/messages", "{}")
	assert.Equal(t, http.StatusForbidden, status)
	assert.Contains(t, body, "the caller's app does not reach this route")
}
