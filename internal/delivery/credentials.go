package delivery

// Credentials is what the deployment mounted for the agent, read afresh
// on each look so a replaced file is seen without a restart. A changed
// answer from either is what re-arms an activation a definite refusal
// paused (ADR-0084).
type Credentials interface {
	// PlacementToken answers the placement token as mounted now.
	PlacementToken() (string, error)
	// LeafSerial answers the serial of the agent certificate the next
	// handshake presents, which renewal changes.
	LeafSerial() (string, error)
}
