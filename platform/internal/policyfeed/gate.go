package policyfeed

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/errorx"
)

// Bound is how long a request waits for the policy service to apply
// the role link its change marked (ADR-0146 §Reading one's own write).
const Bound = 5 * time.Second

// pollEvery is how often a waiter reads the outbox. It waits on the
// table rather than on a signal, so a link delivered by another
// replica's loop ends the wait too.
const pollEvery = 50 * time.Millisecond

// ReasonChangeNotApplied is the reason of the unavailable answer a
// request gets when its change is stored and the policy service has
// not yet applied it (api/openapi.yaml `Error.reason`).
const ReasonChangeNotApplied = "change_not_applied"

// ErrNotApplied answers a change whose role link the service did not
// apply within the bound. The change is committed: the loop goes on
// delivering it, and until it does the service holds the link as it
// was, so the member is decided by the role the service last had.
var ErrNotApplied = errorx.NewWithReason(errorx.KindUnavailable,
	errorx.Reason{Code: ReasonChangeNotApplied},
	"the change is stored and the policy service has not yet applied it")

// ErrLinkSetAside answers a change whose role link the service refused
// for good, so the link is set aside and no wait ends it (ADR-0159). It
// is ErrNotApplied with the cause said, and answers at once: the
// request is told what the wait could not change, which is that the
// change is stored and the service does not hold it. The member stays
// decided by the role the service last had, until the membership is
// written again or an operator restarts the feed.
var ErrLinkSetAside = errorx.Wrap(ErrNotApplied,
	"the policy service refused the role link and it is set aside")

// Gate holds a request until the role links its change marked have
// been applied.
type Gate interface {
	// Await wakes the delivery loop and returns once the outbox holds
	// no mark of the role link of user in org, or of any member of org
	// where user is empty. It returns ErrNotApplied at the bound, and
	// ErrLinkSetAside, which is one, as soon as a mark is set aside.
	Await(ctx context.Context, org, user string) error
}

type gate struct {
	store Store
	wake  func()
	bound time.Duration
}

// NewGate constructs a gate over the outbox store, waking svc's loop.
func NewGate(store Store, svc Service, bound time.Duration) Gate {
	return &gate{store: store, wake: svc.Wake, bound: bound}
}

func (g *gate) Await(ctx context.Context, org, user string) error {
	g.wake()
	deadline := time.NewTimer(g.bound)
	defer deadline.Stop()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		state, err := g.store.LinkState(ctx, org, user)
		switch {
		case err != nil:
			return err
		case state == LinkSetAside:
			return ErrLinkSetAside
		case state == LinkApplied:
			return nil
		}
		select {
		case <-ctx.Done():
			return errorx.Wrap(ctx.Err(), "await role link", errorx.KindCanceled)
		case <-deadline.C:
			return ErrNotApplied
		case <-tick.C:
		}
	}
}
