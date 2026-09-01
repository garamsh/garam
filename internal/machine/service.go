package machine

import (
	"context"
	"crypto/tls"
	"crypto/x509"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// TrustRoot is where a presented certificate has to chain from: the
// certificate authority of the organization that owns the identity the
// certificate names. It takes the subject rather than its organization
// because which authority may sign for a subject is the tenancy rule
// itself (ADR-0014), and it yields the certificate alone — verifying
// one needs no private key.
type TrustRoot interface {
	// Get returns, in DER form, the CA certificate of the organization
	// that owns subject, or a KindNotFound error when it has none.
	Get(ctx context.Context, subject grn.GRN) ([]byte, error)
}

// ServerCertificate is the certificate the listener presents. It is
// asked once per handshake rather than read once at startup, so a
// replaced certificate is served on the next connection and no process
// restarts (ADR-0040). What it answers with is garam's own identity,
// which every machine identity pins — a different trust root from the
// per-organization one [TrustRoot] answers for the other direction.
type ServerCertificate interface {
	// Get returns the certificate to present, or refuses the handshake.
	Get() (*tls.Certificate, error)
}

// Service authenticates the machine identity a client certificate
// names.
type Service interface {
	// Authenticate returns the identity chain proves, or refuses it.
	// chain is a presented certificate chain in DER form, leaf first,
	// as TLS hands it over.
	Authenticate(ctx context.Context, chain [][]byte) (Identity, error)
}

type service struct {
	roots TrustRoot
}

// NewService constructs the authentication service over the trust
// roots it verifies against.
func NewService(roots TrustRoot) Service {
	return &service{roots: roots}
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
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		return Identity{}, errorx.Wrap(err, "machine: certificate does not chain to its organization's authority", errorx.KindUnauthenticated)
	}
	return identity, nil
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
