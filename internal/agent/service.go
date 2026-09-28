package agent

import (
	"context"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// serviceTypeOrg is the GRN service type of an organization. Orgs are
// root-owned, so an identity's organization segment is the identifier
// of the organization that owns it (ADR-0001).
const serviceTypeOrg = "org"

// Service is what an operator does to the agents it runs — it creates
// them, obtains the certificates they authenticate with, and reports
// what it sees of their pods — and what an agent does for itself,
// which is renew its own certificate and report its own runtime state.
//
// Every method takes the caller, and an operator's answers are bounded
// by the assignments it holds — the whole of the authorization here,
// with no path around it (ADR-0015). A caller that is not an operator
// acts on the identity it authenticated as and reaches nothing else
// (ADR-0012, ADR-0040).
type Service interface {
	// Create mints an agent in the operator's own organization and
	// assigns it to that operator.
	Create(ctx context.Context, operator grn.GRN) (*Assignment, error)
	// IssueCertificate returns the certificate the named agent
	// authenticates with, for the agent itself or for an agent assigned
	// to a calling operator at the current epoch, and refuses every
	// other caller.
	IssueCertificate(ctx context.Context, caller, agentGRN grn.GRN) (*Certificate, error)
	// Definitions returns the definitions naming operator, each with
	// its claim state. The definition's operator field is the scoping,
	// so the answer can name nothing the caller is not named in and no
	// policy engine is asked (ADR-0033).
	Definitions(ctx context.Context, operator grn.GRN) ([]*DefinitionState, error)
	// Claim binds operator to the agent GRN a definition naming it
	// minted, which is what admits the operator to that agent's
	// certificate route. A definition is claimed once: a second claim
	// of the same agent GRN is a conflict.
	Claim(ctx context.Context, operator, agentGRN grn.GRN) (*Assignment, error)
	// ReportProvisioningState records what the calling operator says
	// about the agent's pod, at the epoch it holds the agent
	// (ADR-0012). It is bounded by the same assignment the certificate
	// route is, and a report carrying any epoch but the current one is
	// refused as stale.
	ReportProvisioningState(ctx context.Context, operator, agentGRN grn.GRN, epoch int, state ProvisioningState) error
	// ReportRuntimeState records what an agent says about itself, at
	// the epoch its assignment is on. The caller reports for the
	// identity it authenticated as and for no other, so no assignment
	// is read: the connection is what bounds it, and the epoch is what
	// fences it.
	ReportRuntimeState(ctx context.Context, caller, agentGRN grn.GRN, epoch int, state RuntimeState) error
	// ReportConfiguration records what the calling operator says it did
	// with a revision of the agent's definition (ADR-0076). It is
	// bounded by the assignment as ReportProvisioningState is, and a
	// revision the definition has not reached, or one older than the
	// same epoch already acknowledged, is refused and writes nothing.
	ReportConfiguration(ctx context.Context, operator, agentGRN grn.GRN, epoch int, revision Revision, outcome ConfigurationOutcome) error
}

type service struct {
	store  Store
	issuer Issuer
}

// NewService constructs the service over the store it reads
// assignments from and the issuer it signs through.
func NewService(store Store, issuer Issuer) Service {
	return &service{store: store, issuer: issuer}
}

func (s *service) Create(ctx context.Context, operator grn.GRN) (*Assignment, error) {
	if err := operates(operator); err != nil {
		return nil, err
	}
	orgGRN := orgGRNOf(operator)
	g, err := NewGRN(orgGRN)
	if err != nil {
		return nil, err
	}
	a, err := New(g, orgGRN)
	if err != nil {
		return nil, err
	}
	if err := s.store.Create(ctx, a); err != nil {
		return nil, err
	}
	// Assignment is a relation rather than part of the agent
	// (ADR-0012), so it is a second write. One that fails leaves an
	// agent no operator holds: it grants nothing and answers nobody,
	// and the caller creates another.
	return s.store.Assign(ctx, g, operator)
}

func (s *service) IssueCertificate(ctx context.Context, caller, agentGRN grn.GRN) (*Certificate, error) {
	// An agent renews its own certificate over the connection that
	// certificate authenticated, which is what keeps a lifetime short
	// enough to be the revocation ADR-0014 leaves (ADR-0040). It grants
	// nothing: the caller holds that identity already, and once the
	// certificate has expired there is no connection to renew over and
	// the operator route below is the only way back.
	if caller.ServiceType == ServiceType && caller == agentGRN {
		return s.issuer.Issue(ctx, agentGRN)
	}
	if err := operates(caller); err != nil {
		return nil, err
	}
	if err := s.holds(ctx, caller, agentGRN); err != nil {
		return nil, err
	}
	return s.issuer.Issue(ctx, agentGRN)
}

// holds refuses an operator the agent is not assigned to. The row
// holds one operator at the epoch it is assigned at, so the comparison
// is the epoch fence as well. An agent that does not exist has no
// assignment and is refused the same way, so a caller learns nothing
// about the agents it does not hold.
func (s *service) holds(ctx context.Context, operator, agentGRN grn.GRN) error {
	assignment, err := s.store.GetAssignment(ctx, agentGRN)
	if err != nil {
		if errorx.IsKind(err, errorx.KindNotFound) {
			return ErrNotAssigned
		}
		return err
	}
	if assignment.Operator() != operator {
		return ErrNotAssigned
	}
	return nil
}

func (s *service) Definitions(ctx context.Context, operator grn.GRN) ([]*DefinitionState, error) {
	if err := operates(operator); err != nil {
		return nil, err
	}
	return s.store.ListDefinitionsByOperator(ctx, operator)
}

func (s *service) Claim(ctx context.Context, operator, agentGRN grn.GRN) (*Assignment, error) {
	if err := operates(operator); err != nil {
		return nil, err
	}
	return s.store.Claim(ctx, agentGRN, operator)
}

func (s *service) ReportProvisioningState(ctx context.Context, operator, agentGRN grn.GRN, epoch int, state ProvisioningState) error {
	if err := operates(operator); err != nil {
		return err
	}
	if err := s.holds(ctx, operator, agentGRN); err != nil {
		return err
	}
	// The store writes at the current epoch and matches no row at any
	// other, which is what refuses the pod a replaced operator left
	// running. What it answers with is the agent as it now stands, and
	// that carries the runtime state the other party reports, so it is
	// not handed back to this caller.
	_, err := s.store.ReportProvisioningState(ctx, agentGRN, epoch, state)
	return err
}

func (s *service) ReportRuntimeState(ctx context.Context, caller, agentGRN grn.GRN, epoch int, state RuntimeState) error {
	if caller != agentGRN {
		return ErrNotTheAgent
	}
	_, err := s.store.ReportRuntimeState(ctx, agentGRN, epoch, state)
	return err
}

func (s *service) ReportConfiguration(ctx context.Context, operator, agentGRN grn.GRN, epoch int, revision Revision, outcome ConfigurationOutcome) error {
	if err := operates(operator); err != nil {
		return err
	}
	if err := s.holds(ctx, operator, agentGRN); err != nil {
		return err
	}
	// holds answers a caller that holds nothing without naming the
	// agent; the write compares the operator and the epoch again, in
	// one statement, so a reassignment between the two reads cannot
	// attribute this report to the operator that replaced the caller.
	// The agent it answers with carries the other party's runtime
	// state, as it does for the provisioning report.
	_, err := s.store.ReportConfiguration(ctx, operator, agentGRN, epoch, revision, outcome)
	return err
}

// operates refuses a caller that is not an operator. An agent
// authenticates on the same listener and holds no agent (ADR-0015).
func operates(caller grn.GRN) error {
	if caller.ServiceType != ServiceTypeOperator {
		return ErrNotAnOperator
	}
	return nil
}

// orgGRNOf returns the GRN of the organization that owns identity,
// which is the organization an agent it creates belongs to.
func orgGRNOf(identity grn.GRN) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, serviceTypeOrg, identity.Organization)
}
