package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Store persists Agents and the operator each one is assigned to. A
// state report carries the epoch it was produced at and is accepted
// only at the assignment's current epoch (ADR-0012).
type Store interface {
	Create(ctx context.Context, a *Agent) error
	Get(ctx context.Context, g grn.GRN) (*Agent, error)
	// Assign binds the agent to an operator at the next epoch,
	// replacing whichever operator held it before.
	Assign(ctx context.Context, agentGRN, operatorGRN grn.GRN) (*Assignment, error)
	GetAssignment(ctx context.Context, agentGRN grn.GRN) (*Assignment, error)
	// ReportProvisioningState records what the operator assigned at
	// epoch says about the agent's pod, or ErrStaleEpoch.
	ReportProvisioningState(ctx context.Context, agentGRN grn.GRN, epoch int, state ProvisioningState) (*Agent, error)
	// ReportRuntimeState records what the agent assigned at epoch says
	// about itself, or ErrStaleEpoch.
	ReportRuntimeState(ctx context.Context, agentGRN grn.GRN, epoch int, state RuntimeState) (*Agent, error)
}
