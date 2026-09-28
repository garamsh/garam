package operator

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Store persists the operators an organization has registered.
//
// Its one update is to what the organization says about the operator,
// which moves nothing the operator authenticates or enrols by
// (ADR-0074). There is a delete, and it is the one write here that
// takes something away: the record is what an operator's certificate
// authenticates on, so removing it stops the operator rather than
// tidying a list (ADR-0070).
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
	// UpdateDetails applies patch to the operator's name and
	// description and answers the operator as stored, or
	// ErrOperatorNotFound where the organization holds no record under
	// that GRN. It writes neither the GRN nor anything keyed on it, so
	// the certificate lineage, an unspent token and the definitions
	// naming the operator stand as they were (ADR-0074).
	UpdateDetails(ctx context.Context, g grn.GRN, patch DetailsPatch) (*Operator, error)
	// Delete removes the record, and with it the record of the
	// certificate garam last issued the operator and any token still
	// unspent. It answers ErrOperatorNotFound where the organization
	// holds no record under that GRN, and
	// ErrOperatorNamedByDefinition where an agent definition names the
	// operator — nothing is deleted in that case.
	Delete(ctx context.Context, g grn.GRN) error
	Get(ctx context.Context, g grn.GRN) (*Operator, error)
	// ListByOrg returns the organization's operators, ordered by GRN.
	ListByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Operator, error)
	// CountByOrg counts the organization's registered operators, which
	// deleting the organization removes with it. A certificate one of
	// them already holds is not counted, because that row outlives the
	// registration and the organization both (ADR-0045 errata).
	CountByOrg(ctx context.Context, orgGRN grn.GRN) (int, error)
}
