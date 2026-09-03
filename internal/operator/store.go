package operator

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Store persists the operators an organization has registered.
//
// It is a registry and not a lifecycle: there is no update, because a
// record carries nothing to change, and no delete, because deleting one
// would stop nothing an operator can already do (ADR-0045).
type Store interface {
	// Register records the operator and mints the enrollment token it
	// starts with, or ErrOperatorAlreadyRegistered when the
	// organization already has one under that GRN. The two are one
	// write: a record that arrived without its token would be an
	// operator nothing here can enrol and no route can re-mint for
	// (ADR-0063).
	Register(ctx context.Context, o *Operator, e *Enrollment) error
	Get(ctx context.Context, g grn.GRN) (*Operator, error)
	// ListByOrg returns the organization's operators, ordered by GRN.
	ListByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Operator, error)
}
