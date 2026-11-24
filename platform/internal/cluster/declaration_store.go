package cluster

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// DeclarationStore keeps the declarations placed on each record and
// the log of their changes (ADR-0187). Each write holds the record's
// row, appends one change at the record's next position, purges the
// changes older than ChangeRetention and wakes the record's held polls
// when it commits. ErrClusterNotFound where the organization holds no
// record under g.
type DeclarationStore interface {
	// Declare places object on g under id: a new declaration at
	// generation 1, or the next generation of the live one id names on
	// g, answering whether it was new. An id placed on another record
	// is ErrDeclarationTaken, a tombstoned one ErrDeclarationTombstoned,
	// an object another live declaration of g names
	// ErrDeclaredObjectTaken, and one g's latest report shows held by a
	// cluster-owned object ErrDeclaredObjectHeld.
	Declare(ctx context.Context, g grn.GRN, id string, object DeclaredObject) (Declaration, bool, error)
	// Tombstone deletes g's live declaration id by a change at its next
	// generation, or answers ErrDeclarationNotFound.
	Tombstone(ctx context.Context, g grn.GRN, id string) (Declaration, error)
	// GetDeclaration answers g's live declaration id, or
	// ErrDeclarationNotFound.
	GetDeclaration(ctx context.Context, g grn.GRN, id string) (Declaration, error)
	// ListDeclarations answers g's live declarations, in the order each
	// was last written.
	ListDeclarations(ctx context.Context, g grn.GRN) ([]Declaration, error)
	// DeclarationPage answers the page after after, read in one
	// snapshot of g's position, log and declarations, of at most size
	// changes.
	DeclarationPage(ctx context.Context, g grn.GRN, after *string, size int) (Page, error)
}
