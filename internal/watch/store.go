package watch

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Store holds the watches that are open and nothing about a turn. It is
// what lets one process answer for another's streams: the count a poll
// carries is read where the poll is served, and the stream it counts is
// held somewhere else (ADR-0017).
//
// Every row is a watch rather than an agent, so an agent nobody is
// watching is absent rather than stored as a zero.
type Store interface {
	// Open records a watch on agent under id, live for lease, and drops
	// the watches whose leases have run out.
	Open(ctx context.Context, id WatchID, agent grn.GRN, lease time.Duration) error
	// Refresh carries a live watch's lease forward, or returns
	// ErrWatchLapsed when id names none.
	Refresh(ctx context.Context, id WatchID, lease time.Duration) error
	// Close ends the watch. A watch already lapsed is closed the same
	// way, so a caller that closes twice reports the same end.
	Close(ctx context.Context, id WatchID) error
	// Count reports how many watches on agent are live.
	Count(ctx context.Context, agent grn.GRN) (int, error)
}
