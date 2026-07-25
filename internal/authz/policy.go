package authz

import "context"

// Decision is the outcome of evaluating a statement set.
type Decision int

const (
	DecisionDeny Decision = iota
	DecisionAllow
)

// Context is the input bundle for [Evaluator].
type Context struct {
	Subject Subject
	Object  Object
	Action  string
	Env     map[string]string
}

// Evaluator evaluates the statements applicable to a request and
// yields the final Decision (ADR-0002).
type Evaluator interface {
	Evaluate(ctx context.Context, c Context) Decision
}
