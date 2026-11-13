package policyfeed

import "context"

// BatchLimit is how many marks one pass reads (ADR-0140 item 7).
const BatchLimit = 500

// Result is what one pass did.
type Result struct {
	// Marks is how many marks the pass read.
	Marks int
	// Refused holds the entries the service refused, whose marks the
	// pass deleted after sending each entity's delete (ADR-0140
	// item 11).
	Refused []Refusal
}

// Store runs a delivery pass over the outbox.
type Store interface {
	// Deliver runs one pass in one transaction (ADR-0140 item 7): it
	// does nothing unless the feed is on and no other replica holds it,
	// reads up to limit marks, and hands their state to sender under a
	// sequence taken for the pass. It deletes each mark still at the
	// generation it read, and commits. An error from sender, or any
	// other, rolls the pass back and leaves every mark.
	Deliver(ctx context.Context, limit int, sender Sender) (Result, error)
	// LinkPending reports whether the outbox still holds a mark of the
	// role link of user in org, or of any member of org where user is
	// empty. A mark is deleted only after the service applied it, so
	// no mark is a link applied (ADR-0146 §Reading one's own write).
	LinkPending(ctx context.Context, org, user string) (bool, error)
}
