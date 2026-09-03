package operator

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Serial identifies one certificate garam signed, in the decimal form
// `internal/ca` renders a serial number as. It is what distinguishes
// two certificates naming one operator, which is the whole of what the
// renewal rule needs to tell them apart.
type Serial string

// Credential is the certificate a connection authenticated with, as
// what decides whether it may be replaced reads it. It is not evidence
// of anything on its own: the listener verified the chain against the
// organization's authority before a handler saw it (ADR-0014).
type Credential struct {
	Subject   grn.GRN
	Serial    Serial
	NotBefore time.Time
	NotAfter  time.Time
}

// Certificate is what an operator authenticates with and what it
// verifies garam by: the client certificate, the authority that signed
// it, and the garam server root, each in PEM form (ADR-0014,
// ADR-0040). It carries no key, because the key may be one garam never
// saw — an enrolling operator generates its own and sends the request
// garam signs (ADR-0064).
//
// Subject is the identity it names, which the enrollment route answers
// with: the caller learns which operator the token it spent was for.
// Serial names this certificate, and it is what the operator presents
// on the renewal that replaces it.
type Certificate struct {
	Subject        grn.GRN
	Serial         Serial
	CertificatePEM []byte
	IssuerPEM      []byte
	ServerRootPEM  []byte
	NotAfter       time.Time
}

// IssuedCertificate is a Certificate garam generated the key pair for,
// on the two routes that do: the out-of-band mint and the renewal.
// garam stores no private key, so this value carries the only copy.
//
// The key is a field of this type and not of the one above, so that
// what an enrollment answers has no key field to leave empty and
// nothing to render as an empty string.
type IssuedCertificate struct {
	Certificate
	PrivateKeyPEM []byte
}

// CertificateRequest is a PEM-encoded PKCS#10 certificate signing
// request as it arrived, holding the public key an enrolling operator
// generated in its own cluster. It is untrusted until [Issuer] reads
// it: the request names whatever its sender wanted, and what the
// certificate names is the token's to say.
type CertificateRequest []byte

// Current is the certificate garam last issued for an operator: the
// record the renewal rule reads and the successor moves.
type Current struct {
	Operator grn.GRN
	Serial   Serial
	IssuedAt time.Time
}
