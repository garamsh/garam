package identity

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// Admitter is what [Principals] admits a verified assertion through.
type Admitter interface {
	// Admit admits a, an assertion of principal that verified, in one
	// transaction on the database's clock: the principal's record must
	// be live, the assertion timely, and its jti unspent. It spends the
	// jti where every replica reads it, so a second replica refuses a
	// replay. Anything else is [operator.ErrClientInvalid]; an error of
	// another kind is a store that could not be read.
	Admit(ctx context.Context, principal grn.GRN, a operator.VerifiedAssertion) error
}
