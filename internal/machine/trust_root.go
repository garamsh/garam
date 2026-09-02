package machine

import (
	"context"

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
