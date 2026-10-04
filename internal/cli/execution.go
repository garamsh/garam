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
			grants: operatorpg.New(pool),
			policy: audienceAuthorization{groups: memberGroups{store: org.NewPostgresStore(pool)},
				engine: authzv1.NewAuthzServiceClient(engine)},
			now: now,
		},
		operationAuthorities{service: authority},
		provenLeaves{identities: identities},
		requestSigner{issuer: issuer, serverRoot: serverRoot},
		certificates{issuer: issuer, serverRoot: serverRoot})
}

// lifecycleAuthorization decides a hosted operator's lifecycle
// authority over one agent from current state
// (agent.LifecycleAuthorizer): its current grant in the agent's
// organization, which must cover the operation for the agent's
// assignee, and current policy on the agent, where an explicit Deny
// wins. Its registration is rechecked on the
// listener before this is asked.
type lifecycleAuthorization struct {
	grants operator.GrantStore
	policy audienceAuthorization
	now    func() time.Time
}

func (a lifecycleAuthorization) Authorize(ctx context.Context, caller, agentGRN, assignee grn.GRN, op agent.LifecycleOperation) (agent.Grant, error) {
	if caller.ServiceType != operator.ServiceType || caller.Organization != agentGRN.Organization {
		return agent.Grant{}, errorx.New(errorx.KindPermissionDenied, "the caller holds no lifecycle authority over this agent")
	}
	orgGRN := grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeOrg, agentGRN.Organization)
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
	err = a.policy.Authorize(ctx, caller, orgGRN, agentGRN, string(op))
	switch {
	case err == nil:
		return agent.Grant{ID: string(grant.ID), ExpiresAt: grant.ExpiresAt}, nil
	case errorx.IsKind(err, errorx.KindPermissionDenied):
		return agent.Grant{}, err
	}
	return agent.Grant{}, errorx.Wrap(err, "current policy", errorx.KindUnavailable)
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
	if b.Assignment != nil {
		epoch, err := strconv.Atoi(b.Assignment.Epoch)
		if err != nil {
			return nil, errorx.Wrap(err, "bound assignment epoch", errorx.KindInternal)
		}
		out.Assignee, out.Epoch = b.Assignment.Operator, epoch
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
// garam server root beside it.
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
