package policyfeed_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// scriptedStore answers each pass from a script and cancels the loop
// when the script runs out, so Run returns.
type scriptedStore struct {
	mu     sync.Mutex
	script []step
	calls  int
	cancel context.CancelFunc
}

type step struct {
	result policyfeed.Result
	err    error
}

func (s *scriptedStore) Deliver(
	_ context.Context, limit int, _ policyfeed.Sender,
) (policyfeed.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit != policyfeed.BatchLimit {
		panic("pass limit is not the batch limit")
	}
	st := s.script[s.calls]
	s.calls++
	if s.calls == len(s.script) {
		s.cancel()
	}
	return st.result, st.err
}

func (s *scriptedStore) LinkPending(context.Context, string, string) (bool, error) {
	return false, nil
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func run(t *testing.T, script []step, cfg policyfeed.Config) (*scriptedStore, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &scriptedStore{script: script, cancel: cancel}
	var logs syncBuffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	svc := policyfeed.NewService(store, nil, logger, cfg)
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context ended")
	}
	return store, logs.String()
}

func TestRun_LogsFailureOnceAndRecovery(t *testing.T) {
	boom := errors.New("policy service unreachable")
	_, logs := run(t, []step{
		{err: boom}, {err: boom}, {err: boom}, {},
	}, policyfeed.Config{Poll: time.Millisecond, MaxBackoff: 2 * time.Millisecond})

	assert.Equal(t, 1, strings.Count(logs, "policy feed delivery failing"),
		"an outage logs on the transition, not on every retry")
	assert.Equal(t, 1, strings.Count(logs, "policy feed delivery recovered"))
	assert.Contains(t, logs, "failed_passes=3")
}

func TestRun_LogsEachRefusal(t *testing.T) {
	_, logs := run(t, []step{{result: policyfeed.Result{
		Marks: 2,
		Refused: []policyfeed.Refusal{
			{Entity: policyfeed.Entity{Type: "Iam::Team", ID: "grn:acme/iam/group/t1"}, Reason: "too many parents"},
		},
	}}}, policyfeed.Config{Poll: time.Millisecond, MaxBackoff: time.Millisecond})

	assert.Contains(t, logs, "level=ERROR")
	assert.Contains(t, logs, "grn:acme/iam/group/t1")
	assert.Contains(t, logs, "too many parents")
}

func TestRun_FullBatchDoesNotWait(t *testing.T) {
	start := time.Now()
	store, _ := run(t, []step{
		{result: policyfeed.Result{Marks: policyfeed.BatchLimit}},
		{result: policyfeed.Result{Marks: policyfeed.BatchLimit}},
		{result: policyfeed.Result{Marks: 1}},
	}, policyfeed.Config{Poll: time.Hour, MaxBackoff: time.Hour})

	assert.Equal(t, 3, store.calls)
	assert.Less(t, time.Since(start), 5*time.Second,
		"a full batch is followed at once by the next pass")
}

func TestRun_ReturnsNilWhenContextEndsWhileWaiting(t *testing.T) {
	// The control of the full-batch test: a short batch does wait, and
	// the wait ends with the context.
	ctx, cancel := context.WithCancel(context.Background())
	store := &scriptedStore{script: []step{{}}, cancel: cancel}
	svc := policyfeed.NewService(store, nil, slog.New(slog.DiscardHandler),
		policyfeed.Config{Poll: time.Hour, MaxBackoff: time.Hour})

	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept waiting out an hour after its context ended")
	}
}

func TestRun_WakeEndsTheWaitBetweenPasses(t *testing.T) {
	// Poll is an hour, so the second pass runs only if Wake cut the wait.
	// The control: the same script without a wake stays in the wait until
	// the test gives up (TestRun_ReturnsNilWhenContextEndsWhileWaiting
	// holds the wait itself).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &scriptedStore{script: []step{{}, {}}, cancel: cancel}
	svc := policyfeed.NewService(store, nil, slog.New(slog.DiscardHandler),
		policyfeed.Config{Poll: time.Hour, MaxBackoff: time.Hour})
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	require.Eventually(t, func() bool {
		store.mu.Lock()
		defer store.mu.Unlock()
		return store.calls == 1
	}, 5*time.Second, time.Millisecond, "the first pass runs at once")
	svc.Wake()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("a wake did not end the wait between passes")
	}
	assert.Equal(t, 2, store.calls)
}

func TestRun_BackoffStopsAtTheCap(t *testing.T) {
	boom := errors.New("policy service unreachable")
	start := time.Now()
	// Four waits: 50, 100, 120, 120ms at the cap, and 50, 100, 200, 200
	// without.
	run(t, []step{{err: boom}, {err: boom}, {err: boom}, {err: boom}, {err: boom}},
		policyfeed.Config{Poll: 50 * time.Millisecond, MaxBackoff: 120 * time.Millisecond})

	assert.Less(t, time.Since(start), 480*time.Millisecond)
}
