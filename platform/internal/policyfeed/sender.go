package policyfeed

import "context"

// Sender writes entries to the policy service.
type Sender interface {
	// Send applies entries at sequence, which is above every sequence
	// the platform has sent for any entity in them. It returns the
	// entries the service refused for their own fault; a fault of the
	// call itself is the error.
	Send(ctx context.Context, sequence int64, entries []Entry) ([]Refusal, error)
}
