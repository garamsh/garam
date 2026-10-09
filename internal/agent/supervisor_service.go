package agent

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// SupervisorService is how a standalone host's supervisor obtains what
// it authenticates with by spending its administrator's approval, and
// renews or exchanges it afterwards (ADR-0082, ADR-0100). No private
// key reaches garam: under standalone-enrollment.v1 the host generated
// both keys before it asked for the approval and every answer is
// public, and under v2 garam answers a credential it keeps only the
// hash of.
type SupervisorService interface {
	// Enroll spends an approval and answers what it issued, created or
	// answered again for an identical retry: under v1 both
	// certificates, and under v2 a supervisor credential, re-issued by
	// a retry.
	Enroll(ctx context.Context, in EnrollmentInput) (*EnrollmentResult, bool, error)
	// RenewSupervisor issues the calling supervisor's next certificate
	// over the key its request carries, or answers again the one an
	// identical renewal of the presented certificate issued.
	RenewSupervisor(ctx context.Context, caller grn.GRN, presented Credential, requestID RequestID, requestPEM []byte) (*SupervisorCertificate, bool, error)
	// Exchange answers an hour-long token for the supervisor credential
	// belongs to. It refuses a credential no current registration holds
	// with ErrSupervisorCredentialInvalid, before any read where the
	// value is not a credential's shape.
	Exchange(ctx context.Context, credential string) (*SupervisorToken, error)
}

type supervisorService struct {
	store      StandaloneStore
	tokens     SupervisorTokenStore
	authorizer MemberAuthorizer
	signer     RequestSigner
	serverRoot string
	now        func() time.Time
}

// NewSupervisorService constructs the service over the store it keeps
// approvals and registrations in, the store a supervisor's credential
// and tokens are kept in, what decides the approving administrator's
// current authority again, what signs a certificate over a host's key,
// and the garam server root a v2 enrollment answers beside its
// credential. A nil now takes time.Now.
func NewSupervisorService(store StandaloneStore, tokens SupervisorTokenStore, authorizer MemberAuthorizer, signer RequestSigner, serverRootPEM []byte, now func() time.Time) SupervisorService {
	if now == nil {
		now = time.Now
	}
	return &supervisorService{store: store, tokens: tokens, authorizer: authorizer, signer: signer,
		serverRoot: string(serverRootPEM), now: now}
}

func (s *supervisorService) Enroll(ctx context.Context, in EnrollmentInput) (*EnrollmentResult, bool, error) {
	if r := in.Requests; r != nil && (len(r.SupervisorPEM) > MaxRequestPEM || len(r.AgentPEM) > MaxRequestPEM) {
		return nil, false, errorx.New(errorx.KindInvalidArgument, "certificate request exceeds its bound")
	}
	e, err := s.store.GetEnrollmentBySecret(ctx, sha256.Sum256([]byte(in.Secret)))
	if err != nil {
		return nil, false, err
	}
	if e.Withdrawn || !s.now().Before(e.ExpiresAt) {
		return nil, false, ErrBootstrapRefused
	}
	if in.RequestID != e.RequestID {
		return nil, false, ErrEnrollmentConflict
	}
	// The keys an approval binds decide the contract it is spent
	// under, so neither contract spends the other's approval.
	switch {
	case e.Keys != nil && in.Requests == nil:
		return nil, false, ErrApprovedForCertificates
	case e.Keys == nil && in.Requests != nil:
		return nil, false, ErrApprovedForCredential
	}
	if err := authorizeEnroll(ctx, s.authorizer, e.ApprovedBy, e.Org, e.object()); err != nil {
		return nil, false, err
	}
	req := EnrollmentRequest{Enrollment: e.ID, RequestSHA256: sha256.Sum256(in.Body)}
	if e.Keys == nil {
		// garam keeps only the credential's hash, so a retry cannot be
		// answered with the one issued before: it issues another, and
		// the store voids the earlier one with its tokens.
		if req.Credential, err = NewSupervisorCredential(); err != nil {
			return nil, false, err
		}
		req.Lineage = NewLineage()
		result, created, err := s.store.Enroll(ctx, req)
		if err != nil {
			return nil, false, err
		}
		result.Credential, result.ServerRootPEM = req.Credential.Value, s.serverRoot
		return result, created, nil
	}
	// A spent approval is answered, or refused, from its record before
	// anything is signed again; the store still decides under the lock.
	if e.Spent == nil {
		if err := approvedKey(in.Requests.SupervisorPEM, e.Keys.Supervisor); err != nil {
			return nil, false, err
		}
		if err := approvedKey(in.Requests.AgentPEM, e.Keys.Agent); err != nil {
			return nil, false, err
		}
		if req.Supervisor, err = s.signer.SignRequest(ctx, e.Supervisor, in.Requests.SupervisorPEM); err != nil {
			return nil, false, err
		}
		if req.Agent, err = s.signer.SignRequest(ctx, e.Agent, in.Requests.AgentPEM); err != nil {
			return nil, false, err
		}
		req.Lineage = NewLineage()
	}
	return s.store.Enroll(ctx, req)
}

