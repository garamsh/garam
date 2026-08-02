package api

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
// on Object, given the Statements the caller collected for it.
// SubjectGroups names the groups Subject belongs to, so the engine can
// find the policies attached to them (ADR-0008).
type EvalInput struct {
	Subject       grn.GRN
	Object        grn.GRN
	Action        string
	SubjectGroups []grn.GRN
	Statements    []Statement
}

// Evaluator is the authorization seam between the api server and the
// authz engine (ADR-0003). The gRPC client implements it; the api
// package never imports the engine.
type Evaluator interface {
	Evaluate(ctx context.Context, in EvalInput) (bool, error)
}
