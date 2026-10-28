package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/google/uuid"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
)

// actionEnroll is the action approving, withdrawing and revoking a
// standalone host needs. internal/org owns it; this package does not
// import that one, so the string is repeated.
const actionEnroll = "agent:enroll"

// actionRead is the action listing an organization's approvals and
// supervisors needs, repeated for the same reason.
const actionRead = "agent:read"

// StandaloneService is how an administrator stands a standalone host
// up for one agent and takes it down again (ADR-0082). Every act
// decides the actor's authority from current state. It signs nothing:
// the browser-facing process holds no certificate authority key
// (ADR-0017), and the host's supervisor obtains its certificates
// through SupervisorService on the machine listener.
type StandaloneService interface {
	// Approve records an administrator's approval of a host's request
	// and answers it, created or answered again for an identical retry.
	Approve(ctx context.Context, in ApprovalInput) (*Enrollment, bool, error)
	// Withdraw withdraws an unspent approval.
	Withdraw(ctx context.Context, actor, org grn.GRN, id EnrollmentID) error
	// Revoke ends a supervisor's registration and its agent's current
	// activation.
	Revoke(ctx context.Context, actor, org, supervisor grn.GRN) error
	// ListEnrollments pages the organization's approvals.
	ListEnrollments(ctx context.Context, actor, org grn.GRN, f listfilter.Filter) ([]EnrollmentSummary, error)
	// ListSupervisors pages the organization's supervisor
	// registrations.
	ListSupervisors(ctx context.Context, actor, org grn.GRN, f listfilter.Filter) ([]SupervisorSummary, error)
	// AuthorizeEnroll decides whether the actor currently holds
	// agent:enroll on object: the organization a new host's agent
	// joins, or the agent a replacement, a withdrawal or a revoke acts
	// on. Approve, Withdraw and Revoke ask exactly this.
	AuthorizeEnroll(ctx context.Context, actor, org, object grn.GRN) error
}

type standaloneService struct {
	store      StandaloneStore
	authorizer MemberAuthorizer
	now        func() time.Time
}

// NewStandaloneService constructs the service over the store it keeps
// approvals and registrations in and what decides a user's current
// authority. A nil now takes time.Now.
func NewStandaloneService(store StandaloneStore, authorizer MemberAuthorizer, now func() time.Time) StandaloneService {
	if now == nil {
		now = time.Now
	}
	return &standaloneService{store: store, authorizer: authorizer, now: now}
}

func (s *standaloneService) Approve(ctx context.Context, in ApprovalInput) (*Enrollment, bool, error) {
	now := s.now()
	if !in.ExpiresAt.After(now) || in.ExpiresAt.After(now.Add(MaxEnrollmentLifetime)) {
		return nil, false, errorx.New(errorx.KindInvalidArgument, "expiresAt must be in the next 24 hours")
	}
	if in.Placement != PlacementStandalone && in.Placement != PlacementDirect {
		return nil, false, errorx.New(errorx.KindInvalidArgument, "a host's placement is standalone or direct")
	}
	object := in.Org
	if r := in.Replaces; r != nil {
		if err := ValidateOwnership(r.Agent, in.Org); err != nil {
			return nil, false, err
		}
		if r.Assignee.ServiceType != ServiceTypeSupervisor || r.Assignee.Organization != in.Org.Identifier || r.Epoch < 1 {
			return nil, false, errorx.New(errorx.KindInvalidArgument, "a replacement names the supervisor holding the agent and its epoch")
		}
		object = r.Agent
	}
	if err := s.AuthorizeEnroll(ctx, in.Actor, in.Org, object); err != nil {
		return nil, false, err
	}
	agentGRN := object
	if in.Replaces == nil {
		minted, err := NewGRN(in.Org)
		if err != nil {
			return nil, false, err
		}
		agentGRN = minted
	}
	supervisorGRN, err := newSupervisorGRN(in.Org)
	if err != nil {
		return nil, false, err
	}
	e := Enrollment{
		ID: EnrollmentID(uuid.New()), Org: in.Org, Placement: in.Placement, Agent: agentGRN, Supervisor: supervisorGRN,
		HostName: in.HostName, RequestID: in.RequestID, SecretSHA256: in.SecretSHA256,
		Replaces:   in.Replaces,
		ApprovedBy: in.Actor, ExpiresAt: in.ExpiresAt.UTC().Truncate(time.Microsecond),
		CreatedAt: now.UTC().Truncate(time.Microsecond),
	}
	stored, created, err := s.store.Approve(ctx, e)
	if err != nil || created {
		return stored, created, err
	}
	if !stored.approves(e) {
		return nil, false, ErrEnrollmentConflict
	}
	if stored.Spent == nil && (stored.Withdrawn || !now.Before(stored.ExpiresAt)) {
		return nil, false, ErrEnrollmentEnded
	}
	return stored, false, nil
}

