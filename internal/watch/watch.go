// Package watch carries what an agent is doing now to the people
// watching it (ADR-0022), and keeps nothing of it.
//
// A watch is a subscription a browser opens and closes, not a setting:
// an agent nobody is looking at has no row here, no event carried and no
// entry in any process's memory. What is carried is best effort and
// never a record — the platform the events come from drops them for a
// subscriber that falls behind and marks no gap, so anything that has to
// be true afterwards comes from the queue instead (ADR-0013).
package watch

import (
	"strconv"
	"strings"

	"github.com/garamsh/garam/internal/errorx"
)

// WatchID identifies one open watch. It is the key its lease is written
// and deleted under, and it names nothing outside this package.
type WatchID string

// What one event may carry. The bounds are on what a forwarder may send
// rather than on what a browser may read: an event travels to the
// process holding the stream in one announcement, and one too large to
// carry is refused rather than shortened.
const (
	// MaxKindLength bounds the platform's name for an event.
	MaxKindLength = 64
	// MaxBodyLength bounds the event itself, in characters. Ordinary
	// text of this length always fits an announcement; content that
	// encodes to several bytes a character may not, and is refused where
	// it is carried rather than here.
	MaxBodyLength = 4000
)

// Sentinels this package answers with.
var (
	// ErrNotAgent refuses a caller acting on the events of an agent
	// other than the one it authenticates as.
	ErrNotAgent = errorx.New(errorx.KindPermissionDenied, "caller is not the agent")
	// ErrWatchLapsed reports a watch that is no longer counted, because
	// its lease ran out or it was closed. The holder of the stream ends
	// it: an open response nobody forwards to shows silence that reads
	// as an agent saying nothing.
	ErrWatchLapsed = errorx.New(errorx.KindFailedPrecondition, "watch has lapsed")
)

// Event is one turn event as an adapter forwarded it. garam carries it
// and never reads it, so what a kind means and what a body holds are the
// agent platform's to say.
type Event struct {
	kind string
	body string
}

// NewEvent returns the event, or refuses one that cannot be framed or
// carried. The kind reaches a browser as the name of a server-sent
// event, which is one line by construction, so a kind spanning more than
// one would arrive as a different event or as none.
func NewEvent(kind, body string) (Event, error) {
	switch {
	case kind == "":
		return Event{}, errorx.New(errorx.KindInvalidArgument, "event kind is required")
	case strings.ContainsAny(kind, "\r\n"):
		return Event{}, errorx.New(errorx.KindInvalidArgument, "event kind must be one line")
	case len([]rune(kind)) > MaxKindLength:
		return Event{}, errorx.New(errorx.KindInvalidArgument,
			"event kind must be at most "+strconv.Itoa(MaxKindLength)+" characters")
	case len([]rune(body)) > MaxBodyLength:
		return Event{}, errorx.New(errorx.KindInvalidArgument,
			"event body must be at most "+strconv.Itoa(MaxBodyLength)+" characters")
	}
	return Event{kind: kind, body: body}, nil
}

// Kind is what the platform called this event.
func (e Event) Kind() string { return e.kind }

// Body is the event as the platform emitted it. It may be empty and it
// may span lines.
func (e Event) Body() string { return e.body }
