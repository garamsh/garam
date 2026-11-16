package cli

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/agent"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// NewAuthorityService composes delegated operation authority (ADR-0078)
// over the database and the policy service: grants and authorities in
// Postgres, a user's authority decided by the service from current
// state, and where an agent runs read from its assignment. Both
// listeners serve it, so it is composed here once.
func NewAuthorityService(pool *pgxpool.Pool, decisions policydecision.Service) operator.AuthorityService {
	delegations := operatorpg.New(pool)
	return operator.NewAuthorityService(delegations, delegations, operatorpg.New(pool),
		agentPlacements{store: agentpg.New(pool)},
		currentDecision{decisions: decisions},
		operator.Config{})
}

// currentDecision is the policy service's decision, read current
// (ADR-0146 items 12, 14 and 15): it satisfies operator.Authorizer for
// a user's authority and operator.AudienceAuthorizer for a hosted
// operator's. The call carries the subject, the action and the object
// and nothing else: the roles, the groups and a hosted operator's
// delegated operations are the service's own state, and the
// organization is the one the service reads from the subject.
//
// A context marked policydecision.AsProbe asks as a probe, which is how
// the console's permission check decides what to show without recording
// a denial (ADR-0148 item 2); an action does not.
//
// An error from the service is unavailable, never a denial and never an
// allow, whatever kind it arrived with.
type currentDecision struct{ decisions policydecision.Service }

// errNoCurrentPolicy is what a decision answers when no policy service
// is configured: unavailable, as a send's is.
var errNoCurrentPolicy = errorx.New(errorx.KindUnavailable, "no policy service is configured to decide this")

func (d currentDecision) Authorize(ctx context.Context, subject, _, object grn.GRN, action string) error {
	if d.decisions == nil {
		return errNoCurrentPolicy
	}
	decision, err := d.decisions.Evaluate(ctx, policydecision.Question{
		Subject: subject, Action: action, Resource: object, Current: true,
		Probe: policydecision.IsProbe(ctx),
	})
	if err != nil {
		return errorx.Wrap(err, "policy decision", errorx.KindUnavailable)
	}
	if !decision.Allowed {
		return errorx.New(errorx.KindPermissionDenied, "current policy denies "+action)
	}
	return nil
}

// agentPlacements answers where an agent runs
// (operator.AssignmentReader) from its assignment. The epoch is handed
// on as an opaque string.
type agentPlacements struct{ store agent.Store }

func (p agentPlacements) CurrentAssignment(ctx context.Context, g grn.GRN) (operator.Assignment, error) {
	a, err := p.store.GetAssignment(ctx, g)
	if err != nil {
		return operator.Assignment{}, err
	}
	return operator.Assignment{Assignee: a.Assignee, Epoch: strconv.Itoa(a.Epoch)}, nil
}
