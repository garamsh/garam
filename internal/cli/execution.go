package cli

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"

	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/operator"
	operatorpg "github.com/garamsh/garam/internal/operator/postgres"
	"github.com/garamsh/garam/internal/org"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// newExecutionService composes what a lifecycle authority does to an
// agent's execution (ADR-0078) over the database, the policy engine,
// the delegated operation authority, the machine boundary's
// certificate proof and the certificate authority.
func newExecutionService(pool *pgxpool.Pool, engine grpc.ClientConnInterface, authority operator.AuthorityService,
	identities machine.Service, issuer ca.Service, serverRoot []byte, now func() time.Time,
) agent.ExecutionService {
	if now == nil {
		now = time.Now
	}
	return agent.NewExecutionService(agent.NewPostgresStore(pool), agentpg.New(pool),
		lifecycleAuthorization{
			grants:      operatorpg.New(pool),
			supervisors: agentpg.New(pool),
			policy: audienceAuthorization{groups: memberGroups{store: org.NewPostgresStore(pool)},
				engine: authzv1.NewAuthzServiceClient(engine)},
			now: now,
		},
		operationAuthorities{service: authority},
		provenLeaves{identities: identities},
		requestSigner{issuer: issuer, serverRoot: serverRoot},
		certificates{issuer: issuer, serverRoot: serverRoot})
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
	orgGRN := grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeOrg, agentGRN.Organization)
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
// approval that registered it, so a renewal changes nothing an
// activation retry is matched by, and it expires with the certificate
// garam last issued the supervisor.
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
	return agent.Grant{ID: "supervisor:" + registered.Enrollment.String(), ExpiresAt: registered.NotAfter}, nil
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
	if b.Assignment != nil {
		epoch, err := strconv.Atoi(b.Assignment.Epoch)
		if err != nil {
			return nil, errorx.Wrap(err, "bound assignment epoch", errorx.KindInternal)
		}
		out.Assignee, out.Epoch = b.Assignment.Assignee, epoch
	}
	return out, nil
}

// provenLeaves proves a forwarded agent certificate through the machine
// boundary (agent.LeafVerifier). The proof establishes the chain and
// the subject only; the execution fence judges the time and the
// lineage.
type provenLeaves struct{ identities machine.Service }

func (p provenLeaves) VerifyLeaf(ctx context.Context, agentGRN grn.GRN, leaf *x509.Certificate) (agent.Credential, error) {
	identity, err := p.identities.Prove(ctx, agentGRN, leaf)
	if errorx.IsKind(err, errorx.KindInvalidArgument) {
		return agent.Credential{}, agent.ErrLeafNotProven
	}
	if err != nil {
		return agent.Credential{}, err
	}
	return agent.Credential{Serial: identity.Serial(), NotBefore: identity.NotBefore(), NotAfter: identity.NotAfter()}, nil
}

// requestSigner signs an agent's initial or recovered certificate over
// the key its request carries (agent.RequestSigner), and hands back the
// garam server root beside it. Its Chain reads the same two values
// without signing.
type requestSigner struct {
	issuer     ca.Service
	serverRoot []byte
}

func (s requestSigner) SignRequest(ctx context.Context, subject grn.GRN, requestPEM []byte) (*agent.SignedCertificate, error) {
	signed, err := s.issuer.Sign(ctx, subject, requestPEM)
	if errorx.IsKind(err, errorx.KindInvalidArgument) {
		return nil, agent.ErrInvalidCertificateRequest
	}
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(signed.CertificatePEM())
	if block == nil {
		return nil, errorx.New(errorx.KindInternal, "signed certificate is not PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errorx.Wrap(err, "signed certificate", errorx.KindInternal)
	}
	return &agent.SignedCertificate{
		Credential:     agent.Credential{Serial: signed.Serial(), NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter},
		CertificatePEM: string(signed.CertificatePEM()),
		IssuerPEM:      string(signed.IssuerPEM()),
		ServerRootPEM:  string(s.serverRoot),
	}, nil
}

func (s requestSigner) Chain(ctx context.Context, subject grn.GRN) (*agent.Chain, error) {
	issuer, err := s.issuer.IssuerPEM(ctx, subject)
	if err != nil {
		return nil, err
	}
	return &agent.Chain{IssuerPEM: string(issuer), ServerRootPEM: string(s.serverRoot)}, nil
}
