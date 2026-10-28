package agent

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// LifecycleDeadline bounds an activation, a recovery and a
// deactivation from their first authority read to their commit,
// whatever the caller allows. The transport starts it before the
// caller's own recheck. Running out of it says nothing about whether
// the write committed; the caller retries under the same request
// identifier.
const LifecycleDeadline = 10 * time.Second

var (
	// ErrReferenceMismatch refuses an operation reference or handoff
	// minted for another operation, organization, caller, agent or
	// assignment.
	ErrReferenceMismatch = errorx.New(errorx.KindPermissionDenied, "operation authority does not cover this request")
	// ErrBodyMismatch refuses a request whose bytes are not the ones
	// its handoff was minted for. The handoff is not spent.
	ErrBodyMismatch = errorx.New(errorx.KindAlreadyExists, "request body does not match its authority")
	// ErrActivationIntentRequired refuses a first activation of a
	// managed agent that carries no reference: until one activation
	// has succeeded, a generation starts only on its creation's
	// reference, a configure reference (ADR-0078) or the reference of
	// the replacement that moved it (ADR-0107).
	ErrActivationIntentRequired = errorx.New(errorx.KindPermissionDenied,
		"a managed agent's first activation carries its creation, configure or replacement reference")
)

// ExecutionService is what a lifecycle authority does to an agent's
// execution (ADR-0078, ADR-0080).
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
	// Introspect answers whether a forwarded agent token and a
	// generation are current for the agent.
	Introspect(ctx context.Context, caller, agentGRN grn.GRN, credential ForwardedToken, generation *Generation) (*Introspection, error)
	// Recover moves the agent to a new credential lineage under a
	// fresh handoff. It signs nothing: the assignee mints the agent's
	// tokens in the new lineage.
	Recover(ctx context.Context, caller grn.GRN, in RecoveryInput) (*RecoveredCredential, error)
	// CreateManaged registers an agent and its first assignment under
	// a member's agent:create reference, and answers an identical
	// retry with the same agent rather than another.
	CreateManaged(ctx context.Context, caller grn.GRN, in ManagedCreationInput) (*ManagedCreation, bool, error)
}

// ActivationInput is an activation request as received.
type ActivationInput struct {
	Agent        grn.GRN
	RequestID    RequestID
	Epoch        int
	Generation   Generation
	Replaces     *ActivationID
	OperationRef *string
	// Credential is the agent's token, as the caller received it and
	// not yet proven.
	Credential ForwardedToken
}

// ForwardedToken is an agent token as a lifecycle authority received
// it from the agent and forwards it: one its assignee minted to be
// shown to the caller, or, where the caller is a standalone host's
// supervisor, the token it minted for garam (ADR-0100). Proving it
// establishes only that it names the agent; whether it is current is
// the execution fence's to judge.
type ForwardedToken string

// ActivationResult is an activation and the token that admits it.
type ActivationResult struct {
	Activation *Activation
	Token      Token
	Created    bool
}

// RecoveryInput is a recovery request as received. Body is the exact
// bytes the handoff's digest covers.
type RecoveryInput struct {
	Agent     grn.GRN
	RequestID RequestID
	Epoch     int
	Authority string
	Body      []byte
}

// CredentialStanding is whether a forwarded token may act now.
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

// Introspection is what a lifecycle authority learns about a forwarded
// token and generation.
type Introspection struct {
	Agent      grn.GRN
	Org        grn.GRN
	Assignee   grn.GRN
	Epoch      int
	Credential CredentialStanding
	Generation GenerationStanding
	Current    *ActivationID
}

type executionService struct {
	store      Store
	executions ExecutionStore
	authorizer LifecycleAuthorizer
	operations OperationResolver
	tokens     ShownTokenReader
}

// NewExecutionService constructs the execution service over the store
// it reads assignments from, the execution store, and the authority
// boundaries it consults; a forwarded token is proven by tokens.
func NewExecutionService(store Store, executions ExecutionStore, authorizer LifecycleAuthorizer, operations OperationResolver, tokens ShownTokenReader) ExecutionService {
	return &executionService{
		store: store, executions: executions, authorizer: authorizer,
		operations: operations, tokens: tokens,
	}
}

// prove answers the credential a forwarded token stands for once it
// names agentGRN: minted for the agent to be shown to caller or, for a
// supervisor caller, by caller for garam. It is not judged current
// here; the execution fence does that.
func (s *executionService) prove(ctx context.Context, caller, agentGRN grn.GRN, forwarded ForwardedToken) (Credential, error) {
	// A value no mint could have produced reads nothing.
	if !WellFormedAccessToken(string(forwarded)) {
		return Credential{}, ErrTokenNotProven
	}
	holder, err := s.tokens.AccessTokenShownTo(ctx, HashAccessToken(string(forwarded)), caller)
	if err != nil {
		return Credential{}, err
	}
	if holder.Agent != agentGRN {
		return Credential{}, ErrTokenNotProven
	}
	// A token minted for garam proves the agent only to the supervisor
	// that minted it, which is the assignee and the lifecycle authority
	// at once; every other caller forwards a token shown to it
	// (ADR-0100).
	if holder.Audience == nil && caller.ServiceType != ServiceTypeSupervisor {
		return Credential{}, ErrTokenNotProven
	}
	return Credential{Serial: holder.Serial, NotBefore: holder.IssuedAt, NotAfter: holder.ExpiresAt}, nil
}

