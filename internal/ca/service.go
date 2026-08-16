package ca

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// DefaultCertificateLifetime is how long an issued certificate lasts
// unless [Config] says otherwise. ADR-0014 accepted expiry as the only
// revocation there is — no CRL, no OCSP — so this is the whole window a
// compromised certificate stays usable in, and one day of it costs one
// restart a day to rotate.
const DefaultCertificateLifetime = 24 * time.Hour

// serviceTypeOrg is the GRN service type of an organization. Orgs are
// root-owned, so an org's identifier is the organization segment of
// every resource beneath it (ADR-0001).
const serviceTypeOrg = "org"

// Config holds the CA's security parameters.
type Config struct {
	// CertificateLifetime is the longest an issued certificate is
	// valid for; jitter takes it no further than that. A value that is
	// not positive takes DefaultCertificateLifetime.
	CertificateLifetime time.Duration
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
}

// NewService constructs the certificate service. There is no
// constructor that omits the Sealer: a process without a
// key-encryption key cannot reach the CA at all.
func NewService(store Store, sealer Sealer, cfg Config) Service {
	lifetime := cfg.CertificateLifetime
	if lifetime <= 0 {
		lifetime = DefaultCertificateLifetime
	}
	return &service{store: store, sealer: sealer, lifetime: lifetime}
}

func (s *service) Issue(ctx context.Context, subject grn.GRN) (*Certificate, error) {
	if err := subject.Validate(); err != nil {
		return nil, errorx.Wrap(err, "ca: subject")
	}
	if subject.Organization == grn.RootOwner {
		return nil, errorx.New(errorx.KindInvalidArgument, "ca: root owns no certificate authority")
	}
	// The identity is parsed before anything is created for it: a
	// subject that cannot be named in a SAN URI gets no organization
	// CA brought into existence on its way to being refused.
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
	now := time.Now().UTC()
	notAfter, err := expiry(now, s.lifetime)
	if err != nil {
		return nil, err
	}
	der, err := createCertificate(uri, &key.PublicKey, issuer, issuerKey, now, notAfter)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: marshal private key", errorx.KindInternal)
	}
	return &Certificate{
		subject:   subject,
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
		return authority, nil
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
			return s.store.Get(ctx, orgGRN)
		}
		return nil, err
	}
	return authority, nil
}

// newAuthority generates an organization's CA with its private key
// sealed. Nothing returns the key in the clear, so the only reader is
// open below.
func (s *service) newAuthority(ctx context.Context, orgGRN grn.GRN) (*Authority, error) {
	key, err := generateKey()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	der, err := createAuthorityCertificate(orgGRN, key, now)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: marshal authority key", errorx.KindInternal)
	}
	sealed, err := s.sealer.Seal(ctx, keyDER)
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

// open unseals the CA's private key and parses its certificate. A
// stored value that no longer parses is corruption, hence internal.
func (s *service) open(ctx context.Context, a *Authority) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certificate, err := x509.ParseCertificate(a.certificate)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "ca: authority certificate", errorx.KindInternal)
	}
	keyDER, err := s.sealer.Open(ctx, a.sealedKey)
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
// which is the CA that may sign for it.
func orgGRNOf(subject grn.GRN) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, serviceTypeOrg, subject.Organization)
}
