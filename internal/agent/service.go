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

// Issuer signs the certificate a machine identity authenticates with
// (ADR-0014) and hands back the garam server root it verifies garam by
// (ADR-0040). It is the seam `internal/ca` and the composition root
// satisfy between them: this domain names a subject by GRN and never
// reads key material.
type Issuer interface {
	// Issue returns a certificate for subject, signed by the authority
	// of the organization that owns it.
	Issue(ctx context.Context, subject grn.GRN) (*Certificate, error)
}

// Service is what an operator does to the agents it runs: it creates
// them and obtains the certificates they authenticate with.
//
// Every method takes the caller, and an operator's answers are bounded
// by the assignments it holds. That is the whole of the authorization
// here and there is no path around it — an operator's blast radius is
// the agents assigned to it, which ADR-0015 states is not an
// optimisation and may not be skipped. The one caller that is not an
// operator is an agent renewing its own certificate (ADR-0040), which
// is bounded by the identity it already authenticated as.
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
	assignment, err := s.store.GetAssignment(ctx, agentGRN)
	if err != nil {
		if errorx.IsKind(err, errorx.KindNotFound) {
			return nil, ErrNotAssigned
		}
		return nil, err
	}
	// The row holds one operator at the epoch it is assigned at, so
	// comparing it is the epoch fence as well: a reassignment moves the
	// row, and the operator it replaced stops matching. GRNs carry
	// their organization, so an operator of another organization
	// differs here too.
	if assignment.Operator() != caller {
		return nil, ErrNotAssigned
	}
	return s.issuer.Issue(ctx, agentGRN)
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
