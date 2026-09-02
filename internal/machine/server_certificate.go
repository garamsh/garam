package machine

import "crypto/tls"

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
