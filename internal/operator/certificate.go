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
// verifies garam by: the client certificate, the private key it was
// issued with, the authority that signed it, and the garam server root,
// each in PEM form (ADR-0014, ADR-0040). garam stores no private key,
// so this value carries the only copy.
//
// Serial names this certificate, and it is what the operator presents
// on the renewal that replaces it.
type Certificate struct {
	Serial         Serial
	CertificatePEM []byte
	PrivateKeyPEM  []byte
	IssuerPEM      []byte
	ServerRootPEM  []byte
	NotAfter       time.Time
}

// Current is the certificate garam last issued for an operator: the
// record the renewal rule reads and the successor moves.
type Current struct {
	Operator grn.GRN
	Serial   Serial
	IssuedAt time.Time
}