// approves reports whether e, as an identical retry would write it,
// is the request this approval already approved. The identifiers garam
// minted and the approver are not part of it.
func (e *Enrollment) approves(retry Enrollment) bool {
	same := e.SecretSHA256 == retry.SecretSHA256 &&
		e.HostName == retry.HostName && e.ExpiresAt.Equal(retry.ExpiresAt) && e.Placement == retry.Placement
	switch {
	case e.Replaces == nil && retry.Replaces == nil:
		return same
	case e.Replaces != nil && retry.Replaces != nil:
		return same && *e.Replaces == *retry.Replaces
	}
	return false
}

func (s *standaloneService) Withdraw(ctx context.Context, actor, org grn.GRN, id EnrollmentID) error {
	e, err := s.store.GetEnrollment(ctx, org, id)
	if err != nil {
		return err
	}
	if err := s.AuthorizeEnroll(ctx, actor, org, e.object()); err != nil {
		return err
	}
	return s.store.Withdraw(ctx, org, id)
}

func (s *standaloneService) Revoke(ctx context.Context, actor, org, supervisor grn.GRN) error {
	if supervisor.ServiceType != ServiceTypeSupervisor || supervisor.Organization != org.Identifier {
		return ErrSupervisorNotFound
	}
	registered, err := s.store.GetSupervisor(ctx, supervisor)
	if err != nil {
		return err
	}
	if err := s.AuthorizeEnroll(ctx, actor, org, registered.Agent); err != nil {
		return err
	}
	return s.store.Revoke(ctx, org, supervisor)
}

func (s *standaloneService) ListEnrollments(ctx context.Context, actor, org grn.GRN, f listfilter.Filter) ([]EnrollmentSummary, error) {
	if err := authorizeCurrent(ctx, s.authorizer, actor, org, org, actionRead); err != nil {
		return nil, err
	}
	return s.store.ListEnrollments(ctx, org, f)
}

func (s *standaloneService) ListSupervisors(ctx context.Context, actor, org grn.GRN, f listfilter.Filter) ([]SupervisorSummary, error) {
	if err := authorizeCurrent(ctx, s.authorizer, actor, org, org, actionRead); err != nil {
		return nil, err
	}
	return s.store.ListSupervisors(ctx, org, f)
}

// object is what an approval's authority is decided on: the agent a
// replacement acts on, or the organization a new agent joins.
func (e *Enrollment) object() grn.GRN {
	if e.Replaces != nil {
		return e.Agent
	}
	return e.Org
}

func (s *standaloneService) AuthorizeEnroll(ctx context.Context, actor, org, object grn.GRN) error {
	return authorizeEnroll(ctx, s.authorizer, actor, org, object)
}

// authorizeEnroll asks authorizer for agent:enroll.
func authorizeEnroll(ctx context.Context, authorizer MemberAuthorizer, actor, org, object grn.GRN) error {
	return authorizeCurrent(ctx, authorizer, actor, org, object, actionEnroll)
}

// authorizeCurrent asks authorizer for action and refuses on anything
// but an allow. A failure to decide is unavailable whatever its kind,
// so an engine that cannot answer from current state never reads as a
// defect of the request.
func authorizeCurrent(ctx context.Context, authorizer MemberAuthorizer, actor, org, object grn.GRN, action string) error {
	err := authorizer.Authorize(ctx, actor, org, object, action)
	if err == nil || errorx.IsKind(err, errorx.KindPermissionDenied) {
		return err
	}
	return errorx.Wrap(err, "current authorization", errorx.KindUnavailable)
}

// newSupervisorGRN mints a supervisor identifier in the organization:
// 64 random bits in hex, as an agent's is.
func newSupervisorGRN(orgGRN grn.GRN) (grn.GRN, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return grn.GRN{}, errorx.Wrap(err, "supervisor id")
	}
	return grn.New(orgGRN.Identifier, grn.DefaultProject, ServiceTypeSupervisor, hex.EncodeToString(b)), nil
}
