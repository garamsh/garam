package delivery

// Credentials is what the deployment mounted for the agent, read afresh
// on each look so a replaced file is seen without a restart. A changed
// answer from either is what re-arms an activation a definite refusal
// paused (ADR-0084): it asks again, and the answer decides.
type Credentials interface {
	// PlacementToken answers the placement token as mounted now.
	PlacementToken() (string, error)
	// Fingerprint answers a digest of what the agent's calls present
	// now — each token file's hash — which a rewritten token file
	// changes.
	Fingerprint() (string, error)
}
