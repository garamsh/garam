// Package machine authenticates a machine identity — an agent or an
// operator — by the client certificate it presents (ADR-0014,
// ADR-0015).
//
// What the certificate asserts is the GRN in its SAN URI and nothing
// else: no user row, no session, no organization membership. What it is
// — its serial and its dates — travels beside that GRN, asserting
// nothing and answering the one caller that asks to replace it
// (ADR-0046). A presented certificate is verified against the
// certificate authority of the organization its own SAN names, so a
// certificate another organization's CA signed does not authenticate
// here however valid it is elsewhere — tenancy holds in the trust root
// rather than in a check a caller could forget.
//
// The listener [NewServer] builds terminates TLS itself and asks for a
// client certificate. A connection whose certificate does not
// authenticate carries no [Identity] rather than being refused, which
// is what leaves room for the one route a caller reaches before it has
// a certificate (ADR-0063); every other route refuses a request
// carrying none. Nothing here reads a forwarded client-certificate
// header: one is trustworthy only where every deployment strips it at
// the edge (ADR-0014).
//
// What it presents in the other direction is one certificate every
// machine identity pins, asked for per handshake through
// [ServerCertificate] so a replaced one is served without a restart
// (ADR-0040). That root is not this package's per-organization one:
// garam verifies an identity by its organization's authority, and an
// identity verifies garam by the root every organization shares.
package machine

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Identity is the machine identity a verified client certificate
// names, and which certificate named it. Only [Service.Authenticate]
// constructs one, and the listener carries what it returned rather
// than reading the certificate again, so a handler holding an Identity
// holds a GRN whose certificate chained to its own organization's
// certificate authority.
//
// The GRN is the whole of what the certificate asserts — no roles, no
// claims. The three fields below describe the certificate itself, which
// is what a caller asking to replace it is answered by (ADR-0046).
type Identity struct {
	grn       grn.GRN
	serial    string
	notBefore time.Time
	notAfter  time.Time
}

// GRN returns the identity's GRN, which is the whole of what the
// certificate carries.
func (i Identity) GRN() grn.GRN { return i.grn }

// Serial returns the presented certificate's serial number in decimal,
// which is what tells it from another certificate naming the same
// identity.
func (i Identity) Serial() string { return i.serial }

// NotBefore returns when the presented certificate became valid.
func (i Identity) NotBefore() time.Time { return i.notBefore }

// NotAfter returns when the presented certificate stops being valid.
func (i Identity) NotAfter() time.Time { return i.notAfter }
