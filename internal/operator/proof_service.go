package operator

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ProofService proves a controller's current authority to the hosted
// operator that controls it, before each lifecycle decision that
// operator makes (ADR-0078).
//
// It decides on GRNs and reads no certificate: the caller's and the
// controller's are verified on the machine listener before it is
// asked, registrations included. What it reads on every call is the
// caller's current grant, a named agent's current assignment, and
// current policy through AudienceAuthorizer, and a read it cannot make
// refuses as unavailable rather than answering from anything older.
type ProofService interface {
	// Prove answers the proof when the audience's current grant covers
	// agent:activate for controller, a named agent is currently assigned
	// to controller, and current policy does not deny agent:activate on
	// the organization, or on the agent where one is named. controller
	// is one ParseController answered for audience.
	Prove(ctx context.Context, audience, controller grn.GRN, agent *grn.GRN) (*ControllerProof, error)
}

type proofService struct {
	grants      GrantStore
	assignments AssignmentReader
	policy      AudienceAuthorizer
	now         func() time.Time
}

// NewProofService constructs the service over the grants it reads,
// where agents run, and what decides an audience's current policy.
func NewProofService(grants GrantStore, assignments AssignmentReader, policy AudienceAuthorizer, cfg Config) ProofService {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &proofService{grants: grants, assignments: assignments, policy: policy, now: now}
}

func (s *proofService) Prove(ctx context.Context, audience, controller grn.GRN, agent *grn.GRN) (*ControllerProof, error) {
	org := grn.New(grn.RootOwner, grn.DefaultProject, orgServiceType, audience.Organization)
	grant, err := s.grants.CurrentGrant(ctx, org, audience)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return nil, ErrControllerNotDelegated
	}
	if err != nil {
		return nil, errorx.Wrap(err, "current delegation", errorx.KindUnavailable)
	}
	if !grant.Covers(OperationAgentActivate, controller, s.now()) {
		return nil, ErrControllerNotDelegated
	}
	proof := &ControllerProof{Controller: controller, Org: org}
	object := org
	if agent != nil {
		if proof.Agent, err = s.placed(ctx, org, controller, *agent); err != nil {
			return nil, err
		}
		object = *agent
	}
	err = s.policy.Authorize(ctx, audience, org, object, string(OperationAgentActivate))
	switch {
	case err == nil:
		return proof, nil
	case errorx.IsKind(err, errorx.KindPermissionDenied):
		return nil, err
	}
	return nil, errorx.Wrap(err, "current policy", errorx.KindUnavailable)
}

// placed answers agent's current placement where it is on controller.
func (s *proofService) placed(ctx context.Context, org, controller, agent grn.GRN) (*ProvenAgent, error) {
	if agent.ServiceType != agentServiceType || agent.Organization != org.Identifier {
		return nil, ErrAgentNotFound
	}
	a, err := s.assignments.CurrentAssignment(ctx, agent)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return nil, ErrAgentNotFound
	}
	if err != nil {
		return nil, errorx.Wrap(err, "current assignment", errorx.KindUnavailable)
	}
	if a.Assignee != controller {
		return nil, ErrAgentElsewhere
	}
	return &ProvenAgent{Agent: agent, Epoch: a.Epoch}, nil
}
