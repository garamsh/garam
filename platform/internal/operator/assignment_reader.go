package operator

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// AssignmentReader answers where an agent currently runs. The agents
// domain owns assignments; this package reads them through this seam.
// It answers a not-found error for an agent that does not exist or is
// not assigned.
type AssignmentReader interface {
	CurrentAssignment(ctx context.Context, agent grn.GRN) (Assignment, error)
}
