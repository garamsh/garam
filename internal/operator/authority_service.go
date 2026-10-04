package operator

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// actionDelegate is the action granting and withdrawing a delegation
// needs. internal/org owns it; see Operation for why it is repeated.
const actionDelegate = "operator:delegate"

// agentServiceType is the GRN service type of an agent, which a
// configuration change targets. internal/agent owns it.
const agentServiceType = "agent"

// AuthorityService is what delegated operation authority goes through
// on both listeners (ADR-0078): an administrator's grant to a hosted
// operator, a member's request-bound handoff to it, and the operator's
// reading of that handoff back.
//
// Every privileged call decides from current state: the grant and the
// registrations it names are read when the call is made, the actor's
// authority through Authorizer, and a configuration change's placement
// through AssignmentReader. A call that cannot read any of them
// refuses.
type AuthorityService interface {
	// WriteGrant creates or replaces the hosted operator's grant for an
	// actor currently holding operator:delegate; see GrantStore.WriteGrant
	// for replay and staleness.
	WriteGrant(ctx context.Context, w GrantWrite) (*Grant, bool, error)
	// EndGrant withdraws the named grant for an actor currently holding
	// operator:delegate; see GrantStore.EndGrant for a retry.
	EndGrant(ctx context.Context, actor, org, operator grn.GRN, id GrantID) error
	// CurrentGrant answers the hosted operator's current grant.
	CurrentGrant(ctx context.Context, org, operator grn.GRN) (*Grant, error)
	// Mint hands the audience one operation on the actor's behalf.
	Mint(ctx context.Context, r MintRequest) (*Minted, error)
	// Introspect answers what an authority binds, to its audience only,
	// once current state still authorizes it. It consumes nothing.
	Introspect(ctx context.Context, caller grn.GRN, authority string) (Introspected, error)
	// Reference rechecks a durable operation reference for its audience,
	// as an Operator does before activating a new desired revision. It
	// is not a restart capability for a revision already applied.
	Reference(ctx context.Context, caller grn.GRN, ref OperationRef) (Binding, error)
}

type authorityService struct {
	grants      GrantStore
	authorities AuthorityStore
	operators   Store
	assignments AssignmentReader
	authorizer  Authorizer
	now         func() time.Time
}

// NewAuthorityService constructs the service over the grants and
// authorities it keeps, the operator records it reads registrations
// from, where agents run, and what decides a user's current authority.
func NewAuthorityService(grants GrantStore, authorities AuthorityStore, operators Store, assignments AssignmentReader, authorizer Authorizer, cfg Config) AuthorityService {
	clock := cfg.Now
	if clock == nil {
		clock = time.Now
	}
	// Every time this service writes is kept at the database's
	// microsecond precision, so what a call answers is what a later read
	// of the same row answers.
	now := func() time.Time { return clock().UTC().Truncate(time.Microsecond) }
	return &authorityService{grants: grants, authorities: authorities, operators: operators,
		assignments: assignments, authorizer: authorizer, now: now}
}

func (s *authorityService) WriteGrant(ctx context.Context, w GrantWrite) (*Grant, bool, error) {
	if err := s.authorize(ctx, w.Actor, w.Org, w.Org, actionDelegate); err != nil {
		return nil, false, err
	}
	if err := s.registered(ctx, w.Org, w.Operator); err != nil {
		return nil, false, err
	}
	w.Now = s.now()
	return s.grants.WriteGrant(ctx, w, GrantID(uuid.NewString()))
}

func (s *authorityService) EndGrant(ctx context.Context, actor, org, operator grn.GRN, id GrantID) error {
	if err := s.authorize(ctx, actor, org, org, actionDelegate); err != nil {
		return err
	}
	return s.grants.EndGrant(ctx, org, operator, id, s.now())
}

func (s *authorityService) CurrentGrant(ctx context.Context, org, operator grn.GRN) (*Grant, error) {
	return s.grants.CurrentGrant(ctx, org, operator)
}

func (s *authorityService) Mint(ctx context.Context, r MintRequest) (*Minted, error) {
	if !r.Operation.IsMintable() {
		return nil, ErrOperationNotMintable
	}
	if err := validateOwnership(r.Audience, r.Org); err != nil {
		return nil, err
	}
	grant, err := s.grants.CurrentGrant(ctx, r.Org, r.Audience)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return nil, ErrNotDelegated
	}
	if err != nil {
		return nil, err
	}
	b := Binding{
		Ref: OperationRef(uuid.NewString()), Grant: grant.ID, Org: r.Org, Actor: r.Actor,
		Audience: r.Audience, Operation: r.Operation, Target: r.Target,
		RequestID: r.RequestID, Digest: r.Digest,
	}
	controller, object, err := s.placement(ctx, &b)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if !grant.Covers(b.Operation, controller, now) {
		return nil, ErrNotDelegated
	}
	if err := s.authorize(ctx, r.Actor, r.Org, object, string(r.Operation)); err != nil {
		return nil, err
	}
	token, err := newAuthorityToken()
	if err != nil {
		return nil, err
	}
	expiresAt := now.Add(AuthorityTTL)
	if grant.ExpiresAt.Before(expiresAt) {
		expiresAt = grant.ExpiresAt
	}
	ref, err := s.authorities.MintAuthority(ctx, b, HashToken(token), expiresAt, now)
	if err != nil {
		return nil, err
	}
	return &Minted{Authority: token, ExpiresAt: expiresAt, Ref: ref, Grant: grant.ID}, nil
}

