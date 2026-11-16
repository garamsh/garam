package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// answering is a policy service that gives one answer and records the
// question it was asked.
type answering struct {
	decision policydecision.Decision
	err      error
	asked    []policydecision.Question
}

func (a *answering) Evaluate(_ context.Context, q policydecision.Question) (policydecision.Decision, error) {
	a.asked = append(a.asked, q)
	return a.decision, a.err
}

func (a *answering) Evaluations(context.Context, []policydecision.Question) ([]policydecision.Decision, error) {
	panic("currentDecision asks one question")
}

// TestCurrentDecision_AsksTheServiceReadCurrentAndNothingElse pins the
// call to the actor, the action and the object, read current: no
// statement, group or organization is supplied, so what the service
// holds is all that decides.
func TestCurrentDecision_AsksTheServiceReadCurrentAndNothingElse(t *testing.T) {
	svc := &answering{decision: policydecision.Decision{Allowed: true}}
	ada, acme := grn.New("acme", "user", "ada"), grn.New("acme", "project", "p")

	require.NoError(t, currentDecision{decisions: svc}.Authorize(
		context.Background(), ada, grn.New("acme", "org", "acme"), acme, "iam:operator.read"))

	require.Len(t, svc.asked, 1)
	assert.Equal(t, policydecision.Question{
		Subject: ada, Action: "iam:operator.read", Resource: acme, Current: true,
	}, svc.asked[0])
}

// TestCurrentDecision_FailsClosed shows a denial is permission denied
// and any service failure unavailable, never an allowance. The failure
// carries a permission-denied kind of its own, which must not be read
// as the service's denial.
func TestCurrentDecision_FailsClosed(t *testing.T) {
	ada, acme := grn.New("acme", "user", "ada"), grn.New("acme", "project", "p")
	tests := []struct {
		name string
		svc  *answering
		kind errorx.Kind
	}{
		{"a denial", &answering{}, errorx.KindPermissionDenied},
		{"an unreachable service", &answering{err: errorx.New(errorx.KindUnavailable, "down")}, errorx.KindUnavailable},
		{"a refusing service", &answering{err: errorx.New(errorx.KindPermissionDenied, "refused")}, errorx.KindUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := currentDecision{decisions: tc.svc}.Authorize(
				context.Background(), ada, grn.New("acme", "org", "acme"), acme, "iam:operator.read")
			require.Error(t, err)
			assert.Equal(t, tc.kind, errorx.KindOf(err))
		})
	}
}
