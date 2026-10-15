package ca

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Certificate lifetime bounds. There is no CRL and no OCSP (ADR-0014),
// so for an agent's certificate the lifetime is the whole window a
// compromised one stays usable in, and an unbounded lifetime is that
// mechanism switched off rather than a setting. An operator's has a
// second end — deleting the operator refuses it from the next
// connection (ADR-0070) — which shortens the window a deployment
// notices in and does not move these bounds: nothing here reads a
// record, and a compromise nobody sees still runs to expiry. What the
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
	// Issue returns a client certificate for subject over a key pair
	// generated here, signed by the CA of the organization that owns
	// it. The organization's CA is created on the first certificate it
	// needs.
	Issue(ctx context.Context, subject grn.GRN) (*IssuedCertificate, error)
	// Sign returns a client certificate for subject over the public key
	// carried by request, a PEM-encoded PKCS#10 certificate signing
	// request. Only that key is read from it: what the certificate
	// names is the caller's argument, so a request asking for another
	// identity is signed for the one named here and for no other
	// (ADR-0064).
	Sign(ctx context.Context, subject grn.GRN, request []byte) (*Certificate, error)
	// IssuerPEM returns the authority that signs for subject now, in
	// PEM form: the issuer a certificate Issue or Sign returns for
	// subject carries. It creates no authority, so an organization
	// that has none is ErrAuthorityNotFound.
	IssuerPEM(ctx context.Context, subject grn.GRN) ([]byte, error)
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

func (s *service) Issue(ctx context.Context, subject grn.GRN) (*IssuedCertificate, error) {
	key, err := generateKey()
	if err != nil {
		return nil, err
	}
	certificate, err := s.sign(ctx, subject, &key.PublicKey)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: marshal private key", errorx.KindInternal)
	}
	return &IssuedCertificate{
		Certificate: *certificate,
		keyPEM:      encodePEM("PRIVATE KEY", keyDER),
	}, nil
}

func (s *service) Sign(ctx context.Context, subject grn.GRN, request []byte) (*Certificate, error) {
	public, err := requestedKey(request)
	if err != nil {
		return nil, err
	}
	return s.sign(ctx, subject, public)
}

func (s *service) IssuerPEM(ctx context.Context, subject grn.GRN) ([]byte, error) {
	authority, err := s.store.Get(ctx, grn.Org(subject.Organization))
	if err != nil {
		return nil, err
	}
	authority, err = ownedBy(authority, grn.Org(subject.Organization))
	if err != nil {
		return nil, err
	}
	return encodePEM("CERTIFICATE", authority.Certificate), nil
}

// sign issues the certificate that names subject over public, whoever
// generated the key behind it.
func (s *service) sign(ctx context.Context, subject grn.GRN, public *ecdsa.PublicKey) (*Certificate, error) {
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
	authority, err := s.authorityOf(ctx, grn.Org(subject.Organization))
	if err != nil {
		return nil, err
	}
	issuer, issuerKey, err := s.open(ctx, authority)
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
	der, err := createCertificate(serial, uri, public, issuer, issuerKey, now, notAfter)
	if err != nil {
		return nil, err
	}
	return &Certificate{
		subject:   subject,
		serial:    serial.String(),
		certPEM:   encodePEM("CERTIFICATE", der),
		issuerPEM: encodePEM("CERTIFICATE", authority.Certificate),
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
	case a.Organization != orgGRN:
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
		Organization: orgGRN,
		Certificate:  der,
		SealedKey:    sealed,
		NotAfter:     now.Add(authorityLifetime),
		CreatedAt:    now,
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
	certificate, err := x509.ParseCertificate(a.Certificate)
	if err != nil {
		return nil, nil, errorx.Wrap(err, "ca: authority certificate", errorx.KindInternal)
	}
	keyDER, err := s.sealer.Open(ctx, binding(a.Organization), a.SealedKey)
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
