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
	// SetAside holds the links the service refused for good, whose
	// marks the pass kept and recorded as set aside.
	SetAside []LinkRefusal
}

// LinkState is where the marks of a role link stand in the outbox.
type LinkState int

const (
	// LinkApplied is no mark: the service applied every link asked of
	// it.
	LinkApplied LinkState = iota
	// LinkPending is a mark waiting for a pass, and none set aside.
	LinkPending
	// LinkSetAside is a mark the service refused for good, which no
	// pass delivers.
	LinkSetAside
)

// Standing is what the outbox holds, as an operator reads it.
type Standing struct {
	// Pending is the marks a pass will read.
	Pending int64
	// SetAside is the marks the service refused for good.
	SetAside int64
}

// Store runs a delivery pass over the outbox.
type Store interface {
	// Deliver runs one pass in one transaction (ADR-0140 item 7): it
	// does nothing unless the feed is on and no other replica holds it,
	// reads up to limit marks that are not set aside, and hands their
	// state to sender under a sequence taken for the pass. It deletes
	// each mark still at the generation it read, except a link the
	// sender set aside, which it records on the mark, and commits. An
	// error from sender, or any other, rolls the pass back and leaves
	// every mark.
	Deliver(ctx context.Context, limit int, sender Sender) (Result, error)
	// LinkState reports where the outbox's marks of the role link of
	// user in org stand, or of any member of org where user is empty.
	// A mark is deleted only after the service applied it, so no mark
	// is a link applied (ADR-0146 §Reading one's own write); one set
	// aside outranks one pending, because it will not clear.
	LinkState(ctx context.Context, org, user string) (LinkState, error)
	// Standing counts the outbox's marks by whether a pass will read
	// them.
	Standing(ctx context.Context) (Standing, error)
}
