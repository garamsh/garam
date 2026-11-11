// Package policyfeed feeds the policy service from the platform's
// dirty set of entities (ADR-0140): it switches the feed on and off,
// and delivers what the owners' tables say each marked entity is. What
// the feed holds, and the triggers that fill it, are migrations 000067
// and 000068's.
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

// Entity names an entity of the policy service's schema.
type Entity struct {
	Type string
	ID   string
}

// State is what the owners' tables say an entity is: where it sits and
// what it belongs to, and nothing else (ADR-0138 item 6).
type State struct {
	// Org is the organization an entity of a resource type sits in.
	Org *Entity
	// Members is the OrgMembers an Org names as its own.
	Members *Entity
	Parents []Entity
}

// Entry is one entity a pass delivers.
type Entry struct {
	Entity Entity
	// State is nil where the owners hold no such entity, which is a
	// delete.
	State *State
}

// Refusal is an entry the policy service refused for its own fault.
type Refusal struct {
	Entity Entity
	Reason string
}
