package delivery

// Credential is what authenticates this adapter's calls to one peer: a
// token file read before every call (ADR-0100, ADR-0118).
type Credential interface {
	// Authorization answers what a call's Authorization header
	// carries, read now. A token that cannot be read whole is
	// KindUnavailable, so the call is not made and is retried.
	Authorization() (string, error)
	// Fingerprint answers a digest of what the next call presents.
	Fingerprint() (string, error)
}
