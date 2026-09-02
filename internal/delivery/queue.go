package delivery

import "context"

// Queue is garam's side of the loop: the claim this adapter holds for
// its agent, the renewal that keeps it, and the completion that ends
// it. Every call is for the one agent the adapter runs beside, which is
// why none of them names it.
type Queue interface {
	// Claim waits for a message addressed to the agent and returns the
	// hold garam gave it, or ErrNoMessageReady when the wait ran out.
	Claim(ctx context.Context) (*Claim, error)
	// Renew carries the hold forward and answers with it as it now
	// stands, or ErrClaimLapsed when it is gone.
	Renew(ctx context.Context, id ClaimID) (*Claim, error)
	// Complete reports the claimed message delivered, or answers
	// ErrClaimLapsed when the hold was handed on.
	Complete(ctx context.Context, id ClaimID) error
}
