package agent

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// AdmissionService is agent-enrollment.v1's approval and admission
// stages (ADR-0102 §Common agent enrollment), one service for both
// placements. Credential delivery is the assignee's mint of the
// agent's tokens and activation is the lifecycle authority's; neither
// is here. Each act reads the member's reference from current state.
type AdmissionService interface {
	// Prepare records the caller's approval of b and answers it,
	// created or answered again for an identical retry.
	Prepare(ctx context.Context, caller grn.GRN, b EnrollmentBinding) (*EnrollmentApproval, bool, error)
	// Admit spends the caller's approval id on the agent b binds and
	// answers it with what it admitted, created or answered again for
	// an identical retry.
	Admit(ctx context.Context, caller grn.GRN, id ApprovalID, b EnrollmentBinding) (*EnrollmentApproval, bool, error)
}

type admissionService struct {
	store      Store
	approvals  AdmissionStore
	operations OperationResolver
	now        func() time.Time
}

// NewAdmissionService constructs the service over the store it reads
// assignments from, the store it keeps approvals in and what reads a
// member's delegated reference fresh. A nil now takes time.Now.
func NewAdmissionService(store Store, approvals AdmissionStore, operations OperationResolver, now func() time.Time) AdmissionService {
	if now == nil {
		now = time.Now
	}
	return &admissionService{store: store, approvals: approvals, operations: operations, now: now}
}

func (s *admissionService) Prepare(ctx context.Context, caller grn.GRN, b EnrollmentBinding) (*EnrollmentApproval, bool, error) {
	if err := b.admissible(); err != nil {
		return nil, false, err
	}
	if _, err := authorizeAdmission(ctx, s.operations, caller, b); err != nil {
		return nil, false, err
	}
	a := EnrollmentApproval{
		ID: ApprovalID(uuid.New()), Org: grn.Org(caller.Organization), Audience: caller, Binding: b,
		CreatedAt: s.now().UTC().Truncate(time.Microsecond),
	}
	stored, created, err := s.approvals.PrepareApproval(ctx, a)
	if err != nil || created {
		return stored, created, err
	}
	if stored.Audience != caller || !stored.binds(b) {
		return nil, false, ErrApprovalBindingConflict
	}
	return stored, false, nil
}

func (s *admissionService) Admit(ctx context.Context, caller grn.GRN, id ApprovalID, b EnrollmentBinding) (*EnrollmentApproval, bool, error) {
	if err := b.admissible(); err != nil {
		return nil, false, err
	}
	if err := operates(caller); err != nil {
		return nil, false, err
	}
	approval, err := s.approvals.GetApproval(ctx, grn.Org(caller.Organization), id)
	if err != nil {
		return nil, false, err
	}
	if approval.Audience != caller {
		return nil, false, ErrApprovalNotFound
	}
	if !approval.binds(b) {
		if approval.Admitted != nil {
			return nil, false, ErrApprovalSpent
		}
		return nil, false, ErrApprovalBindingConflict
	}
	// The reference is current before anything is admitted or an
	// earlier admission answered.
	ref, err := authorizeAdmission(ctx, s.operations, caller, b)
	if err != nil {
		return nil, false, err
	}
	created := false
	if approval.Admitted == nil {
		agentGRN := ref.Target
		if b.Replaces == nil {
			if agentGRN, err = NewGRN(ref.Org); err != nil {
				return nil, false, err
			}
		}
		approval.Admitted, created, err = s.approvals.Admit(ctx, id, ManagedCreation{
			Agent: agentGRN, Audience: caller, Controller: b.Assignee, RequestID: b.RequestID,
			OperationRef: b.OperationRef, Lineage: NewLineage(), Name: b.Name,
		})
		if err != nil {
			return nil, false, err
		}
	}
	if !created {
		assignment, err := s.store.GetAssignment(ctx, approval.Admitted.Agent)
		if err != nil {
			return nil, false, err
		}
		if !approval.Admitted.placedOn(assignment) {
			return nil, false, ErrAdmissionSuperseded
		}
	}
	return approval, created, nil
}

// authorizeAdmission reads b's reference fresh for caller and answers
// it once it authorizes b: an agent:create reference for a new agent,
// an agent:replace one for a replacement.
func authorizeAdmission(ctx context.Context, operations OperationResolver, caller grn.GRN, b EnrollmentBinding) (*OperationBinding, error) {
	if b.Replaces == nil {
		return authorizeCreation(ctx, operations, caller, b.Assignee, b.RequestID, b.OperationRef, ErrApprovalBindingConflict)
	}
	return authorizeReplacement(ctx, operations, caller, b)
}

// authorizeReplacement reads b's reference fresh for caller and
// answers its binding once it is an agent:replace reference handed to
// caller in its own organization, minted under b's request identifier
// for the agent b replaces, at the assignment b expects, with b's
// assignee as its destination (ADR-0107). A reference of another kind,
// audience or organization is ErrReferenceMismatch; one minted for any
// other agent, assignment, destination or request identifier is
// ErrApprovalBindingConflict.
func authorizeReplacement(ctx context.Context, operations OperationResolver, caller grn.GRN, b EnrollmentBinding) (*OperationBinding, error) {
	if err := operates(caller); err != nil {
		return nil, err
	}
	if b.Assignee.ServiceType != ServiceTypeOperator || b.Assignee.Organization != caller.Organization {
		return nil, errorx.New(errorx.KindNotFound, "controller not found")
	}
	r := b.Replaces
	if ValidateOwnership(r.Agent, grn.Org(caller.Organization)) != nil {
		return nil, ErrAgentNotFound
	}
	ref, err := operations.ResolveReference(ctx, caller, b.OperationRef)
	if err != nil {
		return nil, err
	}
	if ref.Kind != OperationKindReplace || ref.Audience != caller || ref.Org != grn.Org(caller.Organization) {
		return nil, ErrReferenceMismatch
	}
	if ref.Target != r.Agent || ref.Assignee != r.Assignee || ref.Epoch != r.Epoch ||
		ref.Destination != b.Assignee || ref.RequestID != string(b.RequestID) {
		return nil, ErrApprovalBindingConflict
	}
	return ref, nil
}

// authorizeCreation reads ref fresh for caller and answers its binding
// once it is an agent:create reference handed to caller in its own
// organization, minted under requestID for assignee. A reference of
// another kind, audience or organization is ErrReferenceMismatch; one
// minted for another assignee or request identifier is conflict.
func authorizeCreation(ctx context.Context, operations OperationResolver, caller, assignee grn.GRN, requestID RequestID, ref string, conflict error) (*OperationBinding, error) {
	if err := operates(caller); err != nil {
		return nil, err
	}
	if assignee.ServiceType != ServiceTypeOperator || assignee.Organization != caller.Organization {
		return nil, errorx.New(errorx.KindNotFound, "controller not found")
	}
	b, err := operations.ResolveReference(ctx, caller, ref)
	if err != nil {
		return nil, err
	}
	if b.Kind != OperationKindCreate || b.Audience != caller || b.Org != grn.Org(caller.Organization) {
		return nil, ErrReferenceMismatch
	}
	if b.Target != assignee || b.RequestID != string(requestID) {
		return nil, conflict
	}
	return b, nil
}
