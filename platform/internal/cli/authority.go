package cli

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	operatorpg "github.com/garamsh/garam/internal/operator/postgres"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// NewAuthorityService composes delegated operation authority (ADR-0078)
// over the database and the policy engine: grants and authorities in
// Postgres, a user's authority decided from current state, and where an
// agent runs read from its assignment. Both listeners serve it, so it
// is composed here once.
func NewAuthorityService(pool *pgxpool.Pool, engine grpc.ClientConnInterface) operator.AuthorityService {
	delegations := operatorpg.New(pool)
	return operator.NewAuthorityService(delegations, delegations, operatorpg.New(pool),
		agentPlacements{store: agentpg.New(pool)},
		currentAuthorization{orgs: orgpg.New(pool), users: userpg.New(pool),
			engine: authzv1.NewAuthzServiceClient(engine)},
		operator.Config{})
}

// currentAuthorization decides a user's authority from current state
// (operator.Authorizer): the membership, the roles stored on the user
// and the groups it belongs to, read for this call, and the
// organization's persisted policies read past the engine's cache by
// CheckCurrent. An engine that does not serve CheckCurrent answers
// UNIMPLEMENTED, which refuses rather than falling back to Check.
//
// The roles grant what they grant everywhere: on the organization and
// on everything it owns (authz.md §Statement collection). Neither
// operation an authority is minted for targets a group or a project, so
// no scoped role is collected.
type currentAuthorization struct {
	orgs   org.Store
	users  user.Store
	engine authzv1.AuthzServiceClient
}

func (a currentAuthorization) Authorize(ctx context.Context, actor, orgGRN, object grn.GRN, action string) error {
	m, err := a.orgs.GetMember(ctx, orgGRN, actor)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return errorx.New(errorx.KindPermissionDenied, "not a member of this org")
	}
	if err != nil {
		return err
	}
	u, err := a.users.Get(ctx, actor)
	if err != nil {
		return err
	}
	groups, err := a.orgs.ListGroupsByMember(ctx, orgGRN, actor)
	if err != nil {
		return err
	}
	resp, err := a.engine.CheckCurrent(ctx, &authzv1.CheckCurrentRequest{
		SubjectGrn: actor.String(),
		ObjectGrn:  object.String(),
		Action:     action,
		Statements: []*authzv1.Statement{{
			Sid:      "member-roles",
			Effect:   authzv1.Effect_EFFECT_ALLOW,
			Action:   org.ActionsFor(append([]string{string(m.Role)}, u.Roles...)),
			Resource: []string{orgGRN.String(), "grn:" + orgGRN.Identifier + ":*"},
		}},
		SubjectGroups: wire.GRNsToProto(groups),
		SubjectOrg:    orgGRN.String(),
	})
	if err != nil {
		return wire.ErrorOf(err)
	}
	if !resp.GetAllowed() {
		return errorx.New(errorx.KindPermissionDenied, "insufficient role")
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