// authorize reads the agent's current assignee and the caller's fresh
// authority over it. A hosted operator holds lifecycle authority by
// its grant and a supervisor by its registration (ADR-0082); every
// other caller, an agent's own execution credential included, holds
// none.
func (s *executionService) authorize(ctx context.Context, caller, agentGRN grn.GRN, op LifecycleOperation) (*Assignment, Grant, error) {
	if caller.ServiceType != ServiceTypeOperator && caller.ServiceType != ServiceTypeSupervisor {
		return nil, Grant{}, ErrNotALifecycleAuthority
	}
	assignment, err := s.store.GetAssignment(ctx, agentGRN)
	if err != nil {
		if errorx.IsKind(err, errorx.KindNotFound) {
			return nil, Grant{}, ErrNotAssigned
		}
		return nil, Grant{}, err
	}
	grant, err := s.authorizer.Authorize(ctx, caller, agentGRN, assignment.Assignee, op)
	if err != nil {
		return nil, Grant{}, err
	}
	return assignment, grant, nil
}

func (s *executionService) Activate(ctx context.Context, caller grn.GRN, in ActivationInput) (*ActivationResult, error) {
	assignment, grant, err := s.authorize(ctx, caller, in.Agent, OperationActivate)
	if err != nil {
		return nil, err
	}
	if in.OperationRef != nil {
		if err := s.checkReference(ctx, caller, in, assignment); err != nil {
			return nil, err
		}
	}
	credential, err := s.prove(ctx, caller, in.Agent, in.Credential)
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
		Authority:    Lifecycle{Assignee: assignment.Assignee, GrantExpiresAt: grant.ExpiresAt},
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
// assignment the agent is on now. A creation's reference is admitted
// for the first activation of the agent it created alone: it names no
// activation to replace, and the store refuses it once one exists. A
// replacement's is admitted while the agent is where it moved it, so
// an agent replaced before it ever ran can start (ADR-0107).
func (s *executionService) checkReference(ctx context.Context, caller grn.GRN, in ActivationInput, assignment *Assignment) error {
	b, err := s.operations.ResolveReference(ctx, caller, *in.OperationRef)
	if err != nil {
		return err
	}
	switch b.Kind {
	case OperationKindConfigure:
		if !b.covers(caller, in.Agent, assignment) {
			return ErrReferenceMismatch
		}
		return nil
	case OperationKindCreate:
		if in.Replaces != nil {
			return ErrReferenceMismatch
		}
		creation, err := s.creationOf(ctx, b, caller, in.Agent)
		if err != nil {
			return err
		}
		if !creation.placedOn(assignment) {
			return ErrReferenceMismatch
		}
		return nil
	case OperationKindReplace:
		if b.Audience != caller || b.Target != in.Agent {
			return ErrReferenceMismatch
		}
		replaced, err := s.executions.GetReplacement(ctx, b.Ref)
		if err != nil {
			return err
		}
		if replaced == nil || replaced.Agent != in.Agent || !replaced.placedOn(assignment) {
			return ErrReferenceMismatch
		}
		return nil
	}
	return ErrReferenceMismatch
}

// covers reports whether the binding was minted for this caller, this
// agent and the assignment it is on.
func (b *OperationBinding) covers(caller, agentGRN grn.GRN, assignment *Assignment) bool {
	return b.Org == grn.Org(agentGRN.Organization) &&
		b.Audience == caller &&
		b.Target == agentGRN &&
		b.Assignee == assignment.Assignee &&
		b.Epoch == assignment.Epoch
}

func (s *executionService) Deactivate(ctx context.Context, caller, agentGRN grn.GRN, id ActivationID) error {
	assignment, grant, err := s.authorize(ctx, caller, agentGRN, OperationActivate)
	if err != nil {
		return err
	}
	_, err = s.executions.Deactivate(ctx, agentGRN, id, Lifecycle{Assignee: assignment.Assignee, GrantExpiresAt: grant.ExpiresAt})
	return err
}

func (s *executionService) Introspect(ctx context.Context, caller, agentGRN grn.GRN, forwarded ForwardedToken, generation *Generation) (*Introspection, error) {
	assignment, _, err := s.authorize(ctx, caller, agentGRN, OperationActivate)
	if err != nil {
		return nil, err
	}
	credential, err := s.prove(ctx, caller, agentGRN, forwarded)
	if err != nil {
		return nil, err
	}
	// One read, so the token's standing, the generation and the
	// assignment answered were current together.
	snapshot, err := s.executions.Introspect(ctx, agentGRN, credential)
	if err != nil {
		return nil, err
	}
	// The authority was read for the assignee before; an answer about
	// another one would not be the one it authorized.
	if snapshot.Assignee == nil || *snapshot.Assignee != assignment.Assignee {
		return nil, ErrNotAssigned
	}
	out := &Introspection{
		Agent:      agentGRN,
		Org:        grn.Org(agentGRN.Organization),
		Assignee:   *snapshot.Assignee,
		Epoch:      snapshot.Epoch,
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

func (s *executionService) Recover(ctx context.Context, caller grn.GRN, in RecoveryInput) (*RecoveredCredential, error) {
	assignment, grant, err := s.authorize(ctx, caller, in.Agent, OperationRecover)
	if err != nil {
		return nil, err
	}
	b, err := s.operations.ResolveHandoff(ctx, caller, in.Authority)
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
		Authority:      Lifecycle{Assignee: assignment.Assignee, GrantExpiresAt: grant.ExpiresAt},
		TokenExpiresAt: b.ExpiresAt,
		OperationRef:   b.Ref,
		BodySHA256:     b.BodySHA256,
	}
	// A retry answers the lineage the record holds; the store still
	// rechecks it under the lock.
	prior, err := s.executions.GetRecovery(ctx, in.Agent, in.RequestID)
	if err != nil {
		return nil, err
	}
	if prior == nil {
		req.Lineage = NewLineage()
	}
	recovered, err := s.executions.Recover(ctx, req)
	if err != nil {
		return nil, err
	}
	return &RecoveredCredential{Lineage: recovered.Lineage}, nil
}
