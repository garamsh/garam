package watch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

const (
	// DefaultLease is how long a watch stays counted without being
	// refreshed. It bounds what a stream nobody is holding any more can
	// cost: at worst an adapter forwards for this long after the browser
	// went away.
	DefaultLease = 45 * time.Second
	// keepAliveDivisor is how many refreshes fall inside one lease. Three
	// means two may be lost — to a paused process or a slow write —
	// before a watch that is still held lapses.
	keepAliveDivisor = 3
	// streamBuffer is how far behind one stream may fall before its
	// events are dropped. A browser reads a turn as fast as an agent
	// speaks it, so the buffer covers a scheduling delay rather than a
	// reader that stopped.
	streamBuffer = 32
)

// Config tunes how long a watch is counted for.
type Config struct {
	// Lease is how long an opened watch stays live without a refresh. A
	// zero field takes DefaultLease.
	Lease time.Duration
}

// Relay carries an announced event to the processes holding streams. It
// stores nothing, nobody acknowledges, and an event announced while
// nobody watches reaches no one: garam's browser listener and its
// machine listener are separate processes (ADR-0017), so an event
// arriving at one reaches the other's watchers through this and by no
// other route.
type Relay interface {
	// Announce publishes the event for agent to every process listening.
	Announce(ctx context.Context, agent grn.GRN, e Event) error
	// Receive calls deliver for each announced event until ctx is done.
	// deliver is called from one goroutine.
	Receive(ctx context.Context, deliver func(agent grn.GRN, e Event)) error
}

// Service is watching an agent, from both ends: a browser opens a
// subscription and reads it, and the adapter beside the agent forwards
// what it is saying and asks how many are watching.
//
// The two ends run in different processes, so no instance holds both
// halves of one watch. Publish and Watchers take the calling identity
// beside the agent they name and answer only where the two are the same
// identity, which is the whole of the authorization on that side. What
// admits a browser is a separate grant, decided where the browser is
// served (ADR-0022).
type Service interface {
	// Watch opens a subscription to what the agent is saying, from now:
	// nothing replays, because nothing was kept. The caller reads
	// Subscription.Events until it closes the subscription.
	Watch(ctx context.Context, agent grn.GRN) (*Subscription, error)
	// Publish hands one forwarded turn event to whoever is watching the
	// agent, and to nobody when no one is. It reports that the event was
	// announced, never that anyone saw it.
	Publish(ctx context.Context, caller, agent grn.GRN, e Event) error
	// Watchers reports how many streams garam is holding for the agent,
	// which is what tells an adapter whether to forward anything at all.
	Watchers(ctx context.Context, caller, agent grn.GRN) (int, error)
	// Receive delivers the events every process announces to the
	// subscriptions this one holds, until ctx is done. The process
	// serving browsers runs it; one that holds no subscription has
	// nothing to deliver to.
	Receive(ctx context.Context) error
}

// service holds one entry per open subscription and none per agent: an
// agent nobody watches is absent from the map rather than present with
// an empty set, which is the property the whole design rests on
// (ADR-0022).
type service struct {
	store Store
	relay Relay
	cfg   Config
	mu    sync.Mutex
	open  map[WatchID]*Subscription
}

// NewService constructs the watch service over the leases it counts and
// the relay it carries events on, or refuses a lease no watch could stay
// live under.
func NewService(store Store, relay Relay, cfg Config) (Service, error) {
	if cfg.Lease == 0 {
		cfg.Lease = DefaultLease
	}
	if cfg.Lease <= 0 {
		return nil, errorx.New(errorx.KindInvalidArgument, "watch: lease must be positive")
	}
	return &service{store: store, relay: relay, cfg: cfg, open: make(map[WatchID]*Subscription)}, nil
}

func (s *service) Watch(ctx context.Context, agent grn.GRN) (*Subscription, error) {
	id, err := newWatchID()
	if err != nil {
		return nil, err
	}
	if err := s.store.Open(ctx, id, agent, s.cfg.Lease); err != nil {
		return nil, err
	}
	sub := &Subscription{svc: s, id: id, agent: agent, events: make(chan Event, streamBuffer)}
	s.mu.Lock()
	s.open[id] = sub
	s.mu.Unlock()
	return sub, nil
}

func (s *service) Publish(ctx context.Context, caller, agent grn.GRN, e Event) error {
	if caller != agent {
		return ErrNotAgent
	}
	return s.relay.Announce(ctx, agent, e)
}

func (s *service) Watchers(ctx context.Context, caller, agent grn.GRN) (int, error) {
	if caller != agent {
		return 0, ErrNotAgent
	}
	return s.store.Count(ctx, agent)
}

func (s *service) Receive(ctx context.Context) error {
	return s.relay.Receive(ctx, s.deliver)
}

// deliver hands the event to the subscriptions on that agent. It walks
// the open watches rather than indexing them by agent: watching is
// exceptional, and an index would be the per-agent state ADR-0022 says
// an unwatched agent must not have.
func (s *service) deliver(agent grn.GRN, e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sub := range s.open {
		if sub.agent == agent {
			sub.offer(e)
		}
	}
}

// Subscription is one open watch: what the agent has said since it
// opened, and the lease that tells another process's poll it is there.
type Subscription struct {
	svc    *service
	id     WatchID
	agent  grn.GRN
	events chan Event
	once   sync.Once
}

// Events is the stream the holder reads. It closes when the
// subscription does.
func (s *Subscription) Events() <-chan Event { return s.events }

// KeepAlive is how often the holder refreshes the watch, and so how
// often it has something to write on a stream that is otherwise quiet.
// One tick proves both: that the watch is still counted, and that the
// connection carrying it still takes bytes.
func (s *Subscription) KeepAlive() time.Duration {
	return s.svc.cfg.Lease / keepAliveDivisor
}

// Refresh carries the watch's lease forward, or returns ErrWatchLapsed
// when it is no longer counted.
func (s *Subscription) Refresh(ctx context.Context) error {
	return s.svc.store.Refresh(ctx, s.id, s.svc.cfg.Lease)
}

// Close ends the watch, closes the stream and drops the lease, so
// nothing is left for the agent the moment its last watcher leaves.
// Closing twice ends it once.
//
// The context is the caller's rather than the request's: a stream ends
// when the request it was served on is over, and the lease still has to
// be deleted.
func (s *Subscription) Close(ctx context.Context) error {
	var err error
	s.once.Do(func() {
		s.svc.mu.Lock()
		delete(s.svc.open, s.id)
		close(s.events)
		s.svc.mu.Unlock()
		err = s.svc.store.Close(ctx, s.id)
	})
	return err
}

// offer hands the event to the stream, or drops it where the stream is
// that far behind. The platform these events come from drops them for a
// subscriber that falls behind and marks no gap (ADR-0013), so a full
// buffer is that same answer one hop later rather than a new failure
// mode — and holding the publisher would make one slow browser every
// watcher's problem.
func (s *Subscription) offer(e Event) {
	select {
	case s.events <- e:
	default:
	}
}

// newWatchID returns an identifier for one watch.
func newWatchID() (WatchID, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", errorx.Wrap(err, "watch id")
	}
	return WatchID(hex.EncodeToString(b)), nil
}
