package policyfeed

import (
	"context"
	"slices"
	"strings"
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

// ReasonChangeRefused is the reason of the unavailable answer a
// request gets when the policy service refused the role link of its
// change for good (api/openapi.yaml `Error.reason`). Unlike
// ReasonChangeNotApplied, nothing the loop does will apply it.
const ReasonChangeRefused = "change_refused"

// ErrLinkSetAside answers a change whose role link the service refused
// with a code no retry clears, so the link is set aside and no wait
// ends it (ADR-0160). The change is committed, and the answer is at
// once: waiting would not change it. Unlike ErrNotApplied it does not
// say that delivery goes on: the member's role links were removed with
// the refusal, so the service holds no role for the member until the
// membership is written again or an operator restarts the feed, and
// the member is denied meanwhile.
var ErrLinkSetAside = errorx.NewWithReason(errorx.KindUnavailable,
	errorx.Reason{Code: ReasonChangeRefused},
	"the policy service refused the role link and it is set aside")

// Gate holds a request until the role links its change marked have
// been applied.
type Gate interface {
	// Await wakes the delivery loop and returns once the outbox holds
	// no mark of the role link of user in org, or of any member of org
	// where user is empty. It returns ErrNotApplied at the bound, and
	// ErrLinkSetAside as soon as a mark is set aside.
	Await(ctx context.Context, org, user string) error
	// AwaitScoped wakes the loop and returns once the outbox holds no
	// mark of the scoped links of the group or the project owner. It
	// returns ErrNotApplied at the bound, and ErrLinkSetAside as soon
	// as the mark is set aside.
	AwaitScoped(ctx context.Context, owner string) error
	// AwaitEntity wakes the loop and returns once the outbox holds no
	// mark of the entity, or ErrNotApplied at the bound.
	AwaitEntity(ctx context.Context, entity Entity) error
	// AwaitMarks wakes the loop and returns once the outbox holds no
	// mark of any of marks, each named by the kind and identifier it
	// was written under, or at once where there are none. It returns
	// ErrNotApplied at the bound, and ErrLinkSetAside as soon as a
	// link among them is set aside (ADR-0165 item 2).
	AwaitMarks(ctx context.Context, marks []Entity) error
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

// NewObservingGate constructs a gate over the outbox store for a
// process that runs no delivery loop: it wakes nothing and reads the
// outbox until another process's loop has delivered (ADR-0165 item 6).
func NewObservingGate(store Store, bound time.Duration) Gate {
	return &gate{store: store, wake: func() {}, bound: bound}
}

func (g *gate) AwaitMarks(ctx context.Context, marks []Entity) error {
	if len(marks) == 0 {
		return nil
	}
	// A mark seen absent stays settled for this wait, so a tick reads
	// only the marks still pending: a request that names many reads
	// fewer as the loop delivers them.
	pending := slices.Clone(marks)
	return g.wait(ctx, "await marks", func() (bool, error) {
		stillPending := pending[:0]
		for _, m := range pending {
			state, err := g.markState(ctx, m)
			switch {
			case err != nil:
				return false, err
			case state == LinkSetAside:
				return false, ErrLinkSetAside
			case state != LinkApplied:
				stillPending = append(stillPending, m)
			}
		}
		pending = stillPending
		return len(pending) == 0, nil
	})
}

// markState reads where the outbox's mark of m stands, by the read its
// kind has.
func (g *gate) markState(ctx context.Context, m Entity) (LinkState, error) {
	switch m.Type {
	case RoleLinkType:
		org, user, _ := strings.Cut(m.ID, " ")
		return g.store.LinkState(ctx, org, user)
	case ScopedRoleLinkType:
		return g.store.ScopedLinkState(ctx, m.ID)
	}
	pending, err := g.store.EntityPending(ctx, m)
	if pending {
		return LinkPending, err
	}
	return LinkApplied, err
}

func (g *gate) AwaitEntity(ctx context.Context, entity Entity) error {
	return g.wait(ctx, "await entity", func() (bool, error) {
		pending, err := g.store.EntityPending(ctx, entity)
		return !pending, err
	})
}

func (g *gate) AwaitScoped(ctx context.Context, owner string) error {
	return g.wait(ctx, "await scoped role link", func() (bool, error) {
		state, err := g.store.ScopedLinkState(ctx, owner)
		switch {
		case err != nil:
			return false, err
		case state == LinkSetAside:
			return false, ErrLinkSetAside
		}
		return state == LinkApplied, nil
	})
}

func (g *gate) Await(ctx context.Context, org, user string) error {
	return g.wait(ctx, "await role link", func() (bool, error) {
		state, err := g.store.LinkState(ctx, org, user)
		switch {
		case err != nil:
			return false, err
		case state == LinkSetAside:
			return false, ErrLinkSetAside
		}
		return state == LinkApplied, nil
	})
}

// wait wakes the loop and polls settled until it reports true or
// fails, and returns ErrNotApplied at the bound.
func (g *gate) wait(ctx context.Context, what string, settled func() (bool, error)) error {
	g.wake()
	deadline := time.NewTimer(g.bound)
	defer deadline.Stop()
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		done, err := settled()
		if err != nil || done {
			return err
		}
		select {
		case <-ctx.Done():
			return errorx.Wrap(ctx.Err(), what, errorx.KindCanceled)
		case <-deadline.C:
			return ErrNotApplied
		case <-tick.C:
		}
	}
}
