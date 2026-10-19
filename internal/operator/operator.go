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
// An operator enters by a key garam issues on its registration, whose
// assertions it exchanges for short-lived tokens (ADR-0102, ADR-0117).
// An operator authenticating by certificate obtained its first out of
// band, from the command ADR-0032 ships; every certificate after the
// first is issued here, over the connection the certificate being
// replaced authenticated.
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
	"strings"
	"time"
	"unicode/utf8"

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
)

// The bounds on what an organization says about its operator, in
// characters. migrations/000025 holds the same two.
const (
	NameMaxLength        = 100
	DescriptionMaxLength = 1000
)

// DetailsPatch is what a caller says about an operator's name and
// description, each absent where it says nothing (ADR-0074). A
// registration takes it for the details the record starts with, and an
// update for the ones it changes; the zero value says nothing, which
// is valid for both.
//
// Neither field is part of the operator's identity. The GRN is what a
// certificate names and what every other table joins on, so a patch
// reaches no credential, token or definition.
type DetailsPatch struct {
	name        *string
	description *string
}

// NewDetailsPatch parses what a caller said. A name is trimmed and must
// keep at least one character; a description is trimmed and may be
// empty, which clears it. Both are bounded in characters rather than
// bytes, so the bound a person reads is the one applied, and both
// must be text PostgreSQL stores: valid UTF-8 without NUL, which it
// refuses as a storage error rather than as the caller's.
func NewDetailsPatch(name, description *string) (DetailsPatch, error) {
	var p DetailsPatch
	if name != nil {
		n := strings.TrimSpace(*name)
		switch {
		case !storable(n):
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an operator's name must be text without NUL characters")
		case n == "":
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an operator's name must not be empty")
		case utf8.RuneCountInString(n) > NameMaxLength:
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an operator's name is at most 100 characters")
		}
		p.name = &n
	}
	if description != nil {
		d := strings.TrimSpace(*description)
		switch {
		case !storable(d):
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an operator's description must be text without NUL characters")
		case utf8.RuneCountInString(d) > DescriptionMaxLength:
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an operator's description is at most 1000 characters")
		}
		p.description = &d
	}
	return p, nil
}

// storable reports whether s is text a PostgreSQL text column holds.
func storable(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// Name returns the name the patch sets, or nil where it leaves it.
func (p DetailsPatch) Name() *string { return p.name }

// Description returns the description the patch sets, or nil where it
// leaves it.
func (p DetailsPatch) Description() *string { return p.description }

// Operator is an organization's record that it has an operator under
// this GRN, and where that operator stands in obtaining a certificate.
//
// It carries no state the operator reports and no credential. What an
// operator does is reported against the agents it holds (ADR-0012), and
// what it authenticates with is the certificate this package's Service
// issues. The last two fields are what garam already knows about that
// certificate, read beside the record and persisted nowhere on it: the
// state is a property of the tables together rather than of this row.
// Name and Description are persisted on the row and exported, as
// structure.md asks of what an aggregate persists (ADR-0074).
type Operator struct {
	grn          grn.GRN
	orgGRN       grn.GRN
	registeredBy grn.GRN
	createdAt    time.Time

	// Name is what the organization calls the operator. Two operators
	// may share one; the GRN is what tells them apart (ADR-0074).
	Name string
	// Description is what the organization says the operator is for,
	// empty where it says nothing.
	Description string
	// ControlEndpoint is where the operator's control service answers
	// the console, nil where none is registered. Only an actor holding
	// operator:delegate moves it (#1170).
	ControlEndpoint *ControlEndpoint

	// When garam last issued the operator a certificate, zero where it
	// has issued none.
	certificateIssuedAt time.Time
}

// New records that orgGRN has an operator under identifier.
// registeredBy is the member that registered it, and details what it
// says about the operator; a name it leaves out is the identifier.
//
// The identifier is supplied rather than minted, because it is already
// the subject of a certificate wherever an operator holds one
// (ADR-0032).
func New(orgGRN grn.GRN, identifier string, registeredBy grn.GRN, details DetailsPatch) (*Operator, error) {
	g, err := GRNOf(orgGRN, identifier)
	if err != nil {
		return nil, err
	}
	name := g.Identifier
	if details.name != nil {
		name = *details.name
	}
	var description string
	if details.description != nil {
		description = *details.description
	}
	return &Operator{
		grn:          g,
		orgGRN:       orgGRN,
		registeredBy: registeredBy,
		createdAt:    time.Now().UTC(),
		Name:         name,
		Description:  description,
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

// CertificateIssuedAt returns when garam last issued the operator a
// certificate, and the zero time where it has issued none.
//
// It is the current certificate's and not the first one's: a renewal
// moves the record this reads (ADR-0046) and what came before is not
// kept, so this answers when garam last signed for the operator rather
// than when the operator enrolled.
func (o *Operator) CertificateIssuedAt() time.Time { return o.certificateIssuedAt }
