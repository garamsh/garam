package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
)

// Store persists Agents, the operator each one is assigned to, and the
// definitions an organization writes of the agents it should have. A
// state report carries the epoch it was produced at and is accepted
// only at the assignment's current epoch (ADR-0012).
//
// A definition is stored apart from the agent rows and never written
// alongside one: garam learns that an agent exists from the operator
// that reports it, and never from the definition it may have read
// (ADR-0026).
type Store interface {
	Create(ctx context.Context, a *Agent) error
	Get(ctx context.Context, g grn.GRN) (*Agent, error)
	// ListByOrg returns the organization's agents, ordered by GRN.
	ListByOrg(ctx context.Context, orgGRN grn.GRN, f listfilter.Filter) ([]*Agent, error)
	// Assign binds the agent to an operator at the next epoch,
	// replacing whichever operator held it before.
	Assign(ctx context.Context, agentGRN, operatorGRN grn.GRN) (*Assignment, error)
	GetAssignment(ctx context.Context, agentGRN grn.GRN) (*Assignment, error)
	// ListAssignmentsByOrg returns the assignments of the
	// organization's agents, ordered by agent GRN. An unassigned agent
	// has none, so the result is not one per agent.
	ListAssignmentsByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Assignment, error)
	// ReportProvisioningState records what the operator assigned at
	// epoch says about the agent's pod, or ErrStaleEpoch.
	ReportProvisioningState(ctx context.Context, agentGRN grn.GRN, epoch int, state ProvisioningState) (*Agent, error)
	// ReportRuntimeState records what the agent assigned at epoch says
	// about itself, or ErrStaleEpoch.
	ReportRuntimeState(ctx context.Context, agentGRN grn.GRN, epoch int, state RuntimeState) (*Agent, error)
	// Define records what an organization says an agent should be. It
	// creates no agent and assigns none: a definition is a record, and
	// the agent it describes exists when an operator says so.
	Define(ctx context.Context, d *Definition) error
	// CountByOrg counts the organization's agents and definitions,
	// which deleting the organization removes with it. An organization
	// that does not exist holds neither, so the zero OrgCounts is its
	// answer rather than an error.
	CountByOrg(ctx context.Context, orgGRN grn.GRN) (OrgCounts, error)
	// ListDefinitionsByOrg returns the organization's definitions,
	// ordered by the agent GRN each one names.
	ListDefinitionsByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Definition, error)
	// ListDefinitionsByOperator returns the definitions naming the
	// operator, each with the assignment claiming it, ordered by the
	// agent GRN each one names.
	ListDefinitionsByOperator(ctx context.Context, operatorGRN grn.GRN) ([]*DefinitionState, error)
	// Claim binds the definition's agent to the operator: the agent row
	// and the assignment at its first epoch, in one transaction, so the
	// record and the binding never exist apart. It answers
	// ErrDefinitionNotFound when no definition names the agent for this
	// operator and ErrDefinitionClaimed when one already claimed it.
	Claim(ctx context.Context, agentGRN, operatorGRN grn.GRN) (*Assignment, error)
}
