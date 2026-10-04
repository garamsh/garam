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
// renews it afterwards (ADR-0082). No private key reaches garam: the
// host generated both keys and the bootstrap secret before it asked
// for the approval, and every answer is public.
type SupervisorService interface {
	// Enroll spends an approval and answers both certificates, created
	// or answered again for an identical retry.
	Enroll(ctx context.Context, in EnrollmentInput) (*EnrollmentResult, bool, error)
	// RenewSupervisor issues the calling supervisor's next certificate
	// over the key its request carries, or answers again the one an
	// identical renewal of the presented certificate issued.
	RenewSupervisor(ctx context.Context, caller grn.GRN, presented Credential, requestID RequestID, requestPEM []byte) (*SupervisorCertificate, bool, error)
}

type supervisorService struct {
	store      StandaloneStore
	authorizer MemberAuthorizer
	signer     RequestSigner
	now        func() time.Time
}

// NewSupervisorService constructs the service over the store it keeps
// approvals and registrations in, what decides the approving
// administrator's current authority again, and what signs a
// certificate over a host's key. A nil now takes time.Now.
func NewSupervisorService(store StandaloneStore, authorizer MemberAuthorizer, signer RequestSigner, now func() time.Time) SupervisorService {
	if now == nil {
		now = time.Now
	}
	return &supervisorService{store: store, authorizer: authorizer, signer: signer, now: now}
}

func (s *supervisorService) Enroll(ctx context.Context, in EnrollmentInput) (*EnrollmentResult, bool, error) {
	if len(in.SupervisorCertificateRequestPEM) > MaxRequestPEM || len(in.AgentCertificateRequestPEM) > MaxRequestPEM {
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
	if err := authorizeEnroll(ctx, s.authorizer, e.ApprovedBy, e.Org, e.object()); err != nil {
		return nil, false, err
	}
	req := EnrollmentRequest{Enrollment: e.ID, RequestSHA256: sha256.Sum256(in.Body)}
	// A spent approval is answered, or refused, from its record before
	// anything is signed again; the store still decides under the lock.
	if e.Spent == nil {
		if err := approvedKey(in.SupervisorCertificateRequestPEM, e.SupervisorKey); err != nil {
			return nil, false, err
		}
		if err := approvedKey(in.AgentCertificateRequestPEM, e.AgentKey); err != nil {
			return nil, false, err
		}
		if req.Supervisor, err = s.signer.SignRequest(ctx, e.Supervisor, in.SupervisorCertificateRequestPEM); err != nil {
			return nil, false, err
		}
		if req.Agent, err = s.signer.SignRequest(ctx, e.Agent, in.AgentCertificateRequestPEM); err != nil {
			return nil, false, err
		}
		req.Lineage = NewLineage()
	}
	return s.store.Enroll(ctx, req)
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
		if registered.Serial != presented.Serial {
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
