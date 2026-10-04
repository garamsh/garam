package agent

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Bounds on what a lifecycle request may carry, refused before
// anything parses it.
const (
	MaxCertificatePEM = 8 << 10
	MaxRequestPEM     = 4 << 10
)

var (
	// ErrUnsupportedReference refuses an operation reference this
	// release does not accept for an activation: a creation's, until
	// the agent it created is bound to it.
	ErrUnsupportedReference = errorx.New(errorx.KindInvalidArgument, "operation reference is not supported for activation")
	// ErrReferenceMismatch refuses an operation reference or handoff
	// minted for another operation, organization, caller, agent or
	// assignment.
	ErrReferenceMismatch = errorx.New(errorx.KindPermissionDenied, "operation authority does not cover this request")
	// ErrBodyMismatch refuses a request whose bytes are not the ones
	// its handoff was minted for. The handoff is not spent.
	ErrBodyMismatch = errorx.New(errorx.KindAlreadyExists, "request body does not match its authority")
)

// ExecutionService is what a lifecycle authority does to an agent's
// execution, and how an agent renews inside it (ADR-0078, ADR-0080).
//
// Each lifecycle method reads its authority fresh before it takes the
// execution lock and holds no lock while doing so; the store then
// rechecks under the lock what can change without a revocation — the
// assignment and every expiry. A revocation committing in between does
// not undo the act; the next request is refused.
type ExecutionService interface {
	// Activate admits a generation of the agent and answers its token.
	Activate(ctx context.Context, caller grn.GRN, in ActivationInput) (*ActivationResult, error)
	// Deactivate ends the activation id names if it is the agent's
	// current one, and does nothing if it already ended.
	Deactivate(ctx context.Context, caller, agentGRN grn.GRN, id ActivationID) error
	// Introspect answers whether a presented certificate and
	// generation are current for the agent.
	Introspect(ctx context.Context, caller, agentGRN grn.GRN, certificatePEM []byte, generation *Generation) (*Introspection, error)
	// Recover moves the agent to a new credential lineage under a
	// fresh handoff, answering the certificate signed over the
	// request's key.
	Recover(ctx context.Context, caller grn.GRN, in RecoveryInput) (*Recovery, error)
	// Renew issues the calling agent's next certificate inside the
	// lineage the presented one is current in.
	Renew(ctx context.Context, caller grn.GRN, token *Token, presented Credential) (*Certificate, error)
}

// ActivationInput is an activation request as received.
type ActivationInput struct {
	Agent          grn.GRN
	RequestID      RequestID
	Epoch          int
	Generation     Generation
	Replaces       *ActivationID
	OperationRef   *string
	CertificatePEM []byte
}

// ActivationResult is an activation and the token that admits it.
type ActivationResult struct {
	Activation *Activation
	Token      Token
	Created    bool
}

// RecoveryInput is a recovery request as received. Body is the exact
// bytes the handoff's digest covers.
type RecoveryInput struct {
	Agent                 grn.GRN
	RequestID             RequestID
	Epoch                 int
	Authority             string
	Body                  []byte
	CertificateRequestPEM []byte
}

// CredentialStanding is whether a presented certificate may act now.
type CredentialStanding string

const (
	CredentialCurrent CredentialStanding = "current"
	CredentialFenced  CredentialStanding = "fenced"
)

// GenerationStanding is how a supplied generation compares with the
// agent's active one.
type GenerationStanding string

const (
	GenerationCurrent     GenerationStanding = "current"
	GenerationNotCurrent  GenerationStanding = "not_current"
	GenerationNone        GenerationStanding = "none"
	GenerationNotSupplied GenerationStanding = "not_supplied"
)

// Introspection is what a lifecycle authority learns about a presented
// certificate and generation.
type Introspection struct {
	Agent      grn.GRN
	Org        grn.GRN
	Assignee   grn.GRN
	Epoch      int
	Mode       Mode
	Credential CredentialStanding
	Generation GenerationStanding
	Current    *ActivationID
}

type executionService struct {
	store      Store
	executions ExecutionStore
	authorizer LifecycleAuthorizer
	operations OperationResolver
	leaves     LeafVerifier
	signer     RequestSigner
	issuer     Issuer
}

// NewExecutionService constructs the execution service over the store
// it reads assignments from, the execution store, and the authority,
// verification and signing boundaries it consults.
func NewExecutionService(store Store, executions ExecutionStore, authorizer LifecycleAuthorizer, operations OperationResolver, leaves LeafVerifier, signer RequestSigner, issuer Issuer) ExecutionService {
	return &executionService{
		store: store, executions: executions, authorizer: authorizer,
		operations: operations, leaves: leaves, signer: signer, issuer: issuer,
	}
}

