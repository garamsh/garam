package ca

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Certificate lifetime bounds. ADR-0014 accepted expiry as the only
// revocation there is — no CRL, no OCSP — so the lifetime is the whole
// window a compromised certificate stays usable in, and an unbounded
// one is that mechanism switched off rather than a setting. What the
// deployment picks inside these bounds is configuration (ADR-0040).
const (
	// DefaultCertificateLifetime is what [Config] takes when it says
	// nothing: a day of exposure, costing one renewal a day. It is
	// deliberately conservative — the mechanism that makes a shorter one
	// usable ships before the number is tightened (ADR-0040).
	DefaultCertificateLifetime = 24 * time.Hour
	// MinCertificateLifetime keeps a certificate usable. One that
	// expires inside the time it takes to reach the process that
	// presents it is not a credential.
	MinCertificateLifetime = time.Hour
	// MaxCertificateLifetime is the widest exposure window this package
	// will issue. Anything longer is a decision to record rather than a
	// value to configure.
	MaxCertificateLifetime = 30 * 24 * time.Hour
)

// serviceTypeOrg is the GRN service type of an organization. Orgs are
// root-owned, so an org's identifier is the organization segment of
// every resource beneath it (ADR-0001).
const serviceTypeOrg = "org"

// Config holds the CA's security parameters.
type Config struct {
	// CertificateLifetime is the longest an issued certificate is valid
	// for; jitter takes it no further than that. Zero takes
	// DefaultCertificateLifetime, and anything outside
	// MinCertificateLifetime..MaxCertificateLifetime is refused — a
	// refusal the process that reads it from its configuration does not
	// start past (ADR-0040).
	CertificateLifetime time.Duration
	// Now supplies the current time, so a test can hold it still. Zero
	// takes time.Now.
	Now func() time.Time
}

// Service issues the certificates a machine identity authenticates
// with (ADR-0014).
type Service interface {
	// Issue returns a client certificate for subject, signed by the CA
	// of the organization that owns it. The organization's CA is
	// created on the first certificate it needs.
	Issue(ctx context.Context, subject grn.GRN) (*Certificate, error)
}

type service struct {
	store    Store
	sealer   Sealer
	lifetime time.Duration
	now      func() time.Time
}

// NewService constructs the certificate service, or refuses a lifetime
// outside the bounds above. There is no constructor that omits the
// Sealer: a process without a key-encryption key cannot reach the CA at
// all.
func NewService(store Store, sealer Sealer, cfg Config) (Service, error) {
	lifetime := cfg.CertificateLifetime
	if lifetime == 0 {
		lifetime = DefaultCertificateLifetime
	}
	if lifetime < MinCertificateLifetime || lifetime > MaxCertificateLifetime {
		return nil, errorx.New(errorx.KindInvalidArgument,
			"ca: certificate lifetime must be between "+MinCertificateLifetime.String()+" and "+MaxCertificateLifetime.String())
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &service{store: store, sealer: sealer, lifetime: lifetime, now: now}, nil
}

func (s *service) Issue(ctx context.Context, subject grn.GRN) (*Certificate, error) {
	if err := subject.Validate(); err != nil {
		return nil, errorx.Wrap(err, "ca: subject")
	}
	if subject.Organization == grn.RootOwner {
		return nil, errorx.New(errorx.KindInvalidArgument, "ca: root owns no certificate authority")
	}
	uri, err := subjectURI(subject)
	if err != nil {
		return nil, err
	}
	authority, err := s.authorityOf(ctx, orgGRNOf(subject))
	if err != nil {
		return nil, err
	}
	issuer, issuerKey, err := s.open(ctx, authority)
	if err != nil {
		return nil, err
	}

	key, err := generateKey()
	if err != nil {
		return nil, err
	}
	serial, err := serialNumber()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	notAfter, err := expiry(now, s.lifetime)
	if err != nil {
		return nil, err
	}
	// A certificate that outlives its issuer stops verifying the day the
	// CA expires, so it is refused rather than signed. The parsed
	// certificate is what a verifier reads, so it is what bounds this.
	// Neither branch can refuse a CA created by this call: the bounds
	// above cap a lifetime far below authorityLifetime.
	switch {
	case !now.Before(issuer.NotAfter):
		return nil, errorx.New(errorx.KindFailedPrecondition, "ca: the authority that signs has expired")
	case notAfter.After(issuer.NotAfter):
		return nil, errorx.New(errorx.KindFailedPrecondition, "ca: certificate would outlive the authority that signs it")
	}
	der, err := createCertificate(serial, uri, &key.PublicKey, issuer, issuerKey, now, notAfter)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: marshal private key", errorx.KindInternal)
	}
	return &Certificate{
		subject:   subject,
		serial:    serial.String(),
		certPEM:   encodePEM("CERTIFICATE", der),
		keyPEM:    encodePEM("PRIVATE KEY", keyDER),
		issuerPEM: encodePEM("CERTIFICATE", authority.certificate),
		notAfter:  notAfter,
	}, nil
}

