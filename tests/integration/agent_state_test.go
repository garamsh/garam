package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// reportBody is what either party posts: the epoch it believes the
// assignment is on, and the state it is reporting. One shape serves
// both routes, so which fact a report moves is the route it is posted
// to and never the body.
func reportBody(epoch int, state string) string {
	return `{"epoch":` + strconv.Itoa(epoch) + `,"state":"` + state + `"}`
}

// storedAgent reads the agent back out of Postgres. It is the row the
// browser-facing listener renders `provisioningState` and
// `runtimeState` from, so it is where a report either landed or did
// not.
func storedAgent(t *testing.T, p *pgxpool.Pool, g grn.GRN) *agent.Agent {
	t.Helper()
	a, err := agent.NewPostgresStore(p).Get(context.Background(), g)
	require.NoError(t, err)
	return a
}

// TestAgentState_TwoPartiesReportTwoFactsEachFencedByTheEpoch drives
// ADR-0053 over the listener a machine identity authenticates on,
// against real Postgres: an operator reports what it sees of the pod,
// the agent reports what it says about itself, and each report reaches
// the row the console reads.
//
// Every refusal sits beside the same call that lands. All four callers
// authenticate and hold certificates this listener verified — an
// operator and an agent reach it on the same terms (ADR-0015) — so
// what refuses is the one comparison each route makes rather than a
// surface that refuses whoever asks.
//
// The epoch fence is exercised on its own, by assigning the agent
// again to the operator already holding it: that raises the epoch and
// changes nothing else, so the earlier epoch is refused by the fence
// rather than by the caller comparison, and the same two calls at the
// new epoch are answered.
func TestAgentState_TwoPartiesReportTwoFactsEachFencedByTheEpoch(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)

	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	holds := scopedGRN("acme", operator.ServiceType, "one")
	holder := machineClient(t, installed(t, p, outOfBand, holds))
	sibling := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", operator.ServiceType, "two")))

	// The epoch a report carries is the one garam answered the operator
	// with, so it is read off the create rather than assumed.
	status, body := machineRequest(t, holder, http.MethodPost, url+"/agents")
	require.Equal(t, http.StatusCreated, status, string(body))
	var created machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal(body, &created))
	require.Equal(t, 1, created.Epoch)
	agentGRN, err := grn.Parse(created.Grn)
	require.NoError(t, err)
	self := machineClient(t, pairOf(t, issuedTo(t, holder, url, created.Grn)))
	_, stranger := provisioned(t, url, holder)

	provisioning := url + "/agents/" + created.Grn + "/provisioning-state"
	runtime := url + "/agents/" + created.Grn + "/runtime-state"

	// Before either party reports, garam cannot tell this agent from
	// one that never started.
	before := storedAgent(t, p, agentGRN)
	require.Equal(t, agent.ProvisioningStatePending, before.ProvisioningState())
	require.Equal(t, agent.RuntimeStateUnknown, before.RuntimeState())

	// The operator reports the pod, and moves that fact alone.
	status, body = machineSend(t, holder, http.MethodPost, provisioning, reportBody(created.Epoch, "ready"))
	require.Equal(t, http.StatusNoContent, status, string(body))
	assert.Empty(t, body, "a recorded report answered with a body")
	reported := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.ProvisioningStateReady, reported.ProvisioningState())
	assert.Equal(t, agent.RuntimeStateUnknown, reported.RuntimeState(),
		"the operator's report moved the fact the agent reports about itself")

	// The agent reports itself, over the connection its own certificate
	// authenticated, and leaves the operator's fact standing.
	status, body = machineSend(t, self, http.MethodPost, runtime, reportBody(created.Epoch, "connected"))
	require.Equal(t, http.StatusNoContent, status, string(body))
	running := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.RuntimeStateConnected, running.RuntimeState())
	assert.Equal(t, agent.ProvisioningStateReady, running.ProvisioningState(),
		"the agent's report moved the fact its operator reports")

	// Each route answers one party, and each of these callers is
	// refused on the route that is not its own.
	refused := map[string]struct {
		client *http.Client
		url    string
		body   string
	}{
		"an operator that does not hold the agent": {sibling, provisioning, reportBody(created.Epoch, "failed")},
		"the agent reporting on its own pod":       {self, provisioning, reportBody(created.Epoch, "failed")},
		"the operator reporting for the agent":     {holder, runtime, reportBody(created.Epoch, "disconnected")},
		"another agent of the organization":        {stranger, runtime, reportBody(created.Epoch, "disconnected")},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			status, body := machineSend(t, tt.client, http.MethodPost, tt.url, tt.body)
			assert.Equal(t, http.StatusForbidden, status, string(body))
			assert.Contains(t, string(body), `"kind":"permission_denied"`)
		})
	}
	unmoved := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.ProvisioningStateReady, unmoved.ProvisioningState(), "a refused report was recorded")
	assert.Equal(t, agent.RuntimeStateConnected, unmoved.RuntimeState(), "a refused report was recorded")

	// The same operator is assigned the agent again, which raises the
	// epoch and moves nothing else.
	reassigned, err := agent.NewPostgresStore(p).Assign(ctx, agentGRN, holds)
	require.NoError(t, err)
	require.Equal(t, 2, reassigned.Epoch())

	status, body = machineSend(t, holder, http.MethodPost, provisioning, reportBody(created.Epoch, "failed"))
	assert.Equal(t, http.StatusConflict, status, "the operator reporting at the epoch it has left")
	assert.Contains(t, string(body), `"kind":"failed_precondition"`)
	status, body = machineSend(t, self, http.MethodPost, runtime, reportBody(created.Epoch, "disconnected"))
	assert.Equal(t, http.StatusConflict, status, "the agent reporting at the epoch it was told")
	assert.Contains(t, string(body), `"kind":"failed_precondition"`)
	stale := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.ProvisioningStateReady, stale.ProvisioningState(), "a stale report was recorded")
	assert.Equal(t, agent.RuntimeStateConnected, stale.RuntimeState(), "a stale report was recorded")

	// The control for the fence: the same two calls at the epoch the
	// assignment is now on are answered.
	status, body = machineSend(t, holder, http.MethodPost, provisioning, reportBody(reassigned.Epoch(), "failed"))
	require.Equal(t, http.StatusNoContent, status, string(body))
	status, body = machineSend(t, self, http.MethodPost, runtime, reportBody(reassigned.Epoch(), "disconnected"))
	require.Equal(t, http.StatusNoContent, status, string(body))
	current := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.ProvisioningStateFailed, current.ProvisioningState())
	assert.Equal(t, agent.RuntimeStateDisconnected, current.RuntimeState())
}
