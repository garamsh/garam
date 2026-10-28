package watch

import (
	"context"

	"github.com/garamsh/garam/platform/internal/grn"
)

// Relay carries an announced event to the processes holding streams. It
// stores nothing, nobody acknowledges, and an event announced while
// nobody watches reaches no one: garam's browser listener and its
// machine listener are separate processes (ADR-0017), so an event
// arriving at one reaches the other's watchers through this and by no
// other route.
type Relay interface {
	// Announce publishes the event for agent to every process listening.
	// A non-nil fence is the announcing agent's execution fence
	// (ADR-0080): the event is announced only if it admits, and in the
	// transaction it holds, so it commits before any replacement of
	// that execution or not at all. When a listener receives it is not
	// part of that order.
	Announce(ctx context.Context, agent grn.GRN, e Event, fence *Fence) error
	// Receive calls deliver for each announced event until ctx is done.
	// deliver is called from one goroutine.
	Receive(ctx context.Context, deliver func(agent grn.GRN, e Event)) error
}