// authorityOf returns the organization's CA, creating it on the first
// certificate the organization needs. Two processes can reach the
// create at once, and the one that loses reads the row the winner
// wrote rather than failing a caller.
func (s *service) authorityOf(ctx context.Context, orgGRN grn.GRN) (*Authority, error) {
	authority, err := s.store.Get(ctx, orgGRN)
	if err == nil {
		return ownedBy(authority, orgGRN)
	}
	if !errorx.IsKind(err, errorx.KindNotFound) {
		return nil, err
	}
	authority, err = s.newAuthority(ctx, orgGRN)
	if err != nil {
		return nil, err
	}
	if err := s.store.Create(ctx, authority); err != nil {
		if errorx.IsKind(err, errorx.KindAlreadyExists) {
			winner, err := s.store.Get(ctx, orgGRN)
			if err != nil {
				return nil, err
			}
			return ownedBy(winner, orgGRN)
		}
		return nil, err
	}
	return authority, nil
}

// ownedBy refuses an authority that is not the organization's own. The
// store that ships here looks up by primary key, so a mismatch is a bug
// in an implementation of the seam rather than anything a caller did —
// and since ADR-0014 rests tenancy on the trust root, this is the last
// place a wrong CA could be caught before it signs.
func ownedBy(a *Authority, orgGRN grn.GRN) (*Authority, error) {
	switch {
	case a == nil:
		return nil, errorx.New(errorx.KindInternal, "ca: store returned no authority and no error")
	case a.orgGRN != orgGRN:
		return nil, errorx.New(errorx.KindInternal, "ca: store returned another organization's authority")
	}
	return a, nil
}

// newAuthority generates an organization's CA with its private key
// sealed. Nothing returns the key in the clear, so the only reader is
// open below.
func (s *service) newAuthority(ctx context.Context, orgGRN grn.GRN) (*Authority, error) {
	key, err := generateKey()
	if err != nil {
		return nil, err
	}
	now := s.now().UTC().Truncate(time.Second)
	der, err := createAuthorityCertificate(orgGRN, key, now)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: marshal authority key", errorx.KindInternal)
	}
	sealed, err := s.sealer.Seal(ctx, binding(orgGRN), keyDER)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: seal authority key")
	}
	return &Authority{
		orgGRN:      orgGRN,
		certificate: der,
		sealedKey:   sealed,
		notAfter:    now.Add(authorityLifetime),
		createdAt:   now,
	}, nil
}

// binding ties a sealed key to the organization whose row holds it, so
// a key copied onto another organization's row does not open there.
func binding(orgGRN grn.GRN) []byte {
	return []byte(orgGRN.String())
}

// open unseals the CA's private key and parses its certificate. A
// stored value that no longer parses is corruption, hence internal.
func (s *service) open(ctx context.Context, a *Authority) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certificate, err := x509.ParseCertificate(a.certificate)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "ca: authority certificate", errorx.KindInternal)
	}
	keyDER, err := s.sealer.Open(ctx, binding(a.orgGRN), a.sealedKey)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "ca: open authority key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "ca: authority key", errorx.KindInternal)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errorx.New(errorx.KindInternal, "ca: authority key is not an ecdsa key")
	}
	return certificate, key, nil
}

// orgGRNOf returns the GRN of the organization that owns the subject,
// whose CA is the only one that may sign for it. A subject whose
// organization segment would parse into something else never reaches
// here: Issue validates the subject first.
func orgGRNOf(subject grn.GRN) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, serviceTypeOrg, subject.Organization)
}
