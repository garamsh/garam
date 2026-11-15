package policyfeed

import (
	"context"
	"sync"
)

// Raised collects the marks a request's own transaction raised, for a
// request whose write cascades to marks no handler can name before it
// runs (ADR-0165 item 3).
type Raised struct {
	mu       sync.Mutex
	entities []Entity
}

type raisedKey struct{}

// WithRaised returns ctx carrying a collection a store fills, and the
// collection. A store that finds none in its context collects nothing.
func WithRaised(ctx context.Context) (context.Context, *Raised) {
	r := &Raised{}
	return context.WithValue(ctx, raisedKey{}, r), r
}

// RaisedIn returns the collection ctx carries, or nil.
func RaisedIn(ctx context.Context) *Raised {
	r, _ := ctx.Value(raisedKey{}).(*Raised)
	return r
}

// Add records marks read in the transaction that raised them.
func (r *Raised) Add(entities ...Entity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entities = append(r.entities, entities...)
}

// Entities returns what was recorded.
func (r *Raised) Entities() []Entity {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Entity(nil), r.entities...)
}
