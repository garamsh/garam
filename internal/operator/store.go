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
	// operator nothing here can enrol until somebody re-mints for it
	// (ADR-0064, ADR-0067).
	//
	// It answers the operator as stored, carrying the enrollment state
	// read in the same transaction rather than assumed from the write.
	// A GRN that already holds a certificate is registered under the
	// GRN that certificate names, and answers `enrolled` on the
	// registration that minted it a token it does not need.
	Register(ctx context.Context, o *Operator, e *Enrollment) (*Operator, error)
	// Remint replaces the operator's enrollment token with the one
	// minted here, so what was live before it stops being spendable
	// and one identity is never reachable by two (ADR-0067).
	//
	// It answers ErrOperatorNotFound where the organization has no
	// record under that GRN, and ErrOperatorAlreadyEnrolled where the
	// operator holds a certificate: that is the outcome the token
	// exists to produce, so an operator holding one has nothing a
	// token would buy, and whether it may hold a second credential
	// path is undecided. The state is read in the same transaction as
	// the write, by the derivation Register reads it by.
	Remint(ctx context.Context, e *Enrollment) error
	Get(ctx context.Context, g grn.GRN) (*Operator, error)
	// ListByOrg returns the organization's operators, ordered by GRN.
	ListByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Operator, error)
	// CountByOrg counts the organization's registered operators, which
	// deleting the organization removes with it. A certificate one of
	// them already holds is not counted, because that row outlives the
	// registration and the organization both (ADR-0045 errata).
	CountByOrg(ctx context.Context, orgGRN grn.GRN) (int, error)
}
