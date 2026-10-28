package machineapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/wire"
)

// admissionReasons is the closed set of reasons agent-enrollment.v1
// names on its refusals (ADR-0102). A refusal outside it carries none.
var admissionReasons = []struct {
	err    error
	reason string
}{
	{agent.ErrApprovalBindingConflict, "binding_conflict"},
	{agent.ErrApprovalSpent, "approval_spent"},
	{agent.ErrAdmissionSuperseded, "superseded"},
	{agent.ErrDirectPlacementRequiresAdministratorApproval, "direct_placement_gated"},
}

// PrepareAgentEnrollment records the hosted control service's approval
// of one admission under a member's agent:create reference, or a
// replacement under an agent:replace one, the first stage of
// agent-enrollment.v1 (ADR-0102, ADR-0107).
func (s *Server) PrepareAgentEnrollment(w http.ResponseWriter, r *http.Request, params machinegen.PrepareAgentEnrollmentParams) {
	r, cancel := lifecycleBudget(r)
	defer cancel()
	caller, binding, ok := s.admissionParties(w, r, params.AgentEnrollmentContractVersion)
	if !ok {
		return
	}
	approval, created, err := s.admissions.Prepare(r.Context(), caller, binding)
	if err != nil {
		writeAdmissionError(w, r, err)
		return
	}
	b := approval.Binding
	answer := machinegen.AgentEnrollmentApproval{
		ApprovalId: openapi_types.UUID(approval.ID), RequestId: string(b.RequestID),
		Placement: machinegen.AgentEnrollmentApprovalPlacement(b.Placement), Assignee: b.Assignee.String(),
		OperationRef: b.OperationRef,
	}
	if r := b.Replaces; r != nil {
		answer.Replaces = &machinegen.AgentEnrollmentReplacement{
			Agent: r.Agent.String(), ExpectedAssignee: r.Assignee.String(), ExpectedEpoch: strconv.Itoa(r.Epoch),
		}
	}
	if n := b.Name; n != nil {
		name := string(*n)
		answer.Name = &name
	}
	writeJSON(w, createdOrReplayed(created), answer)
}

// AdmitAgentEnrollment admits the agent an approval binds, fenced and
// with no active generation, the second stage of agent-enrollment.v1:
// a new agent, or the move of an existing one a replacement binds.
// The assignee then delivers its tokens with mintAgentToken.
func (s *Server) AdmitAgentEnrollment(w http.ResponseWriter, r *http.Request, approvalID machinegen.ApprovalId, params machinegen.AdmitAgentEnrollmentParams) {
	r, cancel := lifecycleBudget(r)
	defer cancel()
	caller, binding, ok := s.admissionParties(w, r, params.AgentEnrollmentContractVersion)
	if !ok {
		return
	}
	id, err := agent.ParseApprovalID(approvalID)
	if err != nil {
		writeError(w, err)
		return
	}
	approval, created, err := s.admissions.Admit(r.Context(), caller, id, binding)
	if err != nil {
		writeAdmissionError(w, r, err)
		return
	}
	c := approval.Admitted
	writeJSON(w, createdOrReplayed(created), machinegen.AgentAdmission{
		Grn: c.Agent.String(), Placement: machinegen.AgentAdmissionPlacement(approval.Binding.Placement),
		Assignee: c.Controller.String(), Epoch: strconv.Itoa(c.Epoch), Lineage: openapi_types.UUID(c.Lineage),
		Mode: machinegen.AgentAdmissionModeFenced, CredentialDelivery: machinegen.MintAgentToken,
	})
}

// admissionParties stamps agent-enrollment.v1 on the answer and
// answers the caller and the binding its body carries, once the
// request names that value, presents a token and its caller is still
// registered; it writes the refusal itself otherwise. The contract
// takes no certificate.
func (s *Server) admissionParties(w http.ResponseWriter, r *http.Request, version string) (grn.GRN, agent.EnrollmentBinding, bool) {
	w.Header().Set(operator.ContractVersionHeader, agent.AdmissionContractVersion)
	if version != agent.AdmissionContractVersion {
		writeError(w, agent.ErrUnsupportedAdmissionContract)
		return grn.GRN{}, agent.EnrollmentBinding{}, false
	}
	identity, ok := identityOf(w, r)
	if !ok {
		return grn.GRN{}, agent.EnrollmentBinding{}, false
	}
	if !s.recheck(w, r, identity) {
		return grn.GRN{}, agent.EnrollmentBinding{}, false
	}
	var body machinegen.AgentEnrollmentBinding
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "agent enrollment", errorx.KindInvalidArgument))
		return grn.GRN{}, agent.EnrollmentBinding{}, false
	}
	binding, err := bindingOf(body)
	if err != nil {
		writeError(w, err)
		return grn.GRN{}, agent.EnrollmentBinding{}, false
	}
	return identity.GRN(), binding, true
}

// bindingOf parses an agent-enrollment.v1 body.
func bindingOf(body machinegen.AgentEnrollmentBinding) (agent.EnrollmentBinding, error) {
	requestID, err := agent.ParseRequestID(body.RequestId)
	if err != nil {
		return agent.EnrollmentBinding{}, err
	}
	placement, err := agent.ParsePlacement(string(body.Placement))
	if err != nil {
		return agent.EnrollmentBinding{}, err
	}
	assignee, err := grn.Parse(body.Assignee)
	if err != nil {
		return agent.EnrollmentBinding{}, errorx.Wrap(err, "assignee", errorx.KindInvalidArgument)
	}
	name, err := optionalName(body.Name)
	if err != nil {
		return agent.EnrollmentBinding{}, err
	}
	b := agent.EnrollmentBinding{
		RequestID: requestID, Placement: placement, Assignee: assignee, OperationRef: body.OperationRef, Name: name,
	}
	if r := body.Replaces; r != nil {
		replaced, err := grn.Parse(r.Agent)
		if err != nil {
			return agent.EnrollmentBinding{}, errorx.Wrap(err, "replaced agent", errorx.KindInvalidArgument)
		}
		expected, err := grn.Parse(r.ExpectedAssignee)
		if err != nil {
			return agent.EnrollmentBinding{}, errorx.Wrap(err, "expected assignee", errorx.KindInvalidArgument)
		}
		epoch, err := parseEpoch(r.ExpectedEpoch)
		if err != nil {
			return agent.EnrollmentBinding{}, err
		}
		b.Replaces = &agent.Replacement{Agent: replaced, Assignee: expected, Epoch: epoch}
	}
	return b, nil
}

// writeAdmissionError answers a refusal with the reason its contract
// names for it, and every other refusal at its kind's status.
func writeAdmissionError(w http.ResponseWriter, r *http.Request, err error) {
	err = withinBudget(r, err)
	for _, c := range admissionReasons {
		if errors.Is(err, c.err) {
			writeErrorReason(w, wire.StatusOf(errorx.KindOf(err)), err, c.reason)
			return
		}
	}
	writeError(w, err)
}
