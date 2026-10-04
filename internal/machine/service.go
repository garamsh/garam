package machine

import (
	"context"
	"crypto/x509"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Service authenticates the machine identity a client certificate
// names.
type Service interface {
	// Authenticate returns the identity chain proves, or refuses it.
	// chain is a presented certificate chain in DER form, leaf first,
	// as TLS hands it over.
	Authenticate(ctx context.Context, chain [][]byte) (Identity, error)
	// Verify refuses a certificate presented on another party's behalf
	// unless it names subject, chains to the authority of subject's
	// organization, is valid now, and names an identity the
	// organization still holds a record of. A certificate that does not
	// prove subject is invalid-argument, one outside its validity
	// permission-denied, a withdrawn record not-found, and a trust root
	// or record that could not be read unavailable.
	Verify(ctx context.Context, subject grn.GRN, leaf *x509.Certificate) error
	// Recheck refuses an identity Authenticate returned earlier whose
	// certificate is no longer valid now, as permission-denied, or whose
	// record has since been withdrawn, as not-found; a record that could
	// not be read is unavailable. A connection authenticates once, when
	// it is made, and this is what a decision on it asks again.
	Recheck(ctx context.Context, identity Identity) error
}

// Config holds where verification reads the time from.
type Config struct {
	// Now supplies the current time, so a test can hold it still. Zero
	// takes time.Now.
	Now func() time.Time
}

type service struct {
	roots         TrustRoot
	registrations Registration
	now           func() time.Time
}

// NewService constructs the authentication service over the trust
// roots it verifies against and the records it admits against.
func NewService(roots TrustRoot, registrations Registration, cfg Config) Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &service{roots: roots, registrations: registrations, now: now}
}

func (s *service) Authenticate(ctx context.Context, chain [][]byte) (Identity, error) {
	if len(chain) == 0 {
		return Identity{}, errorx.New(errorx.KindUnauthenticated, "machine: no client certificate")
	}
	// The leaf carries the identity and TLS puts it first. Anything
	// presented beside it is ignored rather than trusted: an
	// organization's CA signs leaves only, so a chain needing an
	// intermediate is a chain that authority did not issue.
	leaf, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return Identity{}, errorx.Wrap(err, "machine: client certificate", errorx.KindUnauthenticated)
	}
	identity, err := identityOf(leaf)
	if err != nil {
		return Identity{}, err
	}
	root, err := s.rootOf(ctx, identity.grn)
	if err != nil {
		return Identity{}, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: s.now(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return Identity{}, errorx.Wrap(err, "machine: certificate does not chain to its organization's authority", errorx.KindUnauthenticated)
	}
	if err := s.held(ctx, identity.grn); err != nil {
		return Identity{}, err
	}
	return identity, nil
}

func (s *service) Verify(ctx context.Context, subject grn.GRN, leaf *x509.Certificate) error {
	// The SAN is compared before anything is read, so a certificate
	// naming another identity learns nothing about that identity's
	// organization.
	identity, err := identityOf(leaf)
	if err != nil || identity.grn != subject {
		return ErrNotProven
	}
	root, err := s.rootOf(ctx, subject)
	if errorx.IsKind(err, errorx.KindUnauthenticated) {
		return errorx.Wrap(err, ErrNotProven.Error(), errorx.KindInvalidArgument)
	}
	if err != nil {
		return errorx.Wrap(err, "machine: trust root", errorx.KindUnavailable)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	// The chain is verified at a time inside the leaf's validity, so a
	// refusal here is the signature's and never the clock's; the clock
	// is Recheck's, at now.
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: within(leaf, s.now()),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return errorx.Wrap(err, ErrNotProven.Error(), errorx.KindInvalidArgument)
	}
	return s.Recheck(ctx, identity)
}

func (s *service) Recheck(ctx context.Context, identity Identity) error {
	now := s.now()
	if now.Before(identity.notBefore) || now.After(identity.notAfter) {
		return ErrOutsideValidity
	}
	err := s.registrations.Holds(ctx, identity.grn)
	if err == nil || errorx.IsKind(err, errorx.KindNotFound) {
		return err
	}
	return errorx.Wrap(err, "machine: registration", errorx.KindUnavailable)
}

// within returns now held inside the certificate's validity period.
func within(leaf *x509.Certificate, now time.Time) time.Time {
	if now.Before(leaf.NotBefore) {
		return leaf.NotBefore
	}
	if now.After(leaf.NotAfter) {
		return leaf.NotAfter
	}
	return now
}

// held returns nil where the organization still holds its record of
// subject. A certificate that chains is not enough on its own: the
// organization can withdraw the record, and from then on the
// certificate names an identity garam does not admit (ADR-0070).
//
// It runs after the chain is verified rather than before it, so a
// certificate no authority signed is refused without reading anything
// about whom this deployment holds records for.
//
// The refusal is the same one an unverifiable chain gets, for the same
// reason [service.rootOf]'s is: the caller learns its certificate did
// not authenticate and nothing about which identities exist.
func (s *service) held(ctx context.Context, subject grn.GRN) error {
	if err := s.registrations.Holds(ctx, subject); err != nil {
		if errorx.IsKind(err, errorx.KindNotFound) {
			return errorx.Wrap(err, "machine: the organization holds no record of this identity", errorx.KindUnauthenticated)
		}
		return err
	}
	return nil
}

// rootOf returns the parsed certificate of the authority that may sign
// for subject. An organization with no authority has signed nothing,
// which is a refusal rather than a missing row — the caller learns that
// its certificate did not authenticate and nothing about which
// organizations exist.
func (s *service) rootOf(ctx context.Context, subject grn.GRN) (*x509.Certificate, error) {
	der, err := s.roots.Get(ctx, subject)
	if err != nil {
		if errorx.IsKind(err, errorx.KindNotFound) {
			return nil, errorx.Wrap(err, "machine: the organization has no certificate authority", errorx.KindUnauthenticated)
		}
		return nil, err
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, errorx.Wrap(err, "machine: authority certificate", errorx.KindInternal)
	}
	return root, nil
}

// identityOf reads the identity a certificate names from its SAN URI,
// and the certificate's own dates and serial beside it.
//
// The whole URI goes through [grn.Parse], and that one parse is the
// whole check. Exactly one URI is required: a certificate naming none,
// or naming a second identity beside the first, has no single answer to
// who is calling.
func identityOf(leaf *x509.Certificate) (Identity, error) {
	if len(leaf.URIs) != 1 {
		return Identity{}, errorx.New(errorx.KindUnauthenticated, "machine: certificate does not name exactly one identity")
	}
	subject, err := grn.Parse(leaf.URIs[0].String())
	if err != nil {
		return Identity{}, errorx.Wrap(err, "machine: san uri is not a grn", errorx.KindUnauthenticated)
	}
	return Identity{
		grn:       subject,
		serial:    leaf.SerialNumber.String(),
		notBefore: leaf.NotBefore,
		notAfter:  leaf.NotAfter,
	}, nil
}
