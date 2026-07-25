package authz

import (
	"context"

	"github.com/open-policy-agent/opa/v1/rego"

	"github.com/garamsh/gaon/internal/errorx"
)

// RegoEvaluator compiles a Rego policy bundle once and evaluates
// `data.gaon.authz.allow` for each Context.
type RegoEvaluator struct {
	prepared rego.PreparedEvalQuery
}

// NewRegoEvaluator compiles the given Rego source. The module must
// declare `package gaon.authz` and define `allow` (boolean) or a
// rule that produces `allow`. See internal/authz/policies/authz.rego
// for the canonical input shape.
func NewRegoEvaluator(ctx context.Context, source string) (*RegoEvaluator, error) {
	pq, err := rego.New(
		rego.Query("data.gaon.authz.allow"),
		rego.Module("authz.rego", source),
	).PrepareForEval(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "compile rego policy", errorx.KindInvalidArgument)
	}
	return &RegoEvaluator{prepared: pq}, nil
}

// Evaluate runs the prepared query with the Context converted to a
// Rego input document.
func (e *RegoEvaluator) Evaluate(ctx context.Context, c Context) Decision {
	input := contextToInput(c)

	rs, err := e.prepared.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return DecisionDeny
	}
	if rs.Allowed() {
		return DecisionAllow
	}
	return DecisionDeny
}

// contextToInput projects the gaon Context into the Rego `input`
// document. Policy authors write rules over this shape.
func contextToInput(c Context) map[string]any {
	subject := map[string]any{
		"grn":      subjectGRNString(c.Subject),
		"type":     subjectTypeString(c.Subject),
		"roles":    subjectRoles(c.Subject),
		"policies": subjectPolicies(c.Subject),
	}
	object := map[string]any{
		"grn":  objectGRNString(c.Object),
		"type": objectTypeString(c.Object),
	}
	return map[string]any{
		"subject": subject,
		"object":  object,
		"action":  c.Action,
		"env":     stringMap(c.Env),
	}
}

func subjectGRNString(s Subject) string {
	if s == nil {
		return ""
	}
	return s.GRN().String()
}

func subjectTypeString(s Subject) string {
	if s == nil {
		return ""
	}
	return s.Type()
}

func subjectRoles(s Subject) []string {
	if s == nil {
		return nil
	}
	return s.Roles()
}

func subjectPolicies(s Subject) []string {
	if s == nil {
		return nil
	}
	return s.Policies()
}

func objectGRNString(o Object) string {
	if o == nil {
		return ""
	}
	return o.GRN().String()
}

func objectTypeString(o Object) string {
	if o == nil {
		return ""
	}
	return o.Type()
}

func stringMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
