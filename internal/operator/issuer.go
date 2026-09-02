package operator

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

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
