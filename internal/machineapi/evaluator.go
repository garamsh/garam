package machineapi

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Effect is a statement's outcome when it matches (ADR-0002). It
// mirrors the wire model; the engine owns the semantics.
type Effect string

const (
	EffectAllow Effect = "Allow"
	EffectDeny  Effect = "Deny"
)

// Statement is one IAM-style rule handed to the authz engine for
// evaluation. Action and Resource entries are wildcard patterns.
type Statement struct {
	Sid      string
	Effect   Effect
	Action   []string
	Resource []string
}

// EvalInput is one authorization question: may Subject perform Action
// on Object, given the Statements this listener collected for it.
//
// It carries no subject groups. A machine identity's group membership
// is org data and this listener holds no org store, so a policy
// attached to a group reaches nothing here (ADR-0008).
type EvalInput struct {
	Subject    grn.GRN
	Object     grn.GRN
	Action     string
	Statements []Statement
}

// Evaluator is the authorization seam between the machine listener and
// the authz engine (ADR-0003). The gRPC client implements it; this
// package never imports the engine.
type Evaluator interface {
	Evaluate(ctx context.Context, in EvalInput) (bool, error)
}
