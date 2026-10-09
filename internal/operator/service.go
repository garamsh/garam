package operator

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// renewalFraction is the share of a certificate's validity that is its
// renewal window, counted back from expiry. A third of it: wide enough
// that an operator polling on any cadence its own lifetime tolerates
// meets it many times before running out, and narrow enough that a
// certificate stays a certificate rather than a permit to hold one.
const renewalFraction = 3

// Config holds where the renewal rule reads the time from.
type Config struct {
	// Now supplies the current time, so a test can hold it still. Zero
	// takes time.Now.
	Now func() time.Time
}

// Service is what an operator's own certificate goes through: the
// out-of-band mint that starts a lineage, and the renewal that carries
// it on.
//
// Both record the certificate they issued, and that record is the whole
// of the authorization here: a renewal is admitted for the certificate
// garam last issued and for no other copy of the same identity. That
// is what a renewal has to bound by itself — deleting the operator
// ends every copy at once (ADR-0070), but it ends the operator too, so
// a chain of renewals nothing narrows would make a stolen certificate
// permanent for as long as the organization still wants the operator
// it was taken from.
type Service interface {
	// Issue mints an operator's certificate out of band and adopts it,
	// taking the lineage from whatever the operator held before
	// (ADR-0032).
	Issue(ctx context.Context, subject grn.GRN) (*IssuedCertificate, error)
	// Renew issues the successor to the certificate a connection
	// authenticated with, and refuses one that has expired, one still
	// outside its renewal window, and one garam has already replaced.
	Renew(ctx context.Context, c Credential) (*IssuedCertificate, error)
	// Enroll spends token and signs request for the operator the token
	// names, adopting the certificate as that operator's current one.
	// No key is generated and none is returned: the operator made the
	// key where it will be used, and garam signs what it is sent
	// (ADR-0064).
	Enroll(ctx context.Context, token string, request CertificateRequest) (*Certificate, error)
	// Exchange answers a token for the operator credential belongs to,
	// accepted by garam where audience is nil and by that hosted
	// operator where it names one, for [TokenTTL] (ADR-0100). It
	// refuses a credential no registered operator holds with
	// ErrCredentialInvalid, and an audience that is not another
	// operator of the same organization with ErrAudienceInvalid.
	Exchange(ctx context.Context, credential string, audience *grn.GRN) (*Token, error)
}

type service struct {
	store       CertificateStore
	enrollments EnrollmentStore
	tokens      TokenStore
	issuer      Issuer
	now         func() time.Time
}

// NewService constructs the service over the record it keeps of each
// operator's certificate, the tokens an enrollment spends, the tokens
// a credential is exchanged for, and the issuer it signs through.
func NewService(store CertificateStore, enrollments EnrollmentStore, tokens TokenStore, issuer Issuer, cfg Config) Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &service{store: store, enrollments: enrollments, tokens: tokens, issuer: issuer, now: now}
}

func (s *service) Exchange(ctx context.Context, credential string, audience *grn.GRN) (*Token, error) {
	holder, err := s.tokens.CredentialHolder(ctx, HashToken(credential))
	if err != nil {
		return nil, err
	}
	if audience != nil && (audience.ServiceType != ServiceType ||
		audience.Organization != holder.Operator.Organization || *audience == holder.Operator) {
		return nil, ErrAudienceInvalid
	}
	value, err := secret(TokenPrefix)
	if err != nil {
		return nil, errorx.Wrap(err, "operator token")
	}
	now := s.now().UTC()
	token := &Token{Value: value, Operator: holder.Operator, Audience: audience,
		IssuedAt: now, ExpiresAt: now.Add(TokenTTL)}
	if err := s.tokens.RecordToken(ctx, holder, token); err != nil {
		return nil, err
	}
	return token, nil
}

func (s *service) Issue(ctx context.Context, subject grn.GRN) (*IssuedCertificate, error) {
	if subject.ServiceType != ServiceType {
		return nil, ErrNotAnOperator
	}
	issued, err := s.issuer.Issue(ctx, subject)
	if err != nil {
		return nil, err
	}
	if err := s.store.Adopt(ctx, current(subject, issued.Serial, s.now())); err != nil {
		return nil, err
	}
	return issued, nil
}

func (s *service) Enroll(ctx context.Context, token string, request CertificateRequest) (*Certificate, error) {
	// Spent before anything is signed, so a token buys one attempt
	// rather than one certificate: a request garam refuses costs the
	// token, and the operator registers again. The other order would
	// let a caller hold a token open across as many malformed requests
	// as it liked.
	subject, err := s.enrollments.Spend(ctx, HashToken(token))
	if err != nil {
		return nil, err
	}
	signed, err := s.issuer.Sign(ctx, subject, request)
	if err != nil {
		return nil, err
	}
	// Adopted as the out-of-band mint adopts, so an enrolled operator
	// starts the lineage a renewal is measured against (ADR-0046) and
	// takes it from whatever certificate already named it.
	if err := s.store.Adopt(ctx, current(subject, signed.Serial, s.now())); err != nil {
		return nil, err
	}
	return signed, nil
}

func (s *service) Renew(ctx context.Context, c Credential) (*IssuedCertificate, error) {
	now := s.now()
	if err := renewable(c, now); err != nil {
		return nil, err
	}
	issued, err := s.issuer.Issue(ctx, c.Subject)
	if err != nil {
		return nil, err
	}
	// Signed before the record moves, so a caller that has lost the
	// lineage receives nothing: the certificate it would have had is
	// discarded here, with the private key that exists nowhere but
	// inside this call.
	if err := s.store.Advance(ctx, current(c.Subject, issued.Serial, now), c.Serial); err != nil {
		return nil, err
	}
	return issued, nil
}

// current is the record an issued certificate leaves behind.
func current(subject grn.GRN, serial Serial, now time.Time) Current {
	return Current{Operator: subject, Serial: serial, IssuedAt: now.UTC()}
}

// renewable reports whether the presented certificate may be replaced,
// reading the window off the certificate itself so the rule holds
// whatever lifetime signed it.
//
// Expiry is the empty end of that window rather than a case beside it.
// An expired certificate authenticates nothing, so no identity reaches
// the route holding one (ADR-0014), and the boundary is stated here as
// well, so that reaching the rule any other way meets the same refusal.
func renewable(c Credential, now time.Time) error {
	if c.Subject.ServiceType != ServiceType {
		return ErrNotAnOperator
	}
	remaining := c.NotAfter.Sub(now)
	switch {
	case remaining <= 0:
		return ErrCertificateExpired
	case remaining > c.NotAfter.Sub(c.NotBefore)/renewalFraction:
		return ErrRenewalTooEarly
	}
	return nil
}
