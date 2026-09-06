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
// The registration is that record, and it is what the certificate
// authenticates on: an operator the organization no longer holds a
// record of does not authenticate, which is what deleting one
// withdraws (ADR-0070). It still issues nothing — the record admits a
// certificate and mints none.
//
// The certificate is what proving itself takes. The first is obtained
// by enrolling: registering mints a one-time token, the operator
// generates a key pair in its own cluster and sends a certificate
// signing request presenting that token, and garam signs a key it never
// holds (ADR-0064). An operator that lost its token before obtaining a
// certificate is re-minted one (ADR-0067); the command ADR-0032 ships
// still mints out of band, and is what recovers the operator that
// already holds a certificate, for which no token is minted again.
// Every certificate after the first is issued here, over the connection
// the certificate being replaced authenticated.
//
// A token is permission to ask for a certificate, once. It is not a
// credential to the operator's identity: it authenticates no other
// call, it names the operator garam minted it for and no other, and
// spending it removes it. An operator holds at most one at a time: a
// re-mint replaces what was there, so one identity never has two live
// credential paths.
//
// Only the newest certificate an operator holds may be renewed, and
// renewing moves that record. That is what bounds a stolen
// certificate while the operator is still wanted: the operator it was
// taken from renews, the theft loses the lineage and expires, and
// whichever of the two is cut off learns of it at its next renewal.
// It narrows a theft rather than ending one, and what ends one is
// deleting the operator, which stops every certificate naming it at
// once (ADR-0070).
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
	// ErrOperatorNamedByDefinition refuses deleting an operator an
	// agent definition names. The definition says which operator is to
	// run its agent, so removing the operator underneath it would
	// leave the definition naming a record garam no longer holds —
	// the state registering an operator exists to prevent (ADR-0045).
	// Nothing is deleted, and the definitions go or move first.
	ErrOperatorNamedByDefinition = errorx.New(errorx.KindFailedPrecondition,
		"an agent definition names this operator: point it at another operator or delete it first")
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
	// ErrOperatorAlreadyEnrolled refuses a re-mint for an operator that
	// holds a certificate. Whether a member may issue a second
	// credential path to an identity that already has one is not
	// decided, and this refusal is where that stands rather than a
	// gap somebody has to find out about (ADR-0067). The message says
	// so, because a person meeting it should learn a decision was
	// made and not that something broke.
	ErrOperatorAlreadyEnrolled = errorx.New(errorx.KindFailedPrecondition,
		"this operator has already enrolled: a second token would be a second credential path to an identity that has one, which garam does not decide to issue")
	// ErrEnrollmentInvalid refuses an enrollment token that is not
	// spendable. Unknown, already spent and expired are one refusal:
	// the caller presented no credential either way, and telling the
	// three apart would answer questions about tokens the caller does
	// not hold.
	ErrEnrollmentInvalid = errorx.New(errorx.KindUnauthenticated, "enrollment token is not valid")
)

// EnrollmentState is where an operator stands in obtaining a
// certificate from garam. It is derived from what garam holds — a
// certificate it issued, a token still spendable — and reported by
// nobody: garam never dials an operator (ADR-0012), so no member of
// this type says whether one is running.
//
// What the members mean to a browser caller is
// `api/openapi.yaml` §OperatorEnrollmentState, which is authoritative
// for that (ADR-0059).
type EnrollmentState string

const (
	// EnrollmentStateEnrolled is an operator garam has issued a
	// certificate for. It says one was obtained, not that the operator
	// still holds a valid one: no expiry is recorded beside the record
	// and nothing is asked (ADR-0046).
	EnrollmentStateEnrolled EnrollmentState = "enrolled"
	// EnrollmentStatePending is an operator with no certificate and a
	// token still inside its window. It lasts at most EnrollmentTTL
	// from the registration that minted it.
	EnrollmentStatePending EnrollmentState = "pending"
	// EnrollmentStateExpired is an operator with no certificate and no
	// spendable token. The token was lost or ran out, and this is the
	// state a re-mint answers: it is refused for an operator holding a
	// certificate and for no other (ADR-0067).
	EnrollmentStateExpired EnrollmentState = "expired"
)

// Operator is an organization's record that it has an operator under
// this GRN, and where that operator stands in obtaining a certificate.
//
// It carries no state the operator reports and no credential. What an
// operator does is reported against the agents it holds (ADR-0012), and
// what it authenticates with is the certificate this package's Service
// issues. The two fields below are what garam already knows about that
// certificate, read beside the record and persisted nowhere on it: the
// state is a property of the tables together rather than of this row.
type Operator struct {
	grn          grn.GRN
	orgGRN       grn.GRN
	registeredBy grn.GRN
	createdAt    time.Time

	// When garam last issued the operator a certificate, zero where it
	// has issued none.
	certificateIssuedAt time.Time
	// Whether a token minted for the operator is still spendable.
	liveToken bool
}

// New records that orgGRN has an operator under identifier.
// registeredBy is the member that registered it.
//
// The identifier is supplied rather than minted, because it is already
// the subject of a certificate wherever an operator holds one
// (ADR-0032).
func New(orgGRN grn.GRN, identifier string, registeredBy grn.GRN) (*Operator, error) {
	g, err := GRNOf(orgGRN, identifier)
	if err != nil {
		return nil, err
	}
	return &Operator{
		grn:          g,
		orgGRN:       orgGRN,
		registeredBy: registeredBy,
		createdAt:    time.Now().UTC(),
		// The record and the token it enrols with are one write
		// (ADR-0064), so a registration never exists without a live
		// token beside it. Whether a certificate already exists is
		// what this value cannot know, which is why [Store.Register]
		// answers the operator it stored rather than this one.
		liveToken: true,
	}, nil
}

// GRNOf names the operator of orgGRN registered under identifier.
//
// It is the one derivation of that name: registering builds the GRN it
// stores through this, and a route addressing an operator by its path
// segment builds the same one, so a segment either route refuses is
// refused by both.
func GRNOf(orgGRN grn.GRN, identifier string) (grn.GRN, error) {
	g := grn.New(orgGRN.Identifier, grn.DefaultProject, ServiceType, identifier)
	if err := g.Validate(); err != nil {
		return grn.GRN{}, errorx.Wrap(err, "operator: identifier", errorx.KindInvalidArgument)
	}
	if err := validateOwnership(g, orgGRN); err != nil {
		return grn.GRN{}, err
	}
	return g, nil
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

// EnrollmentState reports where the operator stands in obtaining a
// certificate.
//
// A certificate outranks a token, because it is the outcome the token
// exists to produce: an operator holding one has nothing left to do,
// whatever is still unspent beside it. That case is reachable — a GRN
// `garam issue-certificate` already minted for is registered under the
// GRN that certificate names, and the registration mints a token
// anyway — and it is not a fourth state, it is this order deciding it.
func (o *Operator) EnrollmentState() EnrollmentState {
	switch {
	case !o.certificateIssuedAt.IsZero():
		return EnrollmentStateEnrolled
	case o.liveToken:
		return EnrollmentStatePending
	default:
		return EnrollmentStateExpired
	}
}

// CertificateIssuedAt returns when garam last issued the operator a
// certificate, and the zero time where it has issued none.
//
// It is the current certificate's and not the first one's: a renewal
// moves the record this reads (ADR-0046) and what came before is not
// kept, so this answers when garam last signed for the operator rather
// than when the operator enrolled.
func (o *Operator) CertificateIssuedAt() time.Time { return o.certificateIssuedAt }
