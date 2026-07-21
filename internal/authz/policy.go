package authz

import "context"

// Decision is the outcome of evaluating a single policy or the
// engine as a whole.
type Decision int

const (
	DecisionDeny Decision = iota
	DecisionAllow
)

// Context is the input bundle for [Evaluator] and [Policy].
type Context struct {
	Subject Subject
	Object  Object
	Action  string
	Env     map[string]string
}

// Policy is one named, addressable rule. Implementations are pure
// functions of [Context].
type Policy interface {
	ID() string
	Description() string
	Evaluate(ctx Context) Decision
}

// PolicyStore persists named policies.
type PolicyStore interface {
	Get(ctx context.Context, id string) (Policy, error)
	List(ctx context.Context) ([]Policy, error)
}

// Evaluator combines the policies applicable to a request and yields
// the final Decision. A future iteration may load Rego (OPA) bundles
// behind this interface.
type Evaluator interface {
	Evaluate(ctx context.Context, c Context) Decision
}
