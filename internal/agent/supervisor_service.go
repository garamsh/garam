package agent

import (
	"context"
	"crypto/sha256"
	"time"
)

// SupervisorService is how a standalone host's supervisor obtains what
// it authenticates with by spending its administrator's approval, and
// exchanges it afterwards (ADR-0082, ADR-0100): garam answers a
// credential it keeps only the hash of.
type SupervisorService interface {
	// Enroll spends an approval and answers the supervisor credential
	// it issued, created or, for an identical retry, re-issued.
	Enroll(ctx context.Context, in EnrollmentInput) (*EnrollmentResult, bool, error)
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
	serverRoot string
	now        func() time.Time
}

// NewSupervisorService constructs the service over the store it keeps
// approvals and registrations in, the store a supervisor's credential
// and tokens are kept in, what decides the approving administrator's
// current authority again, and the garam server root an enrollment
// answers beside its credential. A nil now takes time.Now.
func NewSupervisorService(store StandaloneStore, tokens SupervisorTokenStore, authorizer MemberAuthorizer, serverRootPEM []byte, now func() time.Time) SupervisorService {
	if now == nil {
		now = time.Now
	}
	return &supervisorService{store: store, tokens: tokens, authorizer: authorizer,
		serverRoot: string(serverRootPEM), now: now}
}

func (s *supervisorService) Enroll(ctx context.Context, in EnrollmentInput) (*EnrollmentResult, bool, error) {
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
	// garam keeps only the credential's hash, so a retry cannot be
	// answered with the one issued before: it issues another, and the
	// store voids the earlier one with its tokens.
	req := EnrollmentRequest{Enrollment: e.ID, RequestSHA256: sha256.Sum256(in.Body), Lineage: NewLineage()}
	if req.Credential, err = NewSupervisorCredential(); err != nil {
		return nil, false, err
	}
	result, created, err := s.store.Enroll(ctx, req)
	if err != nil {
		return nil, false, err
	}
	result.Credential, result.ServerRootPEM = req.Credential.Value, s.serverRoot
	return result, created, nil
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
