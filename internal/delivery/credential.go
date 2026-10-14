package delivery

// Credential is what authenticates this adapter's calls to one peer:
// the agent certificate its connections present, or a token file
// read before every call (ADR-0100). The two are the adapter's
// certificate and token modes, and a call takes its contract from the
// mode.
type Credential interface {
	// Authorization answers what a call's Authorization header
	// carries, read now: empty for a certificate, which the connection
	// carries instead. A token that cannot be read whole is
	// KindUnavailable, so the call is not made and is retried.
	Authorization() (string, error)
	// Fingerprint answers a digest of what the next call presents.
	Fingerprint() (string, error)
	// IsToken reports the token mode.
	IsToken() bool
}
