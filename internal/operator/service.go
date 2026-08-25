package operator

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// renewalFraction is the share of a certificate's validity that is its
// renewal window, counted back from expiry. A third of it: wide enough
// that an operator polling on any cadence its own lifetime tolerates
// meets it many times before running out, and narrow enough that a
// certificate stays a certificate rather than a permit to hold one.
const renewalFraction = 3

// Issuer signs the certificate an operator authenticates with
// (ADR-0014) and hands back the garam server root it verifies garam by
// (ADR-0040). It is the seam `internal/ca` and the composition root
// satisfy between them: this domain names a subject by GRN and never
// reads key material.
type Issuer interface {
	// Issue returns a certificate for subject, signed by the authority
	// of the organization that owns it.
	Issue(ctx context.Context, subject grn.GRN) (*Certificate, error)
}

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
// garam last issued and for no other copy of the same identity. There
// is no revocation to fall back on (ADR-0014), so a chain of renewals
// nothing narrows would make any stolen certificate permanent.
type Service interface {
	// Issue mints an operator's certificate out of band and adopts it,
	// taking the lineage from whatever the operator held before
	// (ADR-0032).
	Issue(ctx context.Context, subject grn.GRN) (*Certificate, error)
	// Renew issues the successor to the certificate a connection
	// authenticated with, and refuses one that has expired, one still
	// outside its renewal window, and one garam has already replaced.
	Renew(ctx context.Context, c Credential) (*Certificate, error)
}

type service struct {
	store  CertificateStore
	issuer Issuer
	now    func() time.Time
}

// NewService constructs the service over the record it keeps of each
// operator's certificate and the issuer it signs through.
func NewService(store CertificateStore, issuer Issuer, cfg Config) Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &service{store: store, issuer: issuer, now: now}
}

func (s *service) Issue(ctx context.Context, subject grn.GRN) (*Certificate, error) {
	if subject.ServiceType != ServiceType {
		return nil, ErrNotAnOperator
	}
	issued, err := s.issuer.Issue(ctx, subject)
	if err != nil {
		return nil, err
	}
	if err := s.store.Adopt(ctx, current(subject, issued, s.now())); err != nil {
		return nil, err
	}
	return issued, nil
}

func (s *service) Renew(ctx context.Context, c Credential) (*Certificate, error) {
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
	if err := s.store.Advance(ctx, current(c.Subject, issued, now), c.Serial); err != nil {
		return nil, err
	}
	return issued, nil
}

// current is the record an issued certificate leaves behind.
func current(subject grn.GRN, issued *Certificate, now time.Time) Current {
	return Current{Operator: subject, Serial: issued.Serial, IssuedAt: now.UTC()}
}

// renewable reports whether the presented certificate may be replaced,
// reading the window off the certificate itself so the rule holds
// whatever lifetime signed it.
//
// Expiry is the empty end of that window rather than a case beside it.
// An expired certificate has no connection to renew over — the listener
// refuses it a handshake before any handler is reached (ADR-0014) — and
// the boundary is stated here as well, so that reaching the rule any
// other way meets the same refusal.
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
