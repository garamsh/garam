package agent

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// serviceTypeOrg is the GRN service type of an organization. Orgs are
// root-owned, so an identity's organization segment is the identifier
// of the organization that owns it (ADR-0001).
const serviceTypeOrg = "org"

// Service is what an operator does to the agents it runs — it creates
// them, obtains the certificates they authenticate with, and reports
// what it sees of their pods — and what an agent reports about its own
// runtime state. An agent renews its own certificate through
// ExecutionService instead (ADR-0078).
//
// Every method takes the caller, and an operator's answers are bounded
// by the assignments it holds — the whole of the authorization here,
// with no path around it (ADR-0015). A caller that is not an operator
// acts on the identity it authenticated as and reaches nothing else
// (ADR-0012).
type Service interface {
	// Create mints an agent in the operator's own organization and
	// assigns it to that operator.
	Create(ctx context.Context, operator grn.GRN) (*Assignment, error)
	// IssueCertificate returns the certificate the named agent
	// authenticates with, for an agent assigned to a calling operator at
	// the current epoch whose execution is still legacy and whose
	// cutover is not switched, and refuses every other caller. An agent
	// renews its own through ExecutionService.Renew, inside its
	// execution fence (ADR-0078).
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
	ReportRuntimeState(ctx context.Context, caller, agentGRN grn.GRN, epoch int, state RuntimeState, fence Fencing) error
	// MintAccessToken answers a token the agent authenticates with in
	// place of a certificate, for an agent the calling operator or
	// supervisor holds at the current epoch, accepted by garam where
	// audience is nil and by that hosted operator where it names one
	// (ADR-0100). It is
	// admitted where the certificate route would be, and inside the
	// lineage of an agent whose lifecycle authority opened one; every
	// other caller and agent is refused as AccessTokenStore says.
	MintAccessToken(ctx context.Context, caller, agentGRN grn.GRN, audience *grn.GRN) (*AccessToken, error)
}

// Config holds what an agent token is minted with.
type Config struct {
	// TokenLifetime is how long an agent token is accepted; zero is
	// DefaultAccessTokenLifetime. NewService refuses one outside the
	// bounds.
	TokenLifetime time.Duration
	// Backdate is how far before now a token's IssuedAt is set, so a
	// token is current at once on a database whose clock trails this
	// process's, as a certificate's NotBefore is backdated. It moves
	// the start alone; ExpiresAt is measured from now.
	Backdate time.Duration
	// ServerRootPEM is the garam server root answered beside every
	// agent token, for its holder to pin garam by (ADR-0040, ADR-0100).
	ServerRootPEM []byte
	// Now supplies the time a token is minted at, so a test can hold it
	// still. Zero takes time.Now.
	Now func() time.Time
}

type service struct {
	store         Store
	tokens        AccessTokenStore
	audiences     RegisteredOperators
	issuer        Issuer
	tokenLifetime time.Duration
	backdate      time.Duration
	serverRoot    []byte
	now           func() time.Time
}

// NewService constructs the service over the store it reads
// assignments from, the store agent tokens are recorded in, the
// operators a token's audience is checked against, and the issuer it
// signs through.
func NewService(store Store, tokens AccessTokenStore, audiences RegisteredOperators, issuer Issuer, cfg Config) (Service, error) {
	lifetime, err := ParseAccessTokenLifetime(cfg.TokenLifetime)
	if err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &service{store: store, tokens: tokens, audiences: audiences, issuer: issuer, tokenLifetime: lifetime,
		backdate: cfg.Backdate, serverRoot: cfg.ServerRootPEM, now: now}, nil
}

func (s *service) MintAccessToken(ctx context.Context, caller, agentGRN grn.GRN, audience *grn.GRN) (*AccessToken, error) {
	// An assignee mints: an operator, or the supervisor a standalone
	// host runs the agent under (ADR-0100). The store answers either for
	// the agent it holds at the current epoch and no other.
	if caller.ServiceType != ServiceTypeSupervisor {
		if err := operates(caller); err != nil {
			return nil, err
		}
	}
	if audience != nil && caller.ServiceType == ServiceTypeSupervisor {
		return nil, ErrSupervisorMintAudience
	}
	if audience != nil {
		if audience.ServiceType != ServiceTypeOperator ||
			audience.Organization != agentGRN.Organization || *audience == caller {
			return nil, ErrAccessTokenAudience
		}
		if err := s.audiences.Registered(ctx, *audience); err != nil {
			if errorx.IsKind(err, errorx.KindNotFound) {
				return nil, ErrAccessTokenAudience
			}
			return nil, errorx.Wrap(err, "token audience", errorx.KindUnavailable)
		}
	}
	value, err := newAccessTokenValue()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	token := &AccessToken{Value: value, Agent: agentGRN, Audience: audience,
		IssuedAt: now.Add(-s.backdate), ExpiresAt: now.Add(s.tokenLifetime), ServerRootPEM: s.serverRoot}
	if err := s.tokens.MintAccessToken(ctx, caller, token); err != nil {
		return nil, err
	}
	return token, nil
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
	if err := operates(caller); err != nil {
		return nil, err
	}
	if err := s.holds(ctx, caller, agentGRN); err != nil {
		return nil, err
	}
	// Once an agent's execution is fenced, a certificate outside its
	// lineage would reach nothing, and one inside it is issued only by
	// renewal or recovery (ADR-0078); once its cutover is switched, its
	// operator no longer owns it (ADR-0086). The first read spares a
	// signature; the second decides, after signing, so no certificate
	// is answered once a switch has committed.
	if err := s.store.AdmitLegacyIssuance(ctx, agentGRN); err != nil {
		return nil, err
	}
	certificate, err := s.issuer.Issue(ctx, agentGRN)
	if err != nil {
		return nil, err
	}
	if err := s.store.AdmitLegacyIssuance(ctx, agentGRN); err != nil {
		return nil, err
	}
	return certificate, nil
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
	if assignment.Assignee() != operator {
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

func (s *service) ReportRuntimeState(ctx context.Context, caller, agentGRN grn.GRN, epoch int, state RuntimeState, fence Fencing) error {
	if caller != agentGRN {
		return ErrNotTheAgent
	}
	if fence.Agent != agentGRN {
		return errorx.New(errorx.KindInternal, "agent: the fence names another agent")
	}
	_, err := s.store.ReportRuntimeState(ctx, agentGRN, epoch, state, fence)
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
