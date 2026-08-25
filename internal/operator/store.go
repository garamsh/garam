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
	// Register records the operator, or ErrOperatorAlreadyRegistered
	// when the organization already has one under that GRN.
	Register(ctx context.Context, o *Operator) error
	Get(ctx context.Context, g grn.GRN) (*Operator, error)
	// ListByOrg returns the organization's operators, ordered by GRN.
	ListByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Operator, error)
}

// CertificateStore persists which certificate garam last issued for an
// operator. It is separate from Store because the two answer different
// questions: what the organization has registered, and what the holder
// of a certificate may still renew — a registered operator renews
// exactly as an unregistered one does.
//
// There is no read: what the rule needs is not the current certificate
// but whether the presented one is still it, and the write below
// answers that itself. A read in front of it would answer the same
// question a moment earlier, and the two can disagree.
type CertificateStore interface {
	// Adopt records current as the operator's certificate, whatever the
	// operator held before. This is the out-of-band mint (ADR-0032),
	// which is also the recovery: it takes the lineage from every
	// certificate that already names the operator.
	Adopt(ctx context.Context, current Current) error
	// Advance records current in place of predecessor, or ErrSuperseded
	// when the operator's certificate is not predecessor. An operator
	// with no record yet is adopted by its first renewal: nothing has
	// been superseded where nothing was recorded.
	Advance(ctx context.Context, current Current, predecessor Serial) error
}