// authorize reads the agent's current assignee and the caller's fresh
// authority over it.
func (s *executionService) authorize(ctx context.Context, caller, agentGRN grn.GRN, op LifecycleOperation) (*Assignment, Grant, error) {
	if err := operates(caller); err != nil {
		return nil, Grant{}, err
	}
	assignment, err := s.store.GetAssignment(ctx, agentGRN)
	if err != nil {
		if errorx.IsKind(err, errorx.KindNotFound) {
			return nil, Grant{}, ErrNotAssigned
		}
		return nil, Grant{}, err
	}
	grant, err := s.authorizer.Authorize(ctx, caller, agentGRN, assignment.Operator(), op)
	if err != nil {
		return nil, Grant{}, err
	}
	return assignment, grant, nil
}

func (s *executionService) Activate(ctx context.Context, caller grn.GRN, in ActivationInput) (*ActivationResult, error) {
	if len(in.CertificatePEM) > MaxCertificatePEM {
		return nil, errorx.New(errorx.KindInvalidArgument, "certificate exceeds its bound")
	}
	assignment, grant, err := s.authorize(ctx, caller, in.Agent, OperationActivate)
	if err != nil {
		return nil, err
	}
	if in.OperationRef != nil {
		if err := s.checkReference(ctx, caller, in.Agent, assignment, *in.OperationRef); err != nil {
			return nil, err
		}
	}
	credential, err := s.leaves.VerifyLeaf(ctx, in.Agent, in.CertificatePEM)
	if err != nil {
		return nil, err
	}
	token, err := NewToken()
	if err != nil {
		return nil, err
	}
	a, created, err := s.executions.Activate(ctx, ActivationRequest{
		Agent:        in.Agent,
		RequestID:    in.RequestID,
		Generation:   in.Generation,
		Epoch:        in.Epoch,
		Replaces:     in.Replaces,
		Audience:     caller,
		GrantID:      grant.ID,
		OperationRef: in.OperationRef,
		Authority:    Lifecycle{Assignee: assignment.Operator(), GrantExpiresAt: grant.ExpiresAt},
		Credential:   credential,
		TokenHash:    token.Hash(),
	})
	if err != nil {
		return nil, err
	}
	return &ActivationResult{Activation: a, Token: token, Created: created}, nil
}

// checkReference admits a pending revision's durable reference when it
// was minted to configure this agent, through this caller, for the
// assignment the agent is on now. A creation's reference is refused
// until the agent it created is bound to it.
func (s *executionService) checkReference(ctx context.Context, caller, agentGRN grn.GRN, assignment *Assignment, ref string) error {
	b, err := s.operations.ResolveReference(ctx, ref)
	if err != nil {
		return err
	}
	switch b.Kind {
	case OperationKindConfigure:
	case OperationKindCreate:
		return ErrUnsupportedReference
	default:
		return ErrReferenceMismatch
	}
	if !b.covers(caller, agentGRN, assignment) {
		return ErrReferenceMismatch
	}
	return nil
}

// covers reports whether the binding was minted for this caller, this
// agent and the assignment it is on.
func (b *OperationBinding) covers(caller, agentGRN grn.GRN, assignment *Assignment) bool {
	return b.Org == orgGRNOf(agentGRN) &&
		b.Audience == caller &&
		b.Target == agentGRN &&
		b.Assignee == assignment.Operator() &&
		b.Epoch == assignment.Epoch()
}

func (s *executionService) Deactivate(ctx context.Context, caller, agentGRN grn.GRN, id ActivationID) error {
	assignment, grant, err := s.authorize(ctx, caller, agentGRN, OperationActivate)
	if err != nil {
		return err
	}
	_, err = s.executions.Deactivate(ctx, agentGRN, id, Lifecycle{Assignee: assignment.Operator(), GrantExpiresAt: grant.ExpiresAt})
	return err
}

