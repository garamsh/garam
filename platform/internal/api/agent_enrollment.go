package api

import (
	"encoding/hex"
	"net/http"
	"regexp"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
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
		Placement: apigen.AgentEnrollmentPlacement(e.Placement),
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
	secret, err := parseSHA256(body.SecretSha256)
	if err != nil {
		return agent.ApprovalInput{}, errorx.Wrap(err, "secretSha256")
	}
	placement := agent.PlacementStandalone
	if body.Placement != nil {
		if placement, err = agent.ParsePlacement(string(*body.Placement)); err != nil {
			return agent.ApprovalInput{}, err
		}
	}
	in := agent.ApprovalInput{
		Actor: actor, Org: org, Placement: placement, RequestID: requestID, HostName: hostName,
		ExpiresAt: body.ExpiresAt, SecretSHA256: secret,
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

// sha256Pattern is a SHA-256 digest as the wire carries it.
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// parseSHA256 reads a SHA-256 digest as 64 lowercase hexadecimal
// characters.
func parseSHA256(s string) ([32]byte, error) {
	var d [32]byte
	if !sha256Pattern.MatchString(s) {
		return d, errorx.New(errorx.KindInvalidArgument, "a SHA-256 is 64 lowercase hexadecimal characters")
	}
	_, _ = hex.Decode(d[:], []byte(s))
	return d, nil
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

// ListAgentEnrollments pages the organization's standalone approvals
// in every state. Authorization is the service's agent:read, decided
// from current state as the writes beside it are. No secret, hash,
// fingerprint or certificate reaches the answer: the summary the store
// reads carries none.
func (s *Server) ListAgentEnrollments(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListAgentEnrollmentsParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	f, err := listfilter.Parse(params.Limit, params.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	enrollments, err := s.standalone.ListEnrollments(r.Context(), u.GRN, orgGRN(orgSlug), f)
	if err != nil {
		writeError(w, err)
		return
	}
	now := time.Now()
	out := make([]apigen.AgentEnrollmentListing, 0, len(enrollments))
	for _, e := range enrollments {
		out = append(out, enrollmentListingToAPI(e, now))
	}
	writeJSON(w, http.StatusOK, out)
}

func enrollmentListingToAPI(e agent.EnrollmentSummary, now time.Time) apigen.AgentEnrollmentListing {
	out := apigen.AgentEnrollmentListing{
		Id: openapi_types.UUID(e.ID), Placement: apigen.AgentEnrollmentListingPlacement(e.Placement),
		State: apigen.AgentEnrollmentListingState(e.State(now)), AgentGrn: e.Agent.String(),
		SupervisorGrn: e.Supervisor.String(), HostName: string(e.HostName), ApprovedBy: e.ApprovedBy.String(),
		CreatedAt: e.CreatedAt.UTC(), ExpiresAt: e.ExpiresAt.UTC(),
		SpentAt: utcOf(e.SpentAt), WithdrawnAt: utcOf(e.WithdrawnAt),
	}
	if e.SpentAt != nil {
		epoch := e.Epoch
		out.Epoch = &epoch
	}
	if r := e.Replaces; r != nil {
		out.Replaces = &apigen.AgentEnrollmentReplacement{AgentGrn: r.Agent.String(), AssigneeGrn: r.Assignee.String(), Epoch: r.Epoch}
	}
	return out
}

// ListAgentSupervisors pages the organization's supervisor
// registrations, active and revoked. Authorization is
// ListAgentEnrollments'; no serial, certificate or credential reaches
// the answer.
func (s *Server) ListAgentSupervisors(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListAgentSupervisorsParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	f, err := listfilter.Parse(params.Limit, params.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	supervisors, err := s.standalone.ListSupervisors(r.Context(), u.GRN, orgGRN(orgSlug), f)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.AgentSupervisor, 0, len(supervisors))
	for _, sv := range supervisors {
		state := apigen.SupervisorActive
		if sv.EndedAt != nil {
			state = apigen.SupervisorRevoked
		}
		out = append(out, apigen.AgentSupervisor{
			Grn: sv.GRN.String(), AgentGrn: sv.Agent.String(), EnrollmentId: openapi_types.UUID(sv.Enrollment),
			Placement: apigen.AgentSupervisorPlacement(sv.Placement), State: state, HostName: string(sv.HostName),
			Epoch: sv.Epoch, RegisteredAt: sv.RegisteredAt.UTC(), EndedAt: utcOf(sv.EndedAt),
			LastActivatedAt: utcOf(sv.LastActivatedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// utcOf renders an optional time in UTC.
func utcOf(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}
