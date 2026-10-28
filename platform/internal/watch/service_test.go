package watch_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/watch"
)

// lease is short enough that a test does not wait on it and long enough
// that nothing lapses while one runs.
const lease = 2 * time.Second

func agentGRN(id string) grn.GRN {
	return grn.New("acme", grn.DefaultProject, "agent", id)
}

// leases is a watch.Store in memory: one entry per open watch and none
// per agent, so an agent nobody watches is absent rather than held with
// a count of zero.
type leases struct {
	mu    sync.Mutex
	byID  map[watch.WatchID]grn.GRN
	err   error
	opens int
}

func newLeases() *leases { return &leases{byID: map[watch.WatchID]grn.GRN{}} }

func (l *leases) Open(_ context.Context, id watch.WatchID, agent grn.GRN, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return l.err
	}
	l.opens++
	l.byID[id] = agent
	return nil
}

func (l *leases) Refresh(_ context.Context, id watch.WatchID, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, live := l.byID[id]; !live {
		return watch.ErrWatchLapsed
	}
	return nil
}

func (l *leases) Close(_ context.Context, id watch.WatchID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byID, id)
	return nil
}

func (l *leases) Count(_ context.Context, agent grn.GRN) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return 0, l.err
	}
	count := 0
	for _, watched := range l.byID {
		if watched == agent {
			count++
		}
	}
	return count, nil
}

// held reports how many watches the store holds in total, which is what
// says whether anything was left behind for an agent nobody watches.
func (l *leases) held() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.byID)
}

// announcements is a watch.Relay in one process: an announcement reaches
// whoever registered to receive, and an announcement made while nobody
// is receiving reaches no one and is not kept — which is what Postgres
// NOTIFY does between garam's processes.
type announcements struct {
	mu      sync.Mutex
	deliver func(grn.GRN, watch.Event)
	made    []watch.Event
	ready   chan struct{}
	once    sync.Once
}

func newAnnouncements() *announcements {
	return &announcements{ready: make(chan struct{})}
}

func (a *announcements) Announce(_ context.Context, agent grn.GRN, e watch.Event, _ *watch.Fence) error {
	a.mu.Lock()
	a.made = append(a.made, e)
	deliver := a.deliver
	a.mu.Unlock()
	if deliver != nil {
		deliver(agent, e)
	}
	return nil
}

func (a *announcements) Receive(ctx context.Context, deliver func(grn.GRN, watch.Event)) error {
	a.mu.Lock()
	a.deliver = deliver
	a.mu.Unlock()
	a.once.Do(func() { close(a.ready) })
	<-ctx.Done()
	return nil
}

// count is how many announcements were made.
func (a *announcements) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.made)
}

// receiving returns a service delivering what is announced to the
// subscriptions it holds, as `serve api` runs it.
func receiving(t *testing.T, store watch.Store, relay *announcements) watch.Service {
	t.Helper()
	svc, err := watch.NewService(store, relay, watch.Config{Lease: lease})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = svc.Receive(ctx) }()
	select {
	case <-relay.ready:
	case <-time.After(time.Second):
		t.Fatal("no receiver registered")
	}
	return svc
}

// event returns a valid event of the kind named.
func event(t *testing.T, kind, body string) watch.Event {
	t.Helper()
	e, err := watch.NewEvent(kind, body)
	require.NoError(t, err)
	return e
}

// awaitEvent reads the next event the subscription was handed, or fails.
func awaitEvent(t *testing.T, sub *watch.Subscription) watch.Event {
	t.Helper()
	select {
	case e, open := <-sub.Events():
		require.True(t, open, "the stream closed instead of carrying an event")
		return e
	case <-time.After(time.Second):
		t.Fatal("no event reached the stream")
		return watch.Event{}
	}
}

// assertQuiet fails if anything reaches the subscription. The wait is
// short because what is asserted is that nothing was handed over, and a
// delivery that has not happened by now happened before the call.
func assertQuiet(t *testing.T, sub *watch.Subscription, msg string) {
	t.Helper()
	select {
	case e, open := <-sub.Events():
		if open {
			t.Fatalf("%s: %s reached the stream", msg, e.Kind())
		}
	case <-time.After(50 * time.Millisecond):
	}
}