// placement reads what b acts on. A creation targets the controller
// that will run the new agent, and the actor's authority is decided on
// the organization, as defining an agent is. A configuration change or
// a recovery targets the agent, records where it runs now, and is
// decided on the agent.
func (s *authorityService) placement(ctx context.Context, b *Binding) (controller, object grn.GRN, err error) {
	switch {
	case b.Operation == OperationAgentCreate:
		if err := validateOwnership(b.Target, b.Org); err != nil {
			return grn.GRN{}, grn.GRN{}, err
		}
		return b.Target, b.Org, nil
	case b.Operation.TargetsAgent():
		if b.Target.ServiceType != agentServiceType || b.Target.Organization != b.Org.Identifier {
			return grn.GRN{}, grn.GRN{}, errorx.New(errorx.KindInvalidArgument, "target must be an agent of this organization")
		}
		a, err := s.assignments.CurrentAssignment(ctx, b.Target)
		if err != nil {
			return grn.GRN{}, grn.GRN{}, err
		}
		b.Assignment = &a
		return a.Operator, b.Target, nil
	}
	return grn.GRN{}, grn.GRN{}, ErrOperationNotMintable
}

func (s *authorityService) Introspect(ctx context.Context, caller grn.GRN, authority string) (Introspected, error) {
	in, err := s.authorities.AuthorityByToken(ctx, HashToken(authority))
	if err != nil {
		return Introspected{}, err
	}
	if in.Audience != caller || !s.now().Before(in.ExpiresAt) {
		return Introspected{}, ErrAuthorityNotFound
	}
	if err := s.recheck(ctx, in.Binding); err != nil {
		return Introspected{}, err
	}
	return in, nil
}

func (s *authorityService) Reference(ctx context.Context, caller grn.GRN, ref OperationRef) (Binding, error) {
	b, err := s.authorities.AuthorityByRef(ctx, ref)
	if err != nil {
		return Binding{}, err
	}
	if b.Audience != caller {
		return Binding{}, ErrAuthorityNotFound
	}
	if err := s.recheck(ctx, b); err != nil {
		return Binding{}, err
	}
	return b, nil
}

// recheck decides a stored binding against current state: its own grant
// still current and covering it, the bound controller still registered,
// the placement of an operation on an agent unchanged, and the actor
// still authorized. The audience's registration needs no read of its
// own: its deletion removes the binding by cascade.
func (s *authorityService) recheck(ctx context.Context, b Binding) error {
	grant, err := s.grants.GetGrant(ctx, b.Grant)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return ErrAuthorityRevoked
	}
	if err != nil {
		return err
	}
	controller, object := b.Target, b.Org
	if b.Operation.TargetsAgent() {
		current, err := s.assignments.CurrentAssignment(ctx, b.Target)
		if errorx.IsKind(err, errorx.KindNotFound) {
			return ErrAuthorityRevoked
		}
		if err != nil {
			return err
		}
		if b.Assignment == nil || current != *b.Assignment {
			return ErrAuthorityRevoked
		}
		controller, object = current.Operator, b.Target
	}
	if !grant.Covers(b.Operation, controller, s.now()) {
		return ErrAuthorityRevoked
	}
	if err := s.authorize(ctx, b.Actor, b.Org, object, string(b.Operation)); err != nil {
		if errorx.IsKind(err, errorx.KindPermissionDenied) {
			return ErrAuthorityRevoked
		}
		return err
	}
	return nil
}

// authorize asks Authorizer and refuses on anything but an allow. A
// failure to decide is reported as unavailable whatever its kind, so an
// engine that cannot answer from current state never reads as a defect
// of the request.
func (s *authorityService) authorize(ctx context.Context, actor, org, object grn.GRN, action string) error {
	err := s.authorizer.Authorize(ctx, actor, org, object, action)
	if err == nil || errorx.IsKind(err, errorx.KindPermissionDenied) {
		return err
	}
	return errorx.Wrap(err, "current authorization", errorx.KindUnavailable)
}

// registered refuses an operator the organization does not hold.
func (s *authorityService) registered(ctx context.Context, org, operator grn.GRN) error {
	if err := validateOwnership(operator, org); err != nil {
		return err
	}
	_, err := s.operators.Get(ctx, operator)
	return err
}