func (s *supervisorService) Exchange(ctx context.Context, credential string) (*SupervisorToken, error) {
	// A value no spending could have produced reads nothing.
	if !WellFormedSupervisorSecret(credential, SupervisorCredentialPrefix) {
		return nil, ErrSupervisorCredentialInvalid
	}
	holder, err := s.tokens.SupervisorCredentialHolder(ctx, HashSupervisorSecret(credential))
	if err != nil {
		return nil, err
	}
	value, err := newSecret(SupervisorTokenPrefix)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	token := &SupervisorToken{Value: value, Supervisor: holder.Supervisor, IssuedAt: now, ExpiresAt: now.Add(SupervisorTokenTTL)}
	if err := s.tokens.RecordSupervisorToken(ctx, holder, token); err != nil {
		return nil, err
	}
	return token, nil
}

// approvedKey refuses a certificate request whose key is not the one
// approved.
func approvedKey(requestPEM []byte, approved KeyFingerprint) error {
	got, err := RequestedKeyFingerprint(requestPEM)
	if err != nil {
		return err
	}
	if got != approved {
		return ErrKeyNotApproved
	}
	return nil
}

func (s *supervisorService) RenewSupervisor(ctx context.Context, caller grn.GRN, presented Credential, requestID RequestID, requestPEM []byte) (*SupervisorCertificate, bool, error) {
	if caller.ServiceType != ServiceTypeSupervisor {
		return nil, false, ErrNotASupervisor
	}
	if len(requestPEM) > MaxRequestPEM {
		return nil, false, errorx.New(errorx.KindInvalidArgument, "certificate request exceeds its bound")
	}
	req := SupervisorRenewal{Supervisor: caller, Presented: presented, RequestID: requestID, RequestSHA256: sha256.Sum256(requestPEM)}
	// A certificate that already renewed is answered from the record
	// before anything is signed again.
	recorded, err := s.store.GetRenewal(ctx, caller, presented.Serial)
	if err != nil {
		return nil, false, err
	}
	if recorded == nil {
		// Only the certificate garam last issued renews; any other is
		// refused before anything is signed.
		registered, err := s.store.GetSupervisor(ctx, caller)
		if err != nil {
			return nil, false, err
		}
		if registered.Certificate == nil || registered.Certificate.Serial != presented.Serial {
			return nil, false, ErrRenewalConflict
		}
		lifetime := presented.NotAfter.Sub(presented.NotBefore)
		if presented.NotAfter.Sub(s.now()) > lifetime/3 {
			return nil, false, ErrRenewalTooEarly
		}
		if req.Signed, err = s.signer.SignRequest(ctx, caller, requestPEM); err != nil {
			return nil, false, err
		}
	}
	return s.store.Renew(ctx, req)
}
