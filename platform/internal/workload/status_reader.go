package workload

import "context"

// StatusReader reads the status route of a runtime the supervisor
// attaches to, at the loopback literal the host pinned (ADR-0111).
//
// It answers only a status of contract version 1 that names the agent
// and a generation. A runtime that answered anything else, such as
// another agent, another contract version, no generation or a state
// nothing defines, is an error of KindFailedPrecondition. Every other
// error is an answer the supervisor could not read.
type StatusReader interface {
	Status(ctx context.Context) (*RuntimeStatus, error)
}

// RuntimeStatus is what an attached runtime reports of itself: the
// assignment epoch and configuration revision it started with, the
// generation it minted once it held its store, and its state, serving
// or draining.
type RuntimeStatus struct {
	Epoch      string
	Generation string
	Revision   string
	State      string
}
