package user

import (
	"context"

	"github.com/garamsh/gaon/internal/grn"
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
	List(ctx context.Context, f ListFilter) ([]*User, error)
	Update(ctx context.Context, g grn.GRN, p Patch) (*User, error)
	Delete(ctx context.Context, g grn.GRN) error
}

// ListFilter scopes a List call. Limit of 0 means "no limit".
type ListFilter struct {
	Offset int
	Limit  int
}
