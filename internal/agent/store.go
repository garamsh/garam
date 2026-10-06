package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
)

// Store persists Agents and the operator each one is assigned to, and
// reads the definitions an organization wrote of the agents it should
// have, which garam no longer accepts (ADR-0090), and the templates it
// kept before they were retired read-only. A
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
	// Assign binds the agent to an assignee at the next epoch,
	// replacing whichever assignee held it before and ending the
	// activation its runtime held (ADR-0078).
	Assign(ctx context.Context, agentGRN, assigneeGRN grn.GRN) (*Assignment, error)
	GetAssignment(ctx context.Context, agentGRN grn.GRN) (*Assignment, error)
	// ListAssignmentsByOrg returns the assignments of the
	// organization's agents, ordered by agent GRN. An unassigned agent
	// has none, so the result is not one per agent.
	ListAssignmentsByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Assignment, error)
	// ReportProvisioningState records what the operator assigned at
	// epoch says about the agent's pod, or ErrStaleEpoch.
	ReportProvisioningState(ctx context.Context, agentGRN grn.GRN, epoch int, state ProvisioningState) (*Agent, error)
	// ReportRuntimeState records what the agent assigned at epoch says
	// about itself, or ErrStaleEpoch, in one transaction the execution
	// fence admits first (ADR-0080).
	ReportRuntimeState(ctx context.Context, agentGRN grn.GRN, epoch int, state RuntimeState, fence Fencing) (*Agent, error)
	// AdmitLegacyIssuance answers nil while the operator's certificate
	// route may answer for the agent, ErrIssuedByLifecycle once its
	// execution is fenced, ErrCutOver once its cutover is switched, or
	// ErrAgentNotFound. It reads both under the execution row FOR
	// SHARE (ADR-0086).
	AdmitLegacyIssuance(ctx context.Context, agentGRN grn.GRN) error
	// GetDefinition returns the definition naming the agent, or
	// ErrDefinitionNotFound.
	GetDefinition(ctx context.Context, agentGRN grn.GRN) (*Definition, error)
	// RenameDefinition changes what the organization calls the agent
	// and nothing else, or answers ErrDefinitionNotFound (ADR-0075).
	RenameDefinition(ctx context.Context, agentGRN grn.GRN, name Name) (*Definition, error)
	// CountByOrg counts the organization's agents, definitions and
	// templates, which deleting the organization removes with it. An
	// organization that does not exist holds none, so the zero
	// OrgCounts is its answer rather than an error.
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
	// GetTemplate returns the template, or ErrTemplateNotFound.
	// Templates are retired read-only, so reading is all a store does
	// with one (ADR-0088).
	GetTemplate(ctx context.Context, g grn.GRN) (*Template, error)
	// ListTemplatesByOrg returns the organization's templates, ordered
	// by name and then by GRN.
	ListTemplatesByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Template, error)
}
