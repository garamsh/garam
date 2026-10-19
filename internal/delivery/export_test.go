package delivery

// HeldExecution exposes, to the external test package, an execution
// already holding an activation whose token is token: what a queue
// presents once its loop has activated the generation, for a test of
// the queue alone.
func HeldExecution(token Token) *Execution {
	e := NewFencedExecution()
	e.hold(&Activation{ID: "held", TokenVersion: 1, Token: token})
	return e
}
