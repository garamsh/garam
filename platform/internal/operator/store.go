package operator

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// Store persists the operators an organization has registered.
//
// Its one update is to what the organization says about the operator,
// which moves nothing the operator authenticates by (ADR-0074). There
// is a delete, and it is the one write here that takes something away:
// the record is what an operator's tokens authenticate on, so
// removing it stops the operator rather than tidying a list (ADR-0070).
type Store interface {
	// Register records the operator, or ErrOperatorAlreadyRegistered
	// when the organization already has one under that GRN. It answers
	// the operator as stored, read in the same transaction.
	Register(ctx context.Context, o *Operator) (*Operator, error)
	// RegisterPair records a manager and its control service, whose
	// Manager names it, in one transaction, and answers the manager as
	// stored (ADR-0124). Either GRN already registered is
	// ErrOperatorAlreadyRegistered, and the control service is refused
	// as Register refuses it; a refusal records neither.
	RegisterPair(ctx context.Context, manager, control *Operator) (*Operator, error)
	// SetManager makes the record under g the control service of
	// manager, once, and answers it as stored (ADR-0124). A record that
	// already names a manager is ErrManagerAlreadySet; manager is
	// refused as Register refuses a control service's, and g as
	// ErrIdentityIsManager where a record names it as its manager. It
	// answers ErrOperatorNotFound where the organization holds no
	// record under g.
	SetManager(ctx context.Context, g, manager grn.GRN) (*Operator, error)
	// IssueKey records k's public key for its operator as rotation
	// says it stands beside the keys already held, and answers its
	// public record (ADR-0102). KeyRotationImmediate withdraws every
	// live key and deletes its tokens in the same write; planned
	// starts the predecessor's RolloverWindow and refuses a third live
	// key with ErrKeyLimit; first refuses an operator that has ever
	// been issued a key, a withdrawn one included, with
	// ErrOperatorWasKeyed. It answers ErrOperatorNotFound
	// where the organization holds no record under that GRN.
	IssueKey(ctx context.Context, k *IssuedKey, rotation KeyRotation) (Key, error)
	// ListKeys answers every key the operator was issued, withdrawn
	// ones included, oldest first, or ErrOperatorNotFound.
	ListKeys(ctx context.Context, g grn.GRN) ([]Key, error)
	// RevokeKey withdraws the operator's key kid and deletes its
	// tokens in one write, so the next request presenting one is
	// refused (ADR-0102). A key already withdrawn answers nil; one the
	// operator was never issued answers ErrKeyNotFound.
	RevokeKey(ctx context.Context, g grn.GRN, kid string) error
	// UpdateDetails applies patch to the operator's name and
	// description and answers the operator as stored, or
	// ErrOperatorNotFound where the organization holds no record under
	// that GRN. It writes neither the GRN nor anything keyed on it, so
	// its keys stand as they were (ADR-0074).
	UpdateDetails(ctx context.Context, g grn.GRN, patch DetailsPatch) (*Operator, error)
	// SetControlEndpoint writes the operator's control endpoint, nil
	// clearing it, and answers the operator as stored, or
	// ErrOperatorNotFound. It writes nothing else on the record.
	SetControlEndpoint(ctx context.Context, g grn.GRN, endpoint *ControlEndpoint) (*Operator, error)
	// Delete removes the record, and with it its keys and its tokens. It
	// answers ErrOperatorNotFound where the organization holds no record
	// under that GRN.
	Delete(ctx context.Context, g grn.GRN) error
	Get(ctx context.Context, g grn.GRN) (*Operator, error)
	// ListByOrg returns the organization's operators, ordered by GRN.
	ListByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Operator, error)
	// CountByOrg counts the organization's registered operators, which
	// deleting the organization removes with it.
	CountByOrg(ctx context.Context, orgGRN grn.GRN) (int, error)
}
