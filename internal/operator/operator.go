// Package operator owns the record that an organization has an
// operator (ADR-0045).
//
// An operator provisions agents inside a cluster garam never dials
// (ADR-0012) and authenticates with a certificate garam's own command
// mints (ADR-0032). Neither of those left garam holding a record that
// the operator exists, so a definition could name one that never
// would. This package holds that record and nothing else: it issues no
// credential, and a registered operator proves itself exactly as an
// unregistered one does.
package operator

import (
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ServiceType is the canonical GRN service-type for operators.
const ServiceType = "operator"

// Domain sentinel errors.
var (
	ErrOperatorNotFound = errorx.New(errorx.KindNotFound, "operator not found")
	// ErrOperatorAlreadyRegistered refuses a second registration of one
	// GRN. Registering is idempotent in effect and not in answer: the
	// caller learns the identifier is taken rather than being told it
	// registered what somebody else did.
	ErrOperatorAlreadyRegistered = errorx.New(errorx.KindAlreadyExists, "operator is already registered")
)

// Operator is an organization's record that it has an operator under
// this GRN.
//
// It carries no state the operator reports and no credential. What an
// operator does is reported against the agents it holds (ADR-0012), and
// what it authenticates with is the certificate ADR-0032's command
// mints — neither reads a row here.
type Operator struct {
	grn          grn.GRN
	orgGRN       grn.GRN
	registeredBy grn.GRN
	createdAt    time.Time
}

// New records that orgGRN has an operator under identifier.
// registeredBy is the member that registered it.
//
// The identifier is supplied rather than minted, which is what
// separates this from every other org resource garam creates. It is
// already the subject of a certificate wherever an operator holds one
// (ADR-0032), and garam cannot mint an identifier for a certificate
// that was issued before it was asked.
func New(orgGRN grn.GRN, identifier string, registeredBy grn.GRN) (*Operator, error) {
	g := grn.New(orgGRN.Identifier, grn.DefaultProject, ServiceType, identifier)
	if err := g.Validate(); err != nil {
		return nil, errorx.Wrap(err, "operator: identifier", errorx.KindInvalidArgument)
	}
	if err := validateOwnership(g, orgGRN); err != nil {
		return nil, err
	}
	return &Operator{
		grn:          g,
		orgGRN:       orgGRN,
		registeredBy: registeredBy,
		createdAt:    time.Now().UTC(),
	}, nil
}

// validateOwnership returns nil if g names an operator of the
// organization orgGRN identifies. An operator holds one organization's
// certificate (ADR-0015), so an operator of another organization is not
// a resource this one can register.
func validateOwnership(g, orgGRN grn.GRN) error {
	switch {
	case g.ServiceType != ServiceType:
		return errorx.New(errorx.KindInvalidArgument, "operator: grn does not name an operator")
	case g.Organization != orgGRN.Identifier:
		return errorx.New(errorx.KindInvalidArgument, "operator: operator belongs to another organization")
	}
	return nil
}

func (o *Operator) GRN() grn.GRN          { return o.grn }
func (o *Operator) Organization() grn.GRN { return o.orgGRN }

// RegisteredBy returns the member that registered the operator.
func (o *Operator) RegisteredBy() grn.GRN { return o.registeredBy }
func (o *Operator) CreatedAt() time.Time  { return o.createdAt }
