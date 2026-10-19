package machineapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// CreateManagedAgent creates an agent assigned to a controller for the
// hosted control service calling, under a member's agent:create
// reference (ADR-0077). The caller's own credential and registration
// are read again first, as on every lifecycle decision. Creation opens
// the agent's lineage under managed-enrollment.v1 and v2 alike; under
// v2 its assignee then mints the agent's tokens in it (ADR-0100).
func (s *Server) CreateManagedAgent(w http.ResponseWriter, r *http.Request, controllerGRN machinegen.ControllerGrn, params machinegen.CreateManagedAgentParams) {
	if !served(w, params.EnrollmentContractVersion, enrollmentValues) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	identity, ok := identityOf(w, r)
	if !ok || !s.recheck(w, r, identity) {
		return
	}
	caller := identity.GRN()
	controller, err := grn.Parse(controllerGRN)
	if err != nil {
		writeError(w, errorx.Wrap(err, "controller", errorx.KindInvalidArgument))
		return
	}
	var body machinegen.ManagedAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "managed agent", errorx.KindInvalidArgument))
		return
	}
	requestID, err := agent.ParseRequestID(body.RequestId)
	if err != nil {
		writeError(w, err)
		return
	}
	name, err := optionalName(body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	creation, created, err := s.executions.CreateManaged(r.Context(), caller, agent.ManagedCreationInput{
		Controller: controller, RequestID: requestID, OperationRef: body.OperationRef, Name: name,
	})
	if err != nil {
		writeError(w, withinBudget(r, err))
		return
	}
	writeJSON(w, createdOrReplayed(created), machinegen.ManagedAgent{
		Grn: creation.Agent.String(), Epoch: strconv.Itoa(creation.Epoch),
	})
}

// optionalName parses a name a body may carry, nil where it carries
// none (ADR-0119).
func optionalName(sent *string) (*agent.Name, error) {
	if sent == nil {
		return nil, nil
	}
	n, err := agent.ParseName(*sent)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// createdOrReplayed is 201 for what this request wrote and 200 for
// what an identical earlier one did.
func createdOrReplayed(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}
