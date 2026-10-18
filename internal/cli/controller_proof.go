package cli

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	operatorpg "github.com/garamsh/garam/internal/operator/postgres"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// newProofService composes the proof of a controller's current
// authority (ADR-0078) over the database and the policy engine: grants
// in Postgres, where an agent runs read from its assignment, and the
// hosted operator's current policy decided by CheckCurrent.
func newProofService(pool *pgxpool.Pool, engine grpc.ClientConnInterface, now func() time.Time) operator.ProofService {
	return operator.NewProofService(operatorpg.New(pool), agentPlacements{store: agent.NewPostgresStore(pool)},
		audienceAuthorization{groups: memberGroups{store: orgpg.New(pool)},
			engine: authzv1.NewAuthzServiceClient(engine)},
		operator.Config{Now: now})
}

// audienceAuthorization decides a hosted operator's current policy
// (operator.AudienceAuthorizer): the groups it belongs to, read for
// this call, and the organization's persisted policies read past the
// engine's cache by CheckCurrent, beside one Allow of exactly the
// action on the object, which is what the operator's grant contributes.
// A Deny attached to the operator or to one of its groups wins over
// that Allow. An engine that does not serve CheckCurrent answers
// UNIMPLEMENTED, which refuses rather than falling back to Check.
type audienceAuthorization struct {
	groups memberGroups
	engine authzv1.AuthzServiceClient
}

func (a audienceAuthorization) Authorize(ctx context.Context, audience, orgGRN, object grn.GRN, action string) error {
	groups, err := a.groups.List(ctx, audience)
	if err != nil {
		return err
	}
	resp, err := a.engine.CheckCurrent(ctx, &authzv1.CheckCurrentRequest{
		SubjectGrn: audience.String(),
		ObjectGrn:  object.String(),
		Action:     action,
		Statements: []*authzv1.Statement{{
			Sid:      "delegation",
			Effect:   authzv1.Effect_EFFECT_ALLOW,
			Action:   []string{action},
			Resource: []string{object.String()},
		}},
		SubjectGroups: wire.GRNsToProto(groups),
		SubjectOrg:    orgGRN.String(),
	})
	if err != nil {
		return wire.ErrorOf(err)
	}
	if !resp.GetAllowed() {
		return errorx.New(errorx.KindPermissionDenied, "current policy denies "+action)
	}
	return nil
}
