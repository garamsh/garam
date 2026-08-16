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
// (ADR-0014). It is the seam `internal/ca` satisfies: this domain
// names a subject by GRN and never reads key material.
type Issuer interface {
	// Issue returns a certificate for subject, signed by the authority
	// of the organization that owns it.
	Issue(ctx context.Context, subject grn.GRN) (*Certificate, error)
}

// Service is what an operator does to the agents it runs: it creates
// them and obtains the certificates they authenticate with.
//
// Both methods take the calling operator, and every answer is bounded
// by the assignment that operator holds. That is the whole of the
// authorization here and there is no path around it — an operator's
// blast radius is the agents assigned to it, which ADR-0015 states is
// not an optimisation and may not be skipped.
type Service interface {
	// Create mints an agent in the operator's own organization and
	// assigns it to that operator.
	Create(ctx context.Context, operator grn.GRN) (*Assignment, error)
	// IssueCertificate returns the certificate the named agent
	// authenticates with, for an agent assigned to operator at the
	// current epoch, and refuses every other one.
	IssueCertificate(ctx context.Context, operator, agentGRN grn.GRN) (*Certificate, error)
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

func (s *service) IssueCertificate(ctx context.Context, operator, agentGRN grn.GRN) (*Certificate, error) {
	if err := operates(operator); err != nil {
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
	if assignment.Operator() != operator {
		return nil, ErrNotAssigned
	}
	return s.issuer.Issue(ctx, agentGRN)
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
