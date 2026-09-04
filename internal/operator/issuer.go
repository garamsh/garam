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
	// Issue returns a certificate for subject over a key pair generated
	// behind the seam, signed by the authority of the organization that
	// owns it.
	Issue(ctx context.Context, subject grn.GRN) (*IssuedCertificate, error)
	// Sign returns a certificate for subject over the key the request
	// carries. What is signed is the subject named here and never the
	// one the request asks for, so a request is a public key and a
	// proof that its sender holds the private half, and nothing else
	// (ADR-0064).
	Sign(ctx context.Context, subject grn.GRN, request CertificateRequest) (*Certificate, error)
}
