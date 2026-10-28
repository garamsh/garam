package user

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
)

// Patch holds partial-update fields. Nil entries are unchanged.
type Patch struct {
	Nickname *string
	Roles    []string
	Policies []string
}

// Store persists User aggregates. Org membership is owned by the org
// domain (org.Store membership methods), not duplicated here.
type Store interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, g grn.GRN) (*User, error)
	// ListByGRNs resolves many Users at once. GRNs without a user are
	// absent from the result rather than an error.
	ListByGRNs(ctx context.Context, grns []grn.GRN) ([]*User, error)
	// SearchByGRNs answers the Users among grns whose nickname or
	// identifier holds n, case aside, ordered by GRN.
	SearchByGRNs(ctx context.Context, grns []grn.GRN, n listfilter.Needle) ([]*User, error)
	Update(ctx context.Context, g grn.GRN, p Patch) (*User, error)
	Delete(ctx context.Context, g grn.GRN) error
}
