// Package operator owns what an organization records about an operator
// and what that operator authenticates with: the record that the
// organization has it (ADR-0045), and which certificate garam last
// issued it, together with what it takes to replace that with the next
// one (ADR-0046).
//
// An operator provisions agents inside a cluster garam never dials
// (ADR-0012) and authenticates with a certificate of its own
// (ADR-0015). Neither of those left garam holding a record that the
// operator exists, so a definition could name one that never would.
// The registration is that record and no more: it issues nothing and
// admits nothing, and a registered operator proves itself exactly as
// an unregistered one does.
//
// The certificate is what proving itself takes. The first is obtained
// by enrolling: registering mints a one-time token, the operator
// generates a key pair in its own cluster and sends a certificate
// signing request presenting that token, and garam signs a key it never
// holds (ADR-0063). The command ADR-0032 ships still mints one out of
// band, and is what recovers an operator that has no token left. Every
// certificate after the first is issued here, over the connection the
// certificate being replaced authenticated.
//
// A token is permission to ask for a certificate, once. It is not a
// credential to the operator's identity: it authenticates no other
// call, it names the operator garam minted it for and no other, and
// spending it removes it.
//
// Only the newest certificate an operator holds may be renewed, and
// renewing moves that record. garam has no revocation (ADR-0014), so
// that is what bounds a stolen certificate: the operator it was taken
// from renews, the theft loses the lineage and expires, and whichever
// of the two is cut off learns of it at its next renewal.
//
// The agent's certificate is not this package's. An agent is admitted
// to its own by the claim its operator made (ADR-0033) and renews on
// the route ADR-0040 gave it; nothing here reads or writes that.
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
	// ErrNotAnOperator refuses a caller whose certificate names
	// something else. An agent authenticates on the same listener
	// (ADR-0015) and renews on the route of its own that the claim
	// admits it to (ADR-0040).
	ErrNotAnOperator = errorx.New(errorx.KindPermissionDenied, "certificate does not name an operator")
	// ErrCertificateExpired refuses a certificate that has run out.
	// This is the bootstrap boundary: an expired certificate has no
	// connection left to renew over, and the command ADR-0032 ships is
	// what mints the one that follows it.
	ErrCertificateExpired = errorx.New(errorx.KindPermissionDenied, "certificate has expired: the next one is minted out of band")
	// ErrRenewalTooEarly refuses a certificate that has too much life
	// left. A certificate replaceable from the moment it is issued has
	// no lifetime worth configuring.
	ErrRenewalTooEarly = errorx.New(errorx.KindFailedPrecondition, "certificate is not yet within its renewal window")
	// ErrSuperseded refuses a certificate garam has already replaced.
	// The caller holds a certificate that still authenticates and can
	// no longer be renewed: either another holder of the same identity
	// renewed first, or an answer this caller never received did. Both
	// are recovered the same way, by minting out of band again.
	ErrSuperseded = errorx.New(errorx.KindAlreadyExists, "a newer certificate has already been issued for this operator")
	// ErrEnrollmentInvalid refuses an enrollment token that is not
	// spendable. Unknown, already spent and expired are one refusal:
	// the caller presented no credential either way, and telling the
	// three apart would answer questions about tokens the caller does
	// not hold.
	ErrEnrollmentInvalid = errorx.New(errorx.KindUnauthenticated, "enrollment token is not valid")
)

// Operator is an organization's record that it has an operator under
// this GRN.
//
// It carries no state the operator reports and no credential. What an
// operator does is reported against the agents it holds (ADR-0012), and
// what it authenticates with is the certificate this package's Service
// issues — which reads no row here.
type Operator struct {
	grn          grn.GRN
	orgGRN       grn.GRN
	registeredBy grn.GRN
	createdAt    time.Time
}

// New records that orgGRN has an operator under identifier.
// registeredBy is the member that registered it.
//
// The identifier is supplied rather than minted, because it is already
// the subject of a certificate wherever an operator holds one
// (ADR-0032).
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
