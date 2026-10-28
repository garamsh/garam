package cli

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/garamsh/garam/platform/internal/agent"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/operator"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// NewStandaloneService composes how an administrator approves,
// withdraws and revokes a standalone host (ADR-0082) over the database
// and the policy engine, which decides the administrator's authority
// from current state. The browser-facing listener serves it, and it
// signs nothing.
func NewStandaloneService(pool *pgxpool.Pool, engine grpc.ClientConnInterface) agent.StandaloneService {
	return agent.NewStandaloneService(agentpg.New(pool), memberAuthorization(pool, engine), nil)
}

// newSupervisorService composes how a standalone host's supervisor
// enrolls and exchanges its credential over the database and the policy
// engine, which decides the approving administrator's authority again;
// serverRoot is answered beside the credential.
func newSupervisorService(pool *pgxpool.Pool, engine grpc.ClientConnInterface, serverRoot []byte, now func() time.Time) agent.SupervisorService {
	store := agentpg.New(pool)
	return agent.NewSupervisorService(store, store, memberAuthorization(pool, engine), serverRoot, now)
}

// memberAuthorization is a user's authority decided from current
// state, as delegated operation authority decides it.
func memberAuthorization(pool *pgxpool.Pool, engine grpc.ClientConnInterface) currentAuthorization {
	return currentAuthorization{orgs: orgpg.New(pool), users: userpg.New(pool),
		engine: authzv1.NewAuthzServiceClient(engine)}
}

// identityRegistrations reads whether an organization still holds the
// record an identity authenticates on (machine.Registration): an
// operator's record (ADR-0070), or a supervisor's current
// registration, which a revocation or a replacement ends (ADR-0082).
// An agent is admitted by the assignment its holder made and is
// answered without a read.
type identityRegistrations struct {
	operators   operator.Store
	supervisors agent.StandaloneStore
}

func (r identityRegistrations) Holds(ctx context.Context, subject grn.GRN) error {
	switch subject.ServiceType {
	case operator.ServiceType:
		// Store.Get answers the record's presence and joins the two
		// tables the enrollment state is derived from, which this has
		// no question about. It is read through rather than narrowed
		// because a second way to ask whether an organization holds an
		// operator is a second answer to keep agreeing with the first.
		_, err := r.operators.Get(ctx, subject)
		return err
	case agent.ServiceTypeSupervisor:
		_, err := r.supervisors.GetSupervisor(ctx, subject)
		return err
	}
	return nil
}
