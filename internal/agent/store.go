package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
)

// Store persists Agents and the operator each one is assigned to, and
// reads the definitions an organization wrote of the agents it should
// have, which garam no longer accepts (ADR-0090). A
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
	// LatestGeneration answers the generation garam last activated for
	// the agent, whether that activation is current or has ended, or
	// nil for an agent never activated.
	LatestGeneration(ctx context.Context, agentGRN grn.GRN) (*Generation, error)
	// LatestGenerationsByOrg answers LatestGeneration for each of the
	// organization's agents; one never activated has no entry.
	LatestGenerationsByOrg(ctx context.Context, orgGRN grn.GRN) (map[grn.GRN]Generation, error)
	// ReportProvisioningState records what the operator assigned at
	// epoch says about the agent's pod, or ErrStaleEpoch.
	ReportProvisioningState(ctx context.Context, agentGRN grn.GRN, epoch int, state ProvisioningState) (*Agent, error)
	// ReportRuntimeState records what the agent assigned at epoch says
	// about itself, or ErrStaleEpoch, in one transaction the execution
	// fence admits first (ADR-0080).
	ReportRuntimeState(ctx context.Context, agentGRN grn.GRN, epoch int, state RuntimeState, fence Fencing) (*Agent, error)
	// CountByOrg counts the organization's agents, which deleting the
	// organization removes with it. An
	// organization that does not exist holds none, so the zero
	// OrgCounts is its answer rather than an error.
	CountByOrg(ctx context.Context, orgGRN grn.GRN) (OrgCounts, error)
	// Rename sets what the organization calls the agent, or clears it
	// where name is nil, and nothing else (ADR-0119). It answers
	// ErrAgentNotFound for an agent garam does not hold.
	Rename(ctx context.Context, g grn.GRN, name *Name) (*Agent, error)
	// SearchByOrg answers the organization's agents whose identifier
	// or name holds n, case aside, ordered by GRN.
	SearchByOrg(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*Agent, error)
}
