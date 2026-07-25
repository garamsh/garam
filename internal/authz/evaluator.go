package authz

import "context"

// StatementEvaluator is the native, in-process Evaluator (ADR-0002).
// It evaluates a fixed statement set with IAM semantics: an explicit
// Deny wins, otherwise any Allow permits, and the default is Deny.
type StatementEvaluator struct {
	statements []Statement
}

// Compile-time check that StatementEvaluator satisfies Evaluator.
var _ Evaluator = (*StatementEvaluator)(nil)

// NewStatementEvaluator constructs an Evaluator over the given
// statements. The set applies to whatever subject the caller built it
// for; matching considers only action and resource.
func NewStatementEvaluator(statements []Statement) *StatementEvaluator {
	return &StatementEvaluator{statements: statements}
}

// Evaluate returns DecisionAllow when a matching Allow statement
// exists and no matching Deny statement does.
func (e *StatementEvaluator) Evaluate(_ context.Context, c Context) Decision {
	decision := DecisionDeny
	for _, s := range e.statements {
		if !s.Matches(c.Action, c.Object.GRN().String()) {
			continue
		}
		if s.Effect == EffectDeny {
			return DecisionDeny
		}
		decision = DecisionAllow
	}
	return decision
}