// TestService_ForwardedEventsReachTheWatchersOfThatAgentOnly walks one
// agent from unwatched to watched and back. The unwatched case is the
// control: the same forward, over the same relay, reaching nobody —
// without it, a stream that received every event ever announced would
// read the same.
func TestService_ForwardedEventsReachTheWatchersOfThatAgentOnly(t *testing.T) {
	ctx := context.Background()
	store := newLeases()
	relay := newAnnouncements()
	svc := receiving(t, store, relay)
	watched := agentGRN("0a1b2c3d4e5f6071")
	other := agentGRN("9f8e7d6c5b4a3021")

	// Nobody is watching: the event is announced and reaches no one, and
	// nothing is held for the agent it was about.
	require.NoError(t, svc.Publish(ctx, watched, watched, event(t, "turn.start", "before anyone looked"), &watch.Fence{Agent: watched}))
	require.Equal(t, 1, relay.count())
	require.Zero(t, store.held(), "an agent nobody watches left state behind")
	watchers, err := svc.Watchers(ctx, watched, watched)
	require.NoError(t, err)
	assert.Zero(t, watchers, "an agent nobody watches counted a watcher")

	first, err := svc.Watch(ctx, watched)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, watched, watched, event(t, "turn.output", "hello"), &watch.Fence{Agent: watched}))
	assert.Equal(t, "turn.output", awaitEvent(t, first).Kind())
	assertQuiet(t, first, "an event announced before the watch opened")

	// A second watcher on the same agent receives what the first does,
	// and receives nothing that was announced before it opened.
	second, err := svc.Watch(ctx, watched)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, watched, watched, event(t, "turn.output", "both"), &watch.Fence{Agent: watched}))
	assert.Equal(t, "both", awaitEvent(t, first).Body())
	assert.Equal(t, "both", awaitEvent(t, second).Body())
	watchers, err = svc.Watchers(ctx, watched, watched)
	require.NoError(t, err)
	assert.Equal(t, 2, watchers)

	// A watcher on another agent is not a watcher of this one.
	elsewhere, err := svc.Watch(ctx, other)
	require.NoError(t, err)
	require.NoError(t, svc.Publish(ctx, watched, watched, event(t, "turn.output", "mine only"), &watch.Fence{Agent: watched}))
	assert.Equal(t, "mine only", awaitEvent(t, first).Body())
	assert.Equal(t, "mine only", awaitEvent(t, second).Body())
	assertQuiet(t, elsewhere, "an event about another agent")

	// A closed stream stops being forwarded to, and the one beside it is
	// undisturbed.
	require.NoError(t, first.Close(ctx))
	require.NoError(t, svc.Publish(ctx, watched, watched, event(t, "turn.end", "after the first left"), &watch.Fence{Agent: watched}))
	assert.Equal(t, "turn.end", awaitEvent(t, second).Kind())
	_, open := <-first.Events()
	assert.False(t, open, "a closed subscription still carries a stream")
	watchers, err = svc.Watchers(ctx, watched, watched)
	require.NoError(t, err)
	assert.Equal(t, 1, watchers)

	// The last watcher leaving takes the agent's last trace with it.
	require.NoError(t, second.Close(ctx))
	require.NoError(t, elsewhere.Close(ctx))
	watchers, err = svc.Watchers(ctx, watched, watched)
	require.NoError(t, err)
	assert.Zero(t, watchers)
	assert.Zero(t, store.held(), "a watch outlived the stream it belonged to")
	require.NoError(t, svc.Publish(ctx, watched, watched, event(t, "turn.output", "nobody left"), &watch.Fence{Agent: watched}))
	assert.Equal(t, 6, relay.count(), "an event was not announced")
}

// TestService_ActsOnTheEventsOfTheCallingAgentOnly pins the one
// comparison that decides both machine-side calls. The accepted case is
// the control: it is what stops being answered if something other than
// that comparison is doing the refusing.
func TestService_ActsOnTheEventsOfTheCallingAgentOnly(t *testing.T) {
	ctx := context.Background()
	store := newLeases()
	relay := newAnnouncements()
	svc := receiving(t, store, relay)
	watched := agentGRN("0a1b2c3d4e5f6071")
	stranger := agentGRN("9f8e7d6c5b4a3021")

	err := svc.Publish(ctx, stranger, watched, event(t, "turn.output", "not mine to say"), &watch.Fence{Agent: watched})
	require.ErrorIs(t, err, watch.ErrNotAgent)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
	assert.Zero(t, relay.count(), "an event nobody was allowed to forward was announced")

	_, err = svc.Watchers(ctx, stranger, watched)
	require.ErrorIs(t, err, watch.ErrNotAgent)

	require.NoError(t, svc.Publish(ctx, watched, watched, event(t, "turn.output", "mine"), &watch.Fence{Agent: watched}))
	assert.Equal(t, 1, relay.count())
	watchers, err := svc.Watchers(ctx, watched, watched)
	require.NoError(t, err)
	assert.Zero(t, watchers)
}

