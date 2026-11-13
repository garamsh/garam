package policyfeed_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// pendingStore reports a mark for the first `pending` reads, then none,
// or a set-aside one on each read where setAside is set.
type pendingStore struct {
	scriptedStore
	pending  atomic.Int64
	setAside bool
	reads    atomic.Int64
	err      error
	org      string
	user     string
}

func (p *pendingStore) LinkState(_ context.Context, org, user string) (policyfeed.LinkState, error) {
	p.reads.Add(1)
	p.org, p.user = org, user
	switch {
	case p.err != nil:
		return policyfeed.LinkApplied, p.err
	case p.setAside:
		return policyfeed.LinkSetAside, nil
	case p.pending.Add(-1) >= 0:
		return policyfeed.LinkPending, nil
	}
	return policyfeed.LinkApplied, nil
}

type wakeCounter struct{ wakes atomic.Int64 }

func (w *wakeCounter) Run(context.Context) error { return nil }
func (w *wakeCounter) Wake()                     { w.wakes.Add(1) }
func (w *wakeCounter) LastPass() time.Time       { return time.Time{} }

func TestGate_AwaitReturnsOnceTheMarkIsGone(t *testing.T) {
	store := &pendingStore{}
	store.pending.Store(2)
	svc := &wakeCounter{}

	err := policyfeed.NewGate(store, svc, time.Second).Await(context.Background(), "grn:o", "grn:u")

	require.NoError(t, err)
	assert.EqualValues(t, 3, store.reads.Load(), "two reads with the mark, one without")
	assert.EqualValues(t, 1, svc.wakes.Load(), "the loop is woken")
	assert.Equal(t, "grn:o", store.org)
	assert.Equal(t, "grn:u", store.user)
}

func TestGate_AwaitRefusesAtTheBoundWhileTheMarkStays(t *testing.T) {
	// The control is the test above: the same gate returns nil when the
	// mark goes. Here it never does.
	store := &pendingStore{}
	store.pending.Store(1 << 40)

	start := time.Now()
	err := policyfeed.NewGate(store, &wakeCounter{}, 120*time.Millisecond).
		Await(context.Background(), "grn:o", "")

	require.Error(t, err)
	assert.Equal(t, errorx.KindUnavailable, errorx.KindOf(err))
	reason, ok := errorx.ReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, policyfeed.ReasonChangeNotApplied, reason.Code)
	assert.GreaterOrEqual(t, time.Since(start), 120*time.Millisecond)
}

func TestGate_AwaitAnswersASetAsideLinkAtOnce(t *testing.T) {
	// The control is the bound test above: a mark that is only pending
	// holds the request to the bound, and a set-aside one, which no pass
	// will clear, does not.
	store := &pendingStore{setAside: true}

	start := time.Now()
	err := policyfeed.NewGate(store, &wakeCounter{}, time.Hour).
		Await(context.Background(), "grn:o", "grn:u")

	require.ErrorIs(t, err, policyfeed.ErrLinkSetAside)
	assert.NotErrorIs(t, err, policyfeed.ErrNotApplied,
		"it is not the answer that delivery goes on")
	assert.Equal(t, errorx.KindUnavailable, errorx.KindOf(err))
	reason, ok := errorx.ReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, policyfeed.ReasonChangeRefused, reason.Code)
	assert.Less(t, time.Since(start), time.Second, "no wait for the bound")
	assert.EqualValues(t, 1, store.reads.Load())
}

func TestGate_AwaitReturnsAStoreError(t *testing.T) {
	boom := errors.New("read failed")
	store := &pendingStore{err: boom}

	err := policyfeed.NewGate(store, &wakeCounter{}, time.Second).
		Await(context.Background(), "grn:o", "grn:u")

	require.ErrorIs(t, err, boom)
}

func TestGate_AwaitStopsWithItsContext(t *testing.T) {
	store := &pendingStore{}
	store.pending.Store(1 << 40)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := policyfeed.NewGate(store, &wakeCounter{}, time.Hour).Await(ctx, "grn:o", "")

	require.Error(t, err)
	assert.Equal(t, errorx.KindCanceled, errorx.KindOf(err))
}
