package agent

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// EnrollmentContractVersion names the contract the managed enrollment
// routes serve, carried in the Garam-Contract-Version header.
const EnrollmentContractVersion = "managed-enrollment.v1"

// Managed enrollment sentinels (ADR-0077, ADR-0078).
var (
	// ErrUnsupportedEnrollmentContract refuses a request carrying no
	// contract version, or another.
	ErrUnsupportedEnrollmentContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: this route serves "+EnrollmentContractVersion)
	// ErrCreationConflict refuses a creation whose reference binds
	// another controller or request identifier, a request identifier
	// an earlier creation holds under another reference, and the retry
	// of a creation whose assignment has moved since.
	ErrCreationConflict = errorx.New(errorx.KindAlreadyExists, "managed creation conflicts with an earlier one")
	// ErrInitialCertificateConflict refuses an initial issuance that is
	// not the one recorded: another request identifier, reference,
	// epoch or certificate request, once one has been recorded; and
	// one for an agent no longer where its creation put it.
	ErrInitialCertificateConflict = errorx.New(errorx.KindAlreadyExists, "initial certificate conflicts with the one issued")
	// ErrLineageSuperseded refuses an initial issuance, or its retry,
	// once a recovery has replaced the lineage it was issued into.
	ErrLineageSuperseded = errorx.New(errorx.KindFailedPrecondition, "the agent's initial credential lineage was superseded")
	// ErrInvalidCertificateRequest refuses a certificate request that
	// is not one PEM PKCS#10 request proving possession of an ECDSA
	// P-256 key.
	ErrInvalidCertificateRequest = errorx.New(errorx.KindInvalidArgument, "certificate request is not a valid ECDSA P-256 PKCS#10 request")
)

// ManagedCreationInput is a managed creation as received: the
// controller the new agent is assigned to, the request identifier and
// the member's agent:create reference both are bound by.
type ManagedCreationInput struct {
	Controller   grn.GRN
	RequestID    RequestID
	OperationRef string
}

// ManagedCreation is an agent the hosted control service created: its
// identity, its first assignment and the reference that authorized
// it. Lineage is the credential lineage it was born into, which holds
// no certificate until its initial issuance.
type ManagedCreation struct {
	Agent        grn.GRN
	Audience     grn.GRN
	Controller   grn.GRN
	RequestID    RequestID
	OperationRef string
	Epoch        int
	Lineage      Lineage
}

// InitialCertificateInput is an initial issuance as received.
type InitialCertificateInput struct {
	Agent                 grn.GRN
	RequestID             RequestID
	Epoch                 int
	OperationRef          string
	CertificateRequestPEM []byte
}

// InitialCertificateRequest is an initial issuance the authority
// admitted, as the store applies it. Signed is nil when an issuance
// was already recorded and is only to be answered again.
type InitialCertificateRequest struct {
	Agent         grn.GRN
	RequestID     RequestID
	Epoch         int
	OperationRef  string
	RequestSHA256 [32]byte
	Authority     Lifecycle
	Lineage       Lineage
	Signed        *SignedCertificate
}

// InitialCertificate is the public answer of an initial issuance, the
// first time and on a retry. It carries no private key.
type InitialCertificate struct {
	CertificatePEM string
	IssuerPEM      string
	ServerRootPEM  string
	NotAfter       time.Time
}

