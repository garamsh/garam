package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// SignedCertificate is a certificate signed over a key its holder
// generated, so it carries no private key: the certificate, the
// authority that signed it and the garam server root the holder
// verifies garam by (ADR-0040).
type SignedCertificate struct {
	Credential     Credential
	CertificatePEM string
	IssuerPEM      string
	ServerRootPEM  string
}

// RequestSigner signs a certificate for subject over the public key a
// PEM certificate signing request carries. Only the key is read from
// the request; the subject is the argument. A request that does not
// prove an ECDSA P-256 key is ErrInvalidCertificateRequest.
type RequestSigner interface {
	SignRequest(ctx context.Context, subject grn.GRN, requestPEM []byte) (*SignedCertificate, error)
	// Chain answers the IssuerPEM and ServerRootPEM a certificate
	// signed for subject now would carry, signing nothing.
	Chain(ctx context.Context, subject grn.GRN) (*Chain, error)
}

// Chain is what a certificate an agent holds chains to: the authority
// that signed it and the garam server root the holder verifies garam
// by (ADR-0040).
type Chain struct {
	IssuerPEM     string
	ServerRootPEM string
}
