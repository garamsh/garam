package machineapi

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
)

// Statement is one IAM-style rule handed to the authz engine for
// evaluation. Action and Resource entries are wildcard patterns.
type Statement struct {
	Sid      string
	Effect   wire.Effect
	Action   []string
	Resource []string
}

// EvalInput is one authorization question: may Subject perform Action
// on Object, given the Statements this listener collected for it.
// SubjectGroups names the groups Subject belongs to, so the engine can
// find the policies attached to them (ADR-0008). SubjectOrg is the GRN
// of the organization the subject belongs to when the subject is an
// org member; the listener populates it from the sender's own GRN,
// and the engine refuses the call when it is unset (ADR-0038).
type EvalInput struct {
	Subject       grn.GRN
	Object        grn.GRN
	Action        string
	SubjectGroups []grn.GRN
	Statements    []Statement
	SubjectOrg    grn.GRN
}

// Evaluator is the authorization seam between the machine listener and
// the authz engine (ADR-0003). The gRPC client implements it; this
// package never imports the engine.
type Evaluator interface {
	Evaluate(ctx context.Context, in EvalInput) (bool, error)
}