// CreateManaged registers an agent and its first assignment under the
// caller's agent:create reference, rechecked as current before any
// earlier creation is answered. Its execution is fenced from birth in
// a lineage of its own that holds no certificate yet.
func (s *executionService) CreateManaged(ctx context.Context, caller grn.GRN, in ManagedCreationInput) (*ManagedCreation, bool, error) {
	if err := operates(caller); err != nil {
		return nil, false, err
	}
	if in.Controller.ServiceType != ServiceTypeOperator || in.Controller.Organization != caller.Organization {
		return nil, false, errorx.New(errorx.KindNotFound, "controller not found")
	}
	b, err := s.operations.ResolveReference(ctx, caller, in.OperationRef)
	if err != nil {
		return nil, false, err
	}
	if b.Kind != OperationKindCreate || b.Audience != caller || b.Org != orgGRNOf(caller) {
		return nil, false, ErrReferenceMismatch
	}
	if b.Target != in.Controller || b.RequestID != string(in.RequestID) {
		return nil, false, ErrCreationConflict
	}
	creation, err := s.executions.GetManagedCreationByReference(ctx, in.OperationRef)
	if err != nil {
		return nil, false, err
	}
	created := false
	if creation == nil {
		agentGRN, err := NewGRN(b.Org)
		if err != nil {
			return nil, false, err
		}
		a, err := New(agentGRN, b.Org)
		if err != nil {
			return nil, false, err
		}
		creation, created, err = s.executions.CreateManaged(ctx, a, ManagedCreation{
			Agent: agentGRN, Audience: caller, Controller: in.Controller, RequestID: in.RequestID,
			OperationRef: in.OperationRef, Lineage: NewLineage(),
		})
		if err != nil {
			return nil, false, err
		}
	}
	if !created {
		// An earlier answer is the same only while the agent is still
		// where that creation put it.
		assignment, err := s.store.GetAssignment(ctx, creation.Agent)
		if err != nil {
			return nil, false, err
		}
		if !creation.placedOn(assignment) {
			return nil, false, ErrCreationConflict
		}
	}
	return creation, created, nil
}

// IssueInitialCertificate signs the first certificate of a managed
// agent over the key its request carries, and records it as the first
// of the lineage the agent was born into. The creation reference must
// name this agent's creation and still be current, and the caller
// must hold lifecycle authority for the agent's current controller.
// Signing happens before the execution lock; the store rechecks the
// assignment and every expiry under it.
func (s *executionService) IssueInitialCertificate(ctx context.Context, caller grn.GRN, in InitialCertificateInput) (*InitialCertificate, bool, error) {
	if len(in.CertificateRequestPEM) > MaxRequestPEM {
		return nil, false, errorx.New(errorx.KindInvalidArgument, "certificate request exceeds its bound")
	}
	assignment, grant, err := s.authorize(ctx, caller, in.Agent, OperationActivate)
	if err != nil {
		return nil, false, err
	}
	b, err := s.operations.ResolveReference(ctx, caller, in.OperationRef)
	if err != nil {
		return nil, false, err
	}
	creation, err := s.creationOf(ctx, b, caller, in.Agent)
	if err != nil {
		return nil, false, err
	}
	if !creation.placedOn(assignment) {
		return nil, false, ErrInitialCertificateConflict
	}
	req := InitialCertificateRequest{
		Agent:         in.Agent,
		RequestID:     in.RequestID,
		Epoch:         in.Epoch,
		OperationRef:  in.OperationRef,
		RequestSHA256: sha256.Sum256(in.CertificateRequestPEM),
		Authority:     Lifecycle{Assignee: assignment.Operator(), GrantExpiresAt: grant.ExpiresAt},
		Lineage:       creation.Lineage,
	}
	// A recorded issuance is answered, or refused, from the record
	// before anything is signed again; the store still decides under
	// the lock.
	prior, err := s.executions.GetInitialCertificate(ctx, in.Agent)
	if err != nil {
		return nil, false, err
	}
	if prior == nil {
		if req.Signed, err = s.signer.SignRequest(ctx, in.Agent, in.CertificateRequestPEM); err != nil {
			return nil, false, err
		}
	}
	return s.executions.InstallInitialCertificate(ctx, req)
}

// creationOf answers the managed creation of agentGRN when b, read
// back for caller, is that creation's own agent:create reference. A
// reference of another agent's creation, even on the same controller,
// covers nothing here.
func (s *executionService) creationOf(ctx context.Context, b *OperationBinding, caller, agentGRN grn.GRN) (*ManagedCreation, error) {
	if b.Kind != OperationKindCreate || b.Audience != caller || b.Org != orgGRNOf(agentGRN) {
		return nil, ErrReferenceMismatch
	}
	creation, err := s.executions.GetManagedCreation(ctx, agentGRN)
	if err != nil {
		return nil, err
	}
	if creation == nil || creation.OperationRef != b.Ref {
		return nil, ErrReferenceMismatch
	}
	return creation, nil
}

// placedOn reports whether the agent is still where its creation put
// it: the same controller at the same epoch.
func (c *ManagedCreation) placedOn(assignment *Assignment) bool {
	return assignment.Operator() == c.Controller && assignment.Epoch() == c.Epoch
}
