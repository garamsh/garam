package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// cutoverAuthority authorizes a cutover stage as an activation is
// authorized, and additionally requires an agent:cutover reference an
// owner or an administrator minted for this caller, this agent, the
// assignment it is on and the request target the stage was received
// at, read fresh for every stage (ADR-0086). The target is what makes
// a reference for one stage authorize no other.
func (s *executionService) cutoverAuthority(ctx context.Context, caller, agentGRN grn.GRN, ref, target string) (*Assignment, Grant, *OperationBinding, error) {
	assignment, grant, err := s.authorize(ctx, caller, agentGRN, OperationActivate)
	if err != nil {
		return nil, Grant{}, nil, err
	}
	b, err := s.operations.ResolveReference(ctx, caller, ref)
	if err != nil {
		return nil, Grant{}, nil, err
	}
	if b.Kind != OperationKindCutover || b.RequestTarget != target || !b.covers(caller, agentGRN, assignment) {
		return nil, Grant{}, nil, ErrReferenceMismatch
	}
	return assignment, grant, b, nil
}

// cutoverRequest is a stage the authority admitted, as the store
// rechecks it under the lock.
func cutoverRequest(caller grn.GRN, in CutoverInput, assignment *Assignment, grant Grant, b *OperationBinding) CutoverRequest {
	return CutoverRequest{
		Agent:        in.Agent,
		ImportID:     in.ImportID,
		Audience:     caller,
		GrantID:      grant.ID,
		OperationRef: b.Ref,
		Authority:    Lifecycle{Assignee: assignment.Assignee(), GrantExpiresAt: grant.ExpiresAt},
	}
}

func (s *executionService) GetCutover(ctx context.Context, caller grn.GRN, in CutoverInput) (*Cutover, error) {
	agentGRN := in.Agent
	assignment, _, _, err := s.cutoverAuthority(ctx, caller, agentGRN, in.OperationRef, in.RequestTarget)
	if err != nil {
		return nil, err
	}
	c, err := s.executions.GetCutover(ctx, agentGRN)
	if err != nil {
		return nil, err
	}
	// The authority was read for the assignee before; an answer about
	// another one would not be the one it authorized.
	if c.Assignee != assignment.Assignee() {
		return nil, ErrNotAssigned
	}
	return c, nil
}

func (s *executionService) FreezeCutover(ctx context.Context, caller grn.GRN, in FreezeInput) (*CutoverAttempt, bool, error) {
	assignment, grant, b, err := s.cutoverAuthority(ctx, caller, in.Agent, in.OperationRef, in.RequestTarget)
	if err != nil {
		return nil, false, err
	}
	return s.executions.Freeze(ctx, FreezeRequest{
		CutoverRequest: cutoverRequest(caller, in.CutoverInput, assignment, grant, b),
		Epoch:          in.Epoch,
		SourceDigest:   in.SourceDigest,
	})
}

func (s *executionService) SwitchCutover(ctx context.Context, caller grn.GRN, in CutoverInput) (*CutoverAttempt, error) {
	assignment, grant, b, err := s.cutoverAuthority(ctx, caller, in.Agent, in.OperationRef, in.RequestTarget)
	if err != nil {
		return nil, err
	}
	return s.executions.Switch(ctx, cutoverRequest(caller, in, assignment, grant, b))
}

func (s *executionService) RollBackCutover(ctx context.Context, caller grn.GRN, in CutoverInput) (*CutoverAttempt, error) {
	assignment, grant, b, err := s.cutoverAuthority(ctx, caller, in.Agent, in.OperationRef, in.RequestTarget)
	if err != nil {
		return nil, err
	}
	return s.executions.RollBack(ctx, cutoverRequest(caller, in, assignment, grant, b))
}
