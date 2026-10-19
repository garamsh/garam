// Package operator owns what an organization records about an operator
// and what that operator authenticates with: the record that the
// organization has it (ADR-0045), and the keys issued on that record,
// whose signed assertions it exchanges for short-lived tokens
// (ADR-0102, ADR-0117).
//
// An operator provisions agents inside a cluster garam never dials
// (ADR-0012). The registration is the record that it exists, and it is
// what its tokens authenticate on: an operator the organization no
// longer holds a record of does not authenticate, which is what
// deleting one withdraws (ADR-0070). garam issues an operator no
// certificate (ADR-0118).
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
	// ErrNotAnOperator refuses a caller or a stored identity that is
	// not an operator.
	ErrNotAnOperator = errorx.New(errorx.KindPermissionDenied, "the identity is not an operator")
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
// reaches no credential or token.
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
// this GRN.
//
// It carries no state the operator reports and no credential. What an
// operator does is reported against the agents it holds (ADR-0012), and
// what it authenticates with is a token its key was exchanged for.
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
}

// New records that orgGRN has an operator under identifier.
// registeredBy is the member that registered it, and details what it
// says about the operator; a name it leaves out is the identifier.
//
// The identifier is supplied rather than minted, because the operator
// is configured with it before it is registered.
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
// organization orgGRN identifies. An operator of another organization
// is not a resource this one can register.
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
