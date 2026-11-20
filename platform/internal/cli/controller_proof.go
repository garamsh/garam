package cli

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// newProofService composes the proof of a controller's current
// authority (ADR-0078) over the database and the policy service: grants
// in Postgres, where an agent runs read from its assignment, and the
// hosted operator's current policy decided by the service, read
// current.
func newProofService(pool *pgxpool.Pool, decisions policydecision.Service, now func() time.Time) operator.ProofService {
	return operator.NewProofService(operatorpg.New(pool), agentPlacements{store: agentpg.New(pool)},
		currentDecision{decisions: decisions},
		operator.Config{Now: now})
}
