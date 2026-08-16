// Package machine authenticates a machine identity — an agent or an
// operator — by the client certificate it presents (ADR-0014,
// ADR-0015).
//
// The identity is the GRN in the certificate's SAN URI and nothing
// else: no user row, no session, no organization membership. A
// presented certificate is verified against the certificate authority
// of the organization its own SAN names, so a certificate another
// organization's CA signed does not authenticate here however valid it
// is elsewhere — tenancy holds in the trust root rather than in a check
// a caller could forget.
//
// The listener [NewServer] builds terminates TLS itself and requires a
// client certificate. Nothing here reads a forwarded
// client-certificate header: one is trustworthy only where every
// deployment strips it at the edge (ADR-0014).
package machine

import "github.com/garamsh/garam/internal/grn"

// Identity is the machine identity a verified client certificate
// names. Only this package constructs one, so a handler holding an
// Identity holds a GRN whose certificate chained to its own
// organization's certificate authority.
type Identity struct {
	grn grn.GRN
}

// GRN returns the identity's GRN, which is the whole of what the
// certificate carries.
func (i Identity) GRN() grn.GRN { return i.grn }
