package machineapi_test

import (
	"context"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// markGate is the wait of a route that creates an agent, answering the
// error the test sets and recording the marks it was asked about.
type markGate struct {
	policyfeed.Gate
	err    error
	marked [][]policyfeed.Entity
}

func (g *markGate) AwaitMarks(_ context.Context, marks []policyfeed.Entity) error {
	g.marked = append(g.marked, marks)
	return g.err
}

type creating struct {
	agent.ExecutionService
	creation agent.ManagedCreation
	created  bool
}

func (c *creating) CreateManaged(context.Context, grn.GRN, agent.ManagedCreationInput) (*agent.ManagedCreation, bool, error) {
	return &c.creation, c.created, nil
}

type enrolling struct {
	agent.SupervisorService
	result  agent.EnrollmentResult
	created bool
}

func (e *enrolling) Enroll(context.Context, agent.EnrollmentInput) (*agent.EnrollmentResult, bool, error) {
	return &e.result, e.created, nil
}

type admitting struct {
	agent.AdmissionService
	approval agent.EnrollmentApproval
	created  bool
}

func (a *admitting) Admit(context.Context, grn.GRN, agent.ApprovalID, agent.EnrollmentBinding) (*agent.EnrollmentApproval, bool, error) {
	return &a.approval, a.created, nil
}

// post sends a JSON body with headers and returns the status.
func post(t *testing.T, client *http.Client, url, body string, headers map[string]string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// TestAgentCreatingRoutes_AnswerAfterThePolicyServiceHoldsTheAgent
// holds that each machine route that writes a new agent asks the gate
// about the agent's entity and answers 503 at the bound while the
// agent stays stored (ADR-0165), whether the request created the
// agent or replayed the creation; and that a route that moves an agent
// that exists asks nothing, so an older mark of the agent is not its
// answer to give.
//
// The clear gate is the control: the same request answers its own
// status, so the 503 is the wait and not the route. Without a gate the
// routes answer as before.
func TestAgentCreatingRoutes_AnswerAfterThePolicyServiceHoldsTheAgent(t *testing.T) {
	controller := operatorGRN("acme")
	newAgent := agentGRN("acme", "0a1b2c3d4e5f6071")
	mark := []policyfeed.Entity{{Type: policyfeed.AgentType, ID: newAgent.String()}}
	approval := uuid.NewString()
	binding := `{"requestId":"r-1","placement":"managed","assignee":"` + controller.String() +
		`","operationRef":"ref-1"}`
	replacing := `{"requestId":"r-1","placement":"managed","assignee":"` + controller.String() +
		`","operationRef":"ref-1","replaces":{"agent":"` + newAgent.String() +
		`","expectedAssignee":"` + controller.String() + `","expectedEpoch":"1"}}`
	enrollment := agent.EnrollmentResult{Placement: agent.PlacementManaged, Agent: newAgent,
		Supervisor: grn.New("acme", "supervisor", "0a1b2c3d4e5f6071"), Epoch: 1}
	replaced := agent.ActivationID(uuid.New())

	type route struct {
		name    string
		build   func(created bool) *machineapi.Server
		path    string
		body    string
		headers map[string]string
		waits   bool
		status  int
	}
	creation := agent.ManagedCreation{Agent: newAgent, Controller: controller, Epoch: 1}
	admitted := func(replaces *agent.Replacement) agent.EnrollmentApproval {
		return agent.EnrollmentApproval{
			Binding:  agent.EnrollmentBinding{Placement: agent.PlacementManaged, Replaces: replaces},
			Admitted: &creation,
		}
	}
	table := []route{
		{name: "createManagedAgent", waits: true, status: http.StatusCreated,
			path:    "/operators/" + neturl.PathEscape(controller.String()) + "/managed-agents",
			body:    `{"requestId":"r-1","operationRef":"ref-1"}`,
			headers: map[string]string{"Garam-Contract-Version": agent.TokenEnrollmentContractVersion},
			build: func(created bool) *machineapi.Server {
				return machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, &recheck{}, nil,
					&creating{creation: creation, created: created}, nil, nil, nil)
			}},
		{name: "admitAgentEnrollment new agent", waits: true, status: http.StatusCreated,
			path: "/agent-enrollment-approvals/" + approval + "/admission", body: binding,
			headers: map[string]string{"Garam-Contract-Version": agent.AdmissionContractVersion},
			build: func(created bool) *machineapi.Server {
				return machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, &recheck{}, nil, nil, nil,
					&admitting{approval: admitted(nil), created: created}, nil)
			}},
		{name: "admitAgentEnrollment replacement", waits: false, status: http.StatusCreated,
			path: "/agent-enrollment-approvals/" + approval + "/admission", body: replacing,
			headers: map[string]string{"Garam-Contract-Version": agent.AdmissionContractVersion},
			build: func(created bool) *machineapi.Server {
				return machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, &recheck{}, nil, nil, nil,
					&admitting{approval: admitted(&agent.Replacement{Agent: newAgent, Assignee: controller, Epoch: 1}),
						created: created}, nil)
			}},
		{name: "enrollStandaloneAgent new agent", waits: true, status: http.StatusCreated,
			path: "/agent-enrollments", body: `{"requestId":"r-1"}`,
			headers: map[string]string{"Garam-Contract-Version": agent.CredentialStandaloneContractVersion,
				"Authorization": "Garam-Bootstrap secret"},
			build: func(created bool) *machineapi.Server {
				return machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
					&enrolling{result: enrollment, created: created}, nil, nil)
			}},
		{name: "enrollStandaloneAgent replacement", waits: false, status: http.StatusCreated,
			path: "/agent-enrollments", body: `{"requestId":"r-1"}`,
			headers: map[string]string{"Garam-Contract-Version": agent.CredentialStandaloneContractVersion,
				"Authorization": "Garam-Bootstrap secret"},
			build: func(created bool) *machineapi.Server {
				moved := enrollment
				moved.Replaced, moved.ReplacesActivation = true, &replaced
				return machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
					&enrolling{result: moved, created: created}, nil, nil)
			}},
		// An agent never activated has no activation to name, so the
		// answer's ReplacesActivation is nil as a new agent's is: the
		// store's Replaced is what tells them apart.
		{name: "enrollStandaloneAgent replacement of an agent never activated", waits: false,
			status: http.StatusCreated,
			path:   "/agent-enrollments", body: `{"requestId":"r-1"}`,
			headers: map[string]string{"Garam-Contract-Version": agent.CredentialStandaloneContractVersion,
				"Authorization": "Garam-Bootstrap secret"},
			build: func(created bool) *machineapi.Server {
				moved := enrollment
				moved.Replaced = true
				return machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
					&enrolling{result: moved, created: created}, nil, nil)
			}},
	}
	for _, r := range table {
		t.Run(r.name, func(t *testing.T) {
			gate := &markGate{}
			url, client := serveTLS(t, r.build(true).WithFeed(gate), controller)
			status := post(t, client, url+r.path, r.body, r.headers)
			require.Equal(t, r.status, status, "the change is in force")
			if r.waits {
				assert.Equal(t, [][]policyfeed.Entity{mark}, gate.marked)
			} else {
				assert.Empty(t, gate.marked, "a move of an agent that exists raises nothing")
			}

			gate.err = policyfeed.ErrNotApplied
			status = post(t, client, url+r.path, r.body, r.headers)
			if r.waits {
				assert.Equal(t, http.StatusServiceUnavailable, status, "the agent is stored, not applied")
			} else {
				assert.Equal(t, r.status, status, "answered at once whatever an older mark says")
			}

			if r.waits {
				replayURL, replayClient := serveTLS(t, r.build(false).WithFeed(gate), controller)
				assert.Equal(t, http.StatusServiceUnavailable,
					post(t, replayClient, replayURL+r.path, r.body, r.headers),
					"a replay waits too: the first request may have timed out")
			}

			noFeedURL, noFeed := serveTLS(t, r.build(true), controller)
			assert.Equal(t, r.status, post(t, noFeed, noFeedURL+r.path, r.body, r.headers), "without a gate")
		})
	}
}
