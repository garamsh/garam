// Package policyfeed feeds the policy service from cowork's dirty set
// of entities (ADR-0140): it switches the feed on and off, and
// delivers what cowork's tables say each marked entity is, to the
// service as the organization it sits in. What the feed holds, and the
// triggers that fill it, are migration 000006's.
package policyfeed

import "context"

// Switch turns the feed on and off.
type Switch interface {
	// Start enables the feed and marks every live entity, in one
	// transaction that waits out the writers of the source tables. It
	// returns how many entities the outbox then holds.
	Start(ctx context.Context) (marked int64, err error)
	// Stop disables the feed and empties the outbox. It returns how
	// many marks it removed.
	Stop(ctx context.Context) (removed int64, err error)
	// Enabled reports whether the feed is on.
	Enabled(ctx context.Context) (bool, error)
}

// Entity names an entity cowork owns: Type is the GRN type cowork's
// service gives it (channel, page, task, artifact), Org the
// organization it is in and ID its identifier there.
type Entity struct {
	Type string
	Org  string
	ID   string
}

// Entry is one entity a pass delivers. Exists is false where cowork's
// tables no longer hold it, which is a delete.
type Entry struct {
	Entity Entity
	Exists bool
}

// Refusal is an entry the policy service refused for its own fault.
type Refusal struct {
	Entity Entity
	Reason string
}

// Sender writes entries to the policy service.
type Sender interface {
	// Send applies entries, all of org, at sequence, which is above
	// every sequence cowork has sent for any entity in them, under
	// org's own caller token. It returns the entries the service
	// refused for their own fault; a fault of the call itself is the
	// error.
	Send(ctx context.Context, org string, sequence int64, entries []Entry) ([]Refusal, error)
}

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
	// reads up to limit marks, and hands their state to sender, one
	// organization at a time under a sequence taken for each. It
	// deletes each mark still at the generation it read, and commits.
	// An error from sender, or any other, rolls the pass back and
	// leaves every mark.
	Deliver(ctx context.Context, limit int, sender Sender) (Result, error)
}