// TestService_WatchReportsWhatTheStoreRefused keeps a watch that was
// never recorded from being served: the count another process reads
// comes from the store, so a stream opened without one would be
// forwarded to by nobody and look like a quiet agent.
func TestService_WatchReportsWhatTheStoreRefused(t *testing.T) {
	store := newLeases()
	store.err = errorx.New(errorx.KindUnavailable, "no database")
	svc := receiving(t, store, newAnnouncements())

	_, err := svc.Watch(context.Background(), agentGRN("0a1b2c3d4e5f6071"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
}

// TestSubscription_RefreshFailsOnceTheWatchIsGone covers what the holder
// of a stream is told when its lease is no longer there, which is the
// signal to end the stream rather than serve silence.
func TestSubscription_RefreshFailsOnceTheWatchIsGone(t *testing.T) {
	ctx := context.Background()
	store := newLeases()
	svc := receiving(t, store, newAnnouncements())

	sub, err := svc.Watch(ctx, agentGRN("0a1b2c3d4e5f6071"))
	require.NoError(t, err)
	require.NoError(t, sub.Refresh(ctx), "a live watch could not be carried forward")

	require.NoError(t, sub.Close(ctx))
	require.ErrorIs(t, sub.Refresh(ctx), watch.ErrWatchLapsed)
}

// TestSubscription_KeepAliveLeavesRoomForALostRefresh pins that a watch
// is refreshed more than once inside its lease. One write that does not
// land must not lapse a watch whose stream is still open.
func TestSubscription_KeepAliveLeavesRoomForALostRefresh(t *testing.T) {
	svc := receiving(t, newLeases(), newAnnouncements())

	sub, err := svc.Watch(context.Background(), agentGRN("0a1b2c3d4e5f6071"))
	require.NoError(t, err)
	assert.LessOrEqual(t, 2*sub.KeepAlive(), lease)
	assert.Positive(t, sub.KeepAlive())
}

// TestSubscription_CloseEndsTheWatchOnce covers a holder that closes
// twice, which is what a handler returning through more than one path
// does.
func TestSubscription_CloseEndsTheWatchOnce(t *testing.T) {
	ctx := context.Background()
	store := newLeases()
	svc := receiving(t, store, newAnnouncements())

	sub, err := svc.Watch(ctx, agentGRN("0a1b2c3d4e5f6071"))
	require.NoError(t, err)
	require.NoError(t, sub.Close(ctx))
	require.NoError(t, sub.Close(ctx))
	assert.Zero(t, store.held())
}

func TestNewService_RefusesALeaseNoWatchCouldStayLiveUnder(t *testing.T) {
	_, err := watch.NewService(newLeases(), newAnnouncements(), watch.Config{Lease: -time.Second})
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// TestNewEvent_RefusesWhatCannotBeFramed covers the boundary the event
// is parsed at. The accepted case carries a body spanning lines, because
// that is what the framing has to survive; the refusals are the kinds
// that would arrive as a different event or as none.
func TestNewEvent_RefusesWhatCannotBeFramed(t *testing.T) {
	accepted, err := watch.NewEvent("turn.output", "first line\nsecond line")
	require.NoError(t, err)
	assert.Equal(t, "turn.output", accepted.Kind())
	assert.Equal(t, "first line\nsecond line", accepted.Body())

	refused := map[string]struct{ kind, body string }{
		"no kind":        {"", "hello"},
		"kind of lines":  {"turn\noutput", "hello"},
		"kind too long":  {strings.Repeat("k", watch.MaxKindLength+1), "hello"},
		"body too long":  {"turn.output", strings.Repeat("b", watch.MaxBodyLength+1)},
		"kind of a line": {"turn.output\r", "hello"},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			_, err := watch.NewEvent(tc.kind, tc.body)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}
