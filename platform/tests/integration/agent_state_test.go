package integration_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/agent"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
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
	a, err := agentpg.New(p).Get(context.Background(), g)
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
// authenticate with credentials this listener verified, so what refuses is the one comparison each route makes rather than a
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
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)

	holds := scopedGRN("acme", operator.ServiceType, "one")
	holder := operatorClient(t, p, url, holds)
	sibling := operatorClient(t, p, url, scopedGRN("acme", operator.ServiceType, "two"))

	// The epoch a report carries is the assignment's: provisioned assigns
	// the agent once, so it is the first.
	f, active, execution := lineaged(t, p, holds)
	agentGRN, self := f.agent, f.client(t, execution)
	assignment, err := agentpg.New(p).GetAssignment(ctx, agentGRN)
	require.NoError(t, err)
	epoch := assignment.Epoch
	require.Equal(t, 1, epoch)
	_, stranger := provisioned(t, url, holder)

	provisioning := url + "/agents/" + agentGRN.String() + "/provisioning-state"
	runtime := url + "/agents/" + agentGRN.String() + "/runtime-state"

	// Before either party reports, garam cannot tell this agent from
	// one that never started.
	before := storedAgent(t, p, agentGRN)
	require.Equal(t, agent.ProvisioningStatePending, before.ProvisioningState)
	require.Equal(t, agent.RuntimeStateUnknown, before.RuntimeState)

	// The operator reports the pod, and moves that fact alone.
	status, body := machineSend(t, holder, http.MethodPost, provisioning, reportBody(epoch, "ready"))
	require.Equal(t, http.StatusNoContent, status, string(body))
	assert.Empty(t, body, "a recorded report answered with a body")
	reported := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.ProvisioningStateReady, reported.ProvisioningState)
	assert.Equal(t, agent.RuntimeStateUnknown, reported.RuntimeState,
		"the operator's report moved the fact the agent reports about itself")

	// The agent reports itself, presenting its own token, and leaves the operator's fact standing.
	status, body = machineSend(t, self, http.MethodPost, runtime, reportBody(epoch, "connected"))
	require.Equal(t, http.StatusNoContent, status, string(body))
	running := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.RuntimeStateConnected, running.RuntimeState)
	assert.Equal(t, agent.ProvisioningStateReady, running.ProvisioningState,
		"the agent's report moved the fact its operator reports")

	// Each route answers one party, and each of these callers is
	// refused on the route that is not its own.
	// The runtime route reads the caller's credential serial, which an
	// operator's token carries none of, so it is refused before any
	// decision is made (ADR-0100).
	refused := map[string]struct {
		client *http.Client
		url    string
		body   string
		status int
		kind   string
	}{
		"an operator that does not hold the agent": {sibling, provisioning, reportBody(epoch, "failed"), http.StatusForbidden, "permission_denied"},
		"the agent reporting on its own pod":       {self, provisioning, reportBody(epoch, "failed"), http.StatusForbidden, "permission_denied"},
		"the operator reporting for the agent":     {holder, runtime, reportBody(epoch, "disconnected"), http.StatusUnauthorized, "unauthenticated"},
		"another agent of the organization":        {stranger, runtime, reportBody(epoch, "disconnected"), http.StatusForbidden, "permission_denied"},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			status, body := machineSend(t, tt.client, http.MethodPost, tt.url, tt.body)
			assert.Equal(t, tt.status, status, string(body))
			assert.Contains(t, string(body), `"kind":"`+tt.kind+`"`)
		})
	}
	unmoved := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.ProvisioningStateReady, unmoved.ProvisioningState, "a refused report was recorded")
	assert.Equal(t, agent.RuntimeStateConnected, unmoved.RuntimeState, "a refused report was recorded")

	// The same operator is assigned the agent again, which raises the
	// epoch and moves nothing else.
	reassigned, err := agentpg.New(p).Assign(ctx, agentGRN, holds)
	require.NoError(t, err)
	require.Equal(t, 2, reassigned.Epoch)

	status, body = machineSend(t, holder, http.MethodPost, provisioning, reportBody(epoch, "failed"))
	assert.Equal(t, http.StatusConflict, status, "the operator reporting at the epoch it has left")
	assert.Contains(t, string(body), `"kind":"failed_precondition"`)
	// The agent's token was minted at the epoch it has left, so the
	// listener refuses it before any route reads the body (ADR-0080).
	// Activated again and minted a token at the new epoch, the agent
	// reporting the old one meets the route's own fence.
	status, body = machineSend(t, self, http.MethodPost, runtime, reportBody(reassigned.Epoch, "disconnected"))
	assert.Equal(t, http.StatusUnauthorized, status, "a token minted at the epoch the agent has left")
	_, execution = f.activate(t, "reassigned", generation(2), &active.ID, f.valid(t, "1"))
	self = f.client(t, execution)
	status, body = machineSend(t, self, http.MethodPost, runtime, reportBody(epoch, "disconnected"))
	assert.Equal(t, http.StatusConflict, status, "the agent reporting at the epoch it was told")
	assert.Contains(t, string(body), `"kind":"failed_precondition"`)
	stale := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.ProvisioningStateReady, stale.ProvisioningState, "a stale report was recorded")
	assert.Equal(t, agent.RuntimeStateConnected, stale.RuntimeState, "a stale report was recorded")

	// The control for the fence: the same two calls at the epoch the
	// assignment is now on are answered.
	status, body = machineSend(t, holder, http.MethodPost, provisioning, reportBody(reassigned.Epoch, "failed"))
	require.Equal(t, http.StatusNoContent, status, string(body))
	status, body = machineSend(t, self, http.MethodPost, runtime, reportBody(reassigned.Epoch, "disconnected"))
	require.Equal(t, http.StatusNoContent, status, string(body))
	current := storedAgent(t, p, agentGRN)
	assert.Equal(t, agent.ProvisioningStateFailed, current.ProvisioningState)
	assert.Equal(t, agent.RuntimeStateDisconnected, current.RuntimeState)
}
