package cli

import (
	"context"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	operatorpg "github.com/garamsh/garam/internal/operator/postgres"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// newExecutionService composes what a lifecycle authority does to an
// agent's execution (ADR-0078) over the database, the policy engine and
// the delegated operation authority.
func newExecutionService(pool *pgxpool.Pool, engine grpc.ClientConnInterface, authority operator.AuthorityService,
	now func() time.Time,
) agent.ExecutionService {
	if now == nil {
		now = time.Now
	}
	agents := agent.NewPostgresStore(pool)
	return agent.NewExecutionService(agents, agentpg.New(pool),
		lifecycleAuthorization{
			grants:      operatorpg.New(pool),
			supervisors: agentpg.New(pool),
			policy: audienceAuthorization{groups: memberGroups{store: orgpg.New(pool)},
				engine: authzv1.NewAuthzServiceClient(engine)},
			now: now,
		},
		operationAuthorities{service: authority},
		agents)
}

// newAdmissionService composes agent-enrollment.v1's approval and
// admission (ADR-0102) over the database and the delegated operation
// authority that reads a member's reference.
func newAdmissionService(pool *pgxpool.Pool, authority operator.AuthorityService, now func() time.Time) agent.AdmissionService {
	return agent.NewAdmissionService(agent.NewPostgresStore(pool), agentpg.New(pool), operationAuthorities{service: authority}, now)
}

// lifecycleAuthorization decides lifecycle authority over one agent
// from current state (agent.LifecycleAuthorizer). A hosted operator
// holds it by its current grant in the agent's organization, which
// must cover the operation for the agent's assignee; a supervisor by
// its current registration for this agent, as that agent's assignee,
// for agent:activate alone (ADR-0082). For either, current policy on
// the agent is read too and an explicit Deny wins. The caller's
// registration is rechecked on the listener before this is asked.
type lifecycleAuthorization struct {
	grants      operator.GrantStore
	supervisors agent.StandaloneStore
	policy      audienceAuthorization
	now         func() time.Time
}

func (a lifecycleAuthorization) Authorize(ctx context.Context, caller, agentGRN, assignee grn.GRN, op agent.LifecycleOperation) (agent.Grant, error) {
	if caller.Organization != agentGRN.Organization {
		return agent.Grant{}, errorx.New(errorx.KindPermissionDenied, "the caller holds no lifecycle authority over this agent")
	}
	orgGRN := grn.Org(agentGRN.Organization)
	var (
		grant agent.Grant
		err   error
	)
	switch caller.ServiceType {
	case operator.ServiceType:
		grant, err = a.delegated(ctx, orgGRN, caller, assignee, op)
	case agent.ServiceTypeSupervisor:
		grant, err = a.supervising(ctx, caller, agentGRN, assignee, op)
	default:
		err = errorx.New(errorx.KindPermissionDenied, "the caller holds no lifecycle authority over this agent")
	}
	if err != nil {
		return agent.Grant{}, err
	}
	err = a.policy.Authorize(ctx, caller, orgGRN, agentGRN, string(op))
	switch {
	case err == nil:
		return grant, nil
	case errorx.IsKind(err, errorx.KindPermissionDenied):
		return agent.Grant{}, err
	}
	return agent.Grant{}, errorx.Wrap(err, "current policy", errorx.KindUnavailable)
}

// supervising answers a supervisor's authority: its registration, read
// now, names this agent and the supervisor holds the agent's
// assignment. It covers activation alone; recovery is an
// administrator's new approval. The authority's identifier is the
// approval that registered it, so a credential re-issue changes nothing
// an activation retry is matched by. It holds while the registration
// does, which the store reads again under the execution lock, so it is
// bounded by the decision's own deadline.
func (a lifecycleAuthorization) supervising(ctx context.Context, caller, agentGRN, assignee grn.GRN, op agent.LifecycleOperation) (agent.Grant, error) {
	if op != agent.OperationActivate {
		return agent.Grant{}, errorx.New(errorx.KindPermissionDenied, "a supervisor's authority does not cover "+string(op))
	}
	registered, err := a.supervisors.GetSupervisor(ctx, caller)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return agent.Grant{}, errorx.New(errorx.KindPermissionDenied, "the supervisor's registration is not current")
	}
	if err != nil {
		return agent.Grant{}, errorx.Wrap(err, "current registration", errorx.KindUnavailable)
	}
	if registered.Agent != agentGRN || assignee != caller {
		return agent.Grant{}, errorx.New(errorx.KindPermissionDenied, "the supervisor does not hold this agent")
	}
	return agent.Grant{ID: "supervisor:" + registered.Enrollment.String(),
		ExpiresAt: a.now().Add(agent.LifecycleDeadline)}, nil
}

