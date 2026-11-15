package policyfeed_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/policyfeed"
	"github.com/garamsh/garam/lib/errorx"
)

// trail is what a gate did, in the order it did it.
type trail struct {
	mu     sync.Mutex
	events []string
}

func (t *trail) add(e string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, e)
}

func (t *trail) all() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.events...)
}

// marks holds a mark of the entity asked about until it has been read
// releaseAfter times, or for good where releaseAfter is zero.
type marks struct {
	trail        *trail
	releaseAfter int
	err          error
	reads        int
}

func (m *marks) Pending(_ context.Context, e policyfeed.Entity) (bool, error) {
	m.reads++
	m.trail.add("read " + e.Type + " " + e.Org + " " + e.ID)
	if m.err != nil {
		return false, m.err
	}
	return m.releaseAfter == 0 || m.reads < m.releaseAfter, nil
}

// waking is the loop the gate wakes.
type waking struct{ trail *trail }

func (w waking) Run(context.Context) error { return nil }
func (w waking) Wake()                     { w.trail.add("wake") }

var home = policyfeed.Entity{Type: "page", Org: "acme", ID: "abcdefghijkl"}

func TestAwait_WakesTheLoopThenWaitsOnTheMarkOfTheEntityItNames(t *testing.T) {
	tr := &trail{}
	gate := policyfeed.NewGate(&marks{trail: tr, releaseAfter: 3}, waking{tr}, time.Minute)

	require.NoError(t, gate.Await(context.Background(), home))

	read := "read page acme abcdefghijkl"
	assert.Equal(t, []string{"wake", read, read, read}, tr.all())
}

func TestAwait_AnAlreadyDeliveredEntityIsOneReadAndNoWait(t *testing.T) {
	tr := &trail{}
	gate := policyfeed.NewGate(&marks{trail: tr, releaseAfter: 1}, waking{tr}, time.Minute)

	require.NoError(t, gate.Await(context.Background(), home))

	assert.Equal(t, []string{"wake", "read page acme abcdefghijkl"}, tr.all())
}

func TestAwait_AnswersAtTheBoundNamingTheEntityItStored(t *testing.T) {
	tr := &trail{}
	gate := policyfeed.NewGate(&marks{trail: tr}, waking{tr}, 20*time.Millisecond)

	err := gate.Await(context.Background(), policyfeed.Entity{Type: "task", Org: "acme", ID: "k3v7q2m9x4ab"})

	require.Error(t, err)
	assert.Equal(t, errorx.KindUnavailable, errorx.KindOf(err))
	reason, ok := errorx.ReasonOf(err)
	assert.True(t, ok)
	assert.Equal(t, errorx.Reason{Code: "change_not_applied"}, reason)
	var stored interface{ Resource() string }
	require.True(t, errors.As(err, &stored))
	assert.Equal(t, "grn:acme/cowork/task/k3v7q2m9x4ab", stored.Resource())
	assert.Equal(t, "the change is stored and the policy service has not yet applied it", err.Error())
}

func TestAwait_AFailedReadIsTheAnswerAndIsNotRetried(t *testing.T) {
	tr := &trail{}
	failed := errorx.New(errorx.KindUnavailable, "database down")
	m := &marks{trail: tr, err: failed}
	gate := policyfeed.NewGate(m, waking{tr}, time.Minute)

	err := gate.Await(context.Background(), home)

	assert.Same(t, failed, err)
	assert.Equal(t, 1, m.reads)
}

func TestAwait_ACanceledRequestStopsWaiting(t *testing.T) {
	tr := &trail{}
	gate := policyfeed.NewGate(&marks{trail: tr}, waking{tr}, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := gate.Await(ctx, home)

	assert.True(t, errorx.IsKind(err, errorx.KindCanceled), "%v", err)
}
