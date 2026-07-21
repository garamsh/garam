package user

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Patch holds partial-update fields. Nil entries are unchanged.
type Patch struct {
	Nickname      *string
	Roles         []string
	Policies      []string
	Organizations []grn.GRN
}

// Store persists User aggregates. A User may belong to multiple
// Organizations; the membership is held directly on the User.
type Store interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, g grn.GRN) (*User, error)
	List(ctx context.Context, f ListFilter) ([]*User, error)
	Update(ctx context.Context, g grn.GRN, p Patch) (*User, error)
	Delete(ctx context.Context, g grn.GRN) error

	AddOrganization(ctx context.Context, userGRN, orgGRN grn.GRN) error
	RemoveOrganization(ctx context.Context, userGRN, orgGRN grn.GRN) error
}

// ListFilter scopes a List call. Limit of 0 means "no limit".
type ListFilter struct {
	Offset int
	Limit  int
}
