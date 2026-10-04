package api

import (
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ApproveAgentEnrollment records an administrator's approval of what a
// standalone host prepared (ADR-0082). Authorization is the service's,
// decided from current state rather than through the cached authorize.
func (s *Server) ApproveAgentEnrollment(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.AgentEnrollmentApproval
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	in, err := approvalInput(u.GRN, orgGRN(orgSlug), body)
	if err != nil {
		writeError(w, err)
		return
	}
	e, created, err := s.standalone.Approve(r.Context(), in)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, apigen.AgentEnrollment{
		Id: openapi_types.UUID(e.ID), AgentGrn: e.Agent.String(), SupervisorGrn: e.Supervisor.String(),
		HostName: string(e.HostName), ExpiresAt: e.ExpiresAt.UTC(), Spent: e.Spent != nil,
	})
}

// approvalInput parses what an approval request carries.
func approvalInput(actor, org grn.GRN, body apigen.AgentEnrollmentApproval) (agent.ApprovalInput, error) {
	requestID, err := agent.ParseRequestID(body.RequestId)
	if err != nil {
		return agent.ApprovalInput{}, err
	}
	hostName, err := agent.ParseName(body.HostName)
	if err != nil {
		return agent.ApprovalInput{}, err
	}
	secret, err := agent.ParseKeyFingerprint(body.SecretSha256)
	if err != nil {
		return agent.ApprovalInput{}, errorx.Wrap(err, "secretSha256")
	}
	supervisorKey, err := agent.ParseKeyFingerprint(body.SupervisorKeySha256)
	if err != nil {
		return agent.ApprovalInput{}, errorx.Wrap(err, "supervisorKeySha256")
	}
	agentKey, err := agent.ParseKeyFingerprint(body.AgentKeySha256)
	if err != nil {
		return agent.ApprovalInput{}, errorx.Wrap(err, "agentKeySha256")
	}
	in := agent.ApprovalInput{
		Actor: actor, Org: org, RequestID: requestID, HostName: hostName, ExpiresAt: body.ExpiresAt,
		SecretSHA256: [32]byte(secret), SupervisorKey: supervisorKey, AgentKey: agentKey,
	}
	if r := body.Replaces; r != nil {
		agentGRN, err := grn.Parse(r.AgentGrn)
		if err != nil {
			return agent.ApprovalInput{}, errorx.Wrap(err, "replaces.agentGrn", errorx.KindInvalidArgument)
		}
		assignee, err := grn.Parse(r.AssigneeGrn)
		if err != nil {
			return agent.ApprovalInput{}, errorx.Wrap(err, "replaces.assigneeGrn", errorx.KindInvalidArgument)
		}
		in.Replaces = &agent.Replacement{Agent: agentGRN, Assignee: assignee, Epoch: r.Epoch}
	}
	return in, nil
}

// WithdrawAgentEnrollment withdraws an approval no host has spent.
func (s *Server) WithdrawAgentEnrollment(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, enrollmentID openapi_types.UUID) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.standalone.Withdraw(r.Context(), u.GRN, orgGRN(orgSlug), agent.EnrollmentID(enrollmentID)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RevokeAgentSupervisor ends a standalone host's supervisor and the
// activation its agent runs under.
func (s *Server) RevokeAgentSupervisor(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, supervisorSlug string) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	supervisor, err := grn.Parse(grn.New(orgSlug, grn.DefaultProject, agent.ServiceTypeSupervisor, supervisorSlug).String())
	if err != nil {
		writeError(w, errorx.Wrap(err, "supervisor", errorx.KindInvalidArgument))
		return
	}
	if err := s.standalone.Revoke(r.Context(), u.GRN, orgGRN(orgSlug), supervisor); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
