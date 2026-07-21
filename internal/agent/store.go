package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Patch holds partial-update fields.
type Patch struct {
	Name          *string
	Roles         []string
	Policies      []string
	Organizations []grn.GRN
}

// Store persists Agent aggregates.
type Store interface {
	Create(ctx context.Context, a *Agent) error
	Get(ctx context.Context, g grn.GRN) (*Agent, error)
	List(ctx context.Context, f ListFilter) ([]*Agent, error)
	Update(ctx context.Context, g grn.GRN, p Patch) (*Agent, error)
	Delete(ctx context.Context, g grn.GRN) error

	AddOrganization(ctx context.Context, agentGRN, orgGRN grn.GRN) error
	RemoveOrganization(ctx context.Context, agentGRN, orgGRN grn.GRN) error
}

// ListFilter scopes a List call. Limit of 0 means "no limit".
type ListFilter struct {
	Offset int
	Limit  int
}
