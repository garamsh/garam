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

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
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
	// ErrManagerNotRegistered refuses a control service registered for
	// a manager the organization holds no record of (ADR-0124).
	ErrManagerNotRegistered = errorx.New(errorx.KindFailedPrecondition, "the manager is not a registered operator of this organization")
	// ErrManagerIsControlService refuses a manager that is itself the
	// control service of another: an Operator is one manager and at
	// most one control service, never a chain (ADR-0124).
	ErrManagerIsControlService = errorx.New(errorx.KindFailedPrecondition, "the manager is itself a control service")
	// ErrManagerHasControlService refuses a second control service for
	// one manager (ADR-0124).
	ErrManagerHasControlService = errorx.New(errorx.KindAlreadyExists, "the manager already has a control service")
	// ErrManagerAlreadySet refuses setting the manager of a record that
	// names one: the link is set once and never moves (ADR-0124).
	ErrManagerAlreadySet = errorx.New(errorx.KindFailedPrecondition, "the record already names its manager")
	// ErrIdentityIsManager refuses registering as a control service an
	// identity a control service already names as its manager.
	ErrIdentityIsManager = errorx.New(errorx.KindFailedPrecondition, "a control service names this identity as its manager")
)

// The bounds on what an organization says about its operator, in
// characters. platform/migrations/000025 holds the same two.
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
		case !isStorableText(n):
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an operator's name must be UTF-8 text without NUL characters")
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
		case !isStorableText(d):
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an operator's description must be UTF-8 text without NUL characters")
		case utf8.RuneCountInString(d) > DescriptionMaxLength:
			return DetailsPatch{}, errorx.New(errorx.KindInvalidArgument, "an operator's description is at most 1000 characters")
		}
		p.description = &d
	}
	return p, nil
}

// isStorableText reports whether a Postgres text column accepts s: it
// takes valid utf-8 and refuses a nul byte. agent.ParseName checks a
// name the same way, in the same order.
func isStorableText(s string) bool {
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
// Every field is persisted on the row and exported: the type holds no
// invariant over them, so it is a data carrier (ADR-0069, ADR-0074).
type Operator struct {
	GRN          grn.GRN
	Organization grn.GRN
	// RegisteredBy is the member that registered the operator.
	RegisteredBy grn.GRN
	CreatedAt    time.Time

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
	// Manager is the operator this one is the control service of, nil
	// where it is a manager or stands alone (ADR-0124). It is written
	// when the control service is registered, or once by
	// Store.SetManager on a record naming none. It names the identity
	// and not a live record: deleting the manager leaves it naming a
	// GRN the organization no longer holds.
	Manager *grn.GRN
}

// ServeManager makes o the control service of the operator manager
// names (ADR-0124), as [ParseManager] reads it. Whether that operator
// is registered, and free to take a control service, is the store's
// to answer, read where it writes.
func (o *Operator) ServeManager(manager string) error {
	m, err := ParseManager(o.GRN, manager)
	if err != nil {
		return err
	}
	o.Manager = &m
	return nil
}

// ParseManager reads manager as the manager of the record under g: the
// GRN of another operator of g's organization (ADR-0124).
func ParseManager(g grn.GRN, manager string) (grn.GRN, error) {
	m, err := grn.Parse(manager)
	if err != nil {
		return grn.GRN{}, errorx.Wrap(err, "operator: manager", errorx.KindInvalidArgument)
	}
	if want, err := GRNOf(grn.Org(g.Organization), m.Identifier); err != nil || want != m {
		return grn.GRN{}, errorx.New(errorx.KindInvalidArgument, "operator: manager is not an operator of this organization")
	}
	if m == g {
		return grn.GRN{}, errorx.New(errorx.KindInvalidArgument, "operator: an operator is not its own manager")
	}
	return m, nil
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
		GRN:          g,
		Organization: orgGRN,
		RegisteredBy: registeredBy,
		CreatedAt:    time.Now().UTC(),
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
