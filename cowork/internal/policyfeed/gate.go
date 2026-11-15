package policyfeed

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// Bound is how long a request waits for the policy service to hold
// the entity it created, or to drop the one it deleted (ADR-0166).
const Bound = 5 * time.Second

// pollEvery is how often a waiter reads the outbox. It waits on the
// table rather than on a signal, so an entity delivered by another
// replica's loop ends the wait too.
const pollEvery = 50 * time.Millisecond

// ReasonChangeNotApplied is the reason of the unavailable answer a
// request gets when what it created or deleted is stored and the policy
// service does not yet hold it (api/openapi.yaml `Error.reason`).
const ReasonChangeNotApplied = "change_not_applied"

// errNotApplied is the cause of every NotApplied answer.
var errNotApplied = errorx.NewWithReason(errorx.KindUnavailable,
	errorx.Reason{Code: ReasonChangeNotApplied},
	"the change is stored and the policy service has not yet applied it")

// notApplied is errNotApplied naming the resource that is stored.
type notApplied struct{ resource string }

func (e *notApplied) Error() string { return errNotApplied.Error() }

func (e *notApplied) Unwrap() error { return errNotApplied }

// Resource is the GRN of the entity the request stored, which the
// caller reads rather than creating again.
func (e *notApplied) Resource() string { return e.resource }

// Marks reads the outbox for a waiter.
type Marks interface {
	// Pending reports whether the outbox holds a mark of entity.
	Pending(ctx context.Context, entity Entity) (bool, error)
}

// Gate holds a request until the policy service holds the entity it
// created.
type Gate interface {
	// Await wakes the delivery loop and returns once the outbox holds
	// no mark of entity. It returns an error of reason
	// ReasonChangeNotApplied that names the entity's GRN at the bound.
	Await(ctx context.Context, entity Entity) error
}

type gate struct {
	marks Marks
	wake  func()
	bound time.Duration
}

// NewGate constructs a gate over the outbox, waking svc's loop.
func NewGate(marks Marks, svc Service, bound time.Duration) Gate {
	return &gate{marks: marks, wake: svc.Wake, bound: bound}
}

func (g *gate) Await(ctx context.Context, entity Entity) error {
	g.wake()
	deadline := time.NewTimer(g.bound)
	defer deadline.Stop()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		pending, err := g.marks.Pending(ctx, entity)
		if err != nil {
			return err
		}
		if !pending {
			return nil
		}
		select {
		case <-ctx.Done():
			return errorx.Wrap(ctx.Err(), "await entity", errorx.KindCanceled)
		case <-deadline.C:
			return &notApplied{resource: grn.NewIn(entity.Org, "cowork", entity.Type, entity.ID).String()}
		case <-tick.C:
		}
	}
}
