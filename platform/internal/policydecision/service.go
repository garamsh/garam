// Package policydecision asks the policy service for a decision
// (ADR-0146). Nothing in serve api or serve machine calls it yet.
package policydecision

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// Question is one thing the platform asks: may Subject perform Action
// on Resource. Probe states that the caller is deciding what to show,
// so a denial is not recorded as an attempt (ADR-0148 item 2).
type Question struct {
	Subject  grn.GRN
	Action   string
	Resource grn.GRN
	Probe    bool
}

// Reason is one ground of a decision. PolicyID is empty when the
// service refused the question itself, and Error is empty unless a
// policy failed to evaluate or the question was refused.
type Reason struct {
	PolicyID string
	Error    string
}

// Reasons is a set: the service states no order.
type Reasons map[Reason]struct{}

// Decision is the service's answer to one question. A Decision that
// accompanies an error is the zero value, which denies.
type Decision struct {
	Allowed bool
	Reasons Reasons
}

// Service asks the policy service for decisions. Evaluations answers
// one Decision per question, in order. Both fail closed: an error
// comes with a denying Decision, and a caller treats it as a denial.
type Service interface {
	Evaluate(ctx context.Context, q Question) (Decision, error)
	Evaluations(ctx context.Context, qs []Question) ([]Decision, error)
}
