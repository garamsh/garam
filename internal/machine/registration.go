package machine

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Registration is the organization's record of a machine identity, as
// a decision that admits it reads it: withdrawing the record is what
// stops the identity acting (ADR-0070).
type Registration interface {
	// Holds returns nil where the organization still holds a record of
	// subject, and a KindNotFound error where it holds none.
	//
	// An identity garam records no registration for, an agent, is held
	// rather than refused: an agent is admitted by its assignment and
	// its token's lineage, which its token's read already joined.
	Holds(ctx context.Context, subject grn.GRN) error
}
