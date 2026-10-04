package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// SignedCertificate is a certificate signed over a key its holder
// generated, so it carries no private key.
type SignedCertificate struct {
	Credential     Credential
	CertificatePEM string
}

// RequestSigner signs a certificate for subject over the public key a
// PEM certificate signing request carries. Only the key is read from
// the request; the subject is the argument.
type RequestSigner interface {
	SignRequest(ctx context.Context, subject grn.GRN, requestPEM []byte) (*SignedCertificate, error)
}