// delegated answers a hosted operator's authority: its current grant,
// covering op for the agent's assignee now.
func (a lifecycleAuthorization) delegated(ctx context.Context, orgGRN, caller, assignee grn.GRN, op agent.LifecycleOperation) (agent.Grant, error) {
	grant, err := a.grants.CurrentGrant(ctx, orgGRN, caller)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return agent.Grant{}, errorx.New(errorx.KindPermissionDenied, "the caller holds no current delegation")
	}
	if err != nil {
		return agent.Grant{}, errorx.Wrap(err, "current delegation", errorx.KindUnavailable)
	}
	if !grant.Covers(operator.Operation(op), assignee, a.now()) {
		return agent.Grant{}, errorx.New(errorx.KindPermissionDenied, "the delegation does not cover "+string(op)+" for the agent's assignee")
	}
	return agent.Grant{ID: string(grant.ID), ExpiresAt: grant.ExpiresAt}, nil
}

// operationAuthorities reads a delegated operation authority back for
// the execution service (agent.OperationResolver), rechecked from
// current state by the operator domain.
type operationAuthorities struct{ service operator.AuthorityService }

func (o operationAuthorities) ResolveReference(ctx context.Context, audience grn.GRN, ref string) (*agent.OperationBinding, error) {
	b, err := o.service.Reference(ctx, audience, operator.OperationRef(ref))
	if err != nil {
		return nil, err
	}
	return bindingOf(b, time.Time{})
}

func (o operationAuthorities) ResolveHandoff(ctx context.Context, audience grn.GRN, authority string) (*agent.OperationBinding, error) {
	in, err := o.service.Introspect(ctx, audience, authority)
	if err != nil {
		return nil, err
	}
	return bindingOf(in.Binding, in.ExpiresAt)
}

// bindingOf renders an operator binding as the agent domain reads it.
// An assignment epoch garam did not write as a decimal binds no
// assignment this agent can be on.
func bindingOf(b operator.Binding, expiresAt time.Time) (*agent.OperationBinding, error) {
	out := &agent.OperationBinding{
		Ref: string(b.Ref), Kind: agent.OperationKind(b.Operation), Org: b.Org,
		Audience: b.Audience, Target: b.Target, RequestID: string(b.RequestID), ExpiresAt: expiresAt,
	}
	digest, err := hex.DecodeString(string(b.Digest))
	if err != nil || len(digest) != len(out.BodySHA256) {
		return nil, errorx.New(errorx.KindInternal, "stored body digest is not SHA-256")
	}
	copy(out.BodySHA256[:], digest)
	if b.RequestTarget != nil {
		out.RequestTarget = string(*b.RequestTarget)
	}
	if b.Destination != nil {
		out.Destination = *b.Destination
	}
	if b.Assignment != nil {
		epoch, err := strconv.Atoi(b.Assignment.Epoch)
		if err != nil {
			return nil, errorx.Wrap(err, "bound assignment epoch", errorx.KindInternal)
		}
		out.Assignee, out.Epoch = b.Assignment.Assignee, epoch
	}
	return out, nil
}