func (s *executionService) Introspect(ctx context.Context, caller, agentGRN grn.GRN, certificatePEM []byte, generation *Generation) (*Introspection, error) {
	if len(certificatePEM) > MaxCertificatePEM {
		return nil, errorx.New(errorx.KindInvalidArgument, "certificate exceeds its bound")
	}
	assignment, _, err := s.authorize(ctx, caller, agentGRN, OperationActivate)
	if err != nil {
		return nil, err
	}
	credential, err := s.leaves.VerifyLeaf(ctx, agentGRN, certificatePEM)
	if err != nil {
		return nil, err
	}
	// One read, so the certificate's standing, the generation and the
	// assignment answered were current together.
	snapshot, err := s.executions.Introspect(ctx, agentGRN, credential)
	if err != nil {
		return nil, err
	}
	// The authority was read for the assignee before; an answer about
	// another one would not be the one it authorized.
	if snapshot.Assignee == nil || *snapshot.Assignee != assignment.Operator() {
		return nil, ErrNotAssigned
	}
	out := &Introspection{
		Agent:      agentGRN,
		Org:        orgGRNOf(agentGRN),
		Assignee:   *snapshot.Assignee,
		Epoch:      snapshot.Epoch,
		Mode:       snapshot.Mode,
		Credential: CredentialFenced,
		Generation: standing(snapshot.Current, generation),
	}
	if snapshot.CredentialCurrent {
		out.Credential = CredentialCurrent
	}
	if snapshot.Current != nil {
		out.Current = &snapshot.Current.ID
	}
	return out, nil
}

// standing compares a supplied generation with the active one: with
// one supplied, it is current or not; with none, the answer says only
// whether any generation is active.
func standing(active *CurrentActivation, supplied *Generation) GenerationStanding {
	switch {
	case supplied != nil && active != nil && active.Generation == *supplied:
		return GenerationCurrent
	case supplied != nil:
		return GenerationNotCurrent
	case active == nil:
		return GenerationNone
	default:
		return GenerationNotSupplied
	}
}

func (s *executionService) Recover(ctx context.Context, caller grn.GRN, in RecoveryInput) (*Recovery, error) {
	if len(in.CertificateRequestPEM) > MaxRequestPEM {
		return nil, errorx.New(errorx.KindInvalidArgument, "certificate request exceeds its bound")
	}
	assignment, grant, err := s.authorize(ctx, caller, in.Agent, OperationRecover)
	if err != nil {
		return nil, err
	}
	b, err := s.operations.ResolveHandoff(ctx, in.Authority)
	if err != nil {
		return nil, err
	}
	if b.Kind != OperationKindRecover || !b.covers(caller, in.Agent, assignment) {
		return nil, ErrReferenceMismatch
	}
	if sha256.Sum256(in.Body) != b.BodySHA256 {
		return nil, ErrBodyMismatch
	}
	req := RecoveryRequest{
		Agent:          in.Agent,
		RequestID:      in.RequestID,
		Epoch:          in.Epoch,
		Authority:      Lifecycle{Assignee: assignment.Operator(), GrantExpiresAt: grant.ExpiresAt},
		TokenExpiresAt: b.ExpiresAt,
		OperationRef:   b.Ref,
		BodySHA256:     b.BodySHA256,
	}
	// A retry is answered from the record before anything is signed
	// again; the store still rechecks it under the lock.
	prior, err := s.executions.GetRecovery(ctx, in.Agent, in.RequestID)
	if err != nil {
		return nil, err
	}
	if prior == nil {
		signed, err := s.signer.SignRequest(ctx, in.Agent, in.CertificateRequestPEM)
		if err != nil {
			return nil, err
		}
		req.Lineage, req.Credential, req.CertificatePEM = NewLineage(), signed.Credential, signed.CertificatePEM
	}
	return s.executions.Recover(ctx, req)
}

func (s *executionService) Renew(ctx context.Context, caller grn.GRN, token *Token, presented Credential) (*Certificate, error) {
	if caller.ServiceType != ServiceType {
		return nil, ErrNotTheAgent
	}
	// Checked before signing so a fenced certificate costs no
	// signature; RecordRenewal is what decides.
	current, err := s.executions.CredentialCurrent(ctx, caller, presented)
	if err != nil {
		return nil, err
	}
	if !current {
		return nil, ErrCredentialFenced
	}
	certificate, err := s.issuer.Issue(ctx, caller)
	if err != nil {
		return nil, err
	}
	successor, err := credentialOf(certificate.CertificatePEM)
	if err != nil {
		return nil, err
	}
	var hash *TokenHash
	if token != nil {
		h := token.Hash()
		hash = &h
	}
	if err := s.executions.RecordRenewal(ctx, caller, hash, presented, successor); err != nil {
		return nil, err
	}
	return certificate, nil
}

// credentialOf reads the credential of a certificate this deployment
// just issued. One that does not parse is an internal error.
func credentialOf(certificatePEM []byte) (Credential, error) {
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		return Credential{}, errorx.New(errorx.KindInternal, "issued certificate is not PEM")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Credential{}, errorx.Wrap(err, "issued certificate", errorx.KindInternal)
	}
	return Credential{Serial: leaf.SerialNumber.String(), NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter}, nil
}
