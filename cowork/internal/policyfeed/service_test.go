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

	"github.com/garamsh/garam/cowork/internal/policyfeed"
)

// scripted is a Store that answers each pass with the next Result and
// cancels the loop once it has none left.
type scripted struct {
	mu      sync.Mutex
	results []policyfeed.Result
	cancel  context.CancelFunc
}

func (s *scripted) Deliver(context.Context, int, policyfeed.Sender) (policyfeed.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.results) == 0 {
		s.cancel()
		return policyfeed.Result{}, nil
	}
	r := s.results[0]
	s.results = s.results[1:]
	return r, nil
}

// passes signals each pass it runs, and gives up with the loop.
type passes struct{ ran chan struct{} }

func (p passes) Deliver(ctx context.Context, _ int, _ policyfeed.Sender) (policyfeed.Result, error) {
	select {
	case p.ran <- struct{}{}:
	case <-ctx.Done():
	}
	return policyfeed.Result{}, nil
}

func TestWake_StartsAPassWithoutWaitingOutThePoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := passes{ran: make(chan struct{})}
	svc := policyfeed.NewService(store, nil, slog.Default(), policyfeed.Config{Poll: time.Hour, MaxBackoff: time.Hour})
	done := make(chan error, 1)
	go func() { done <- svc.Run(ctx) }()

	// A pass that never comes is the failure, so the wait ends rather
	// than holds the run.
	pass := func(what string) {
		select {
		case <-store.ran:
		case <-time.After(10 * time.Second):
			require.FailNow(t, "no pass ran", what)
		}
	}
	pass("the first, after which the loop sleeps an hour")
	svc.Wake()
	pass("the second, which only a wake starts")

	cancel()
	assert.NoError(t, <-done)
}

func TestRun_LogsAFailingOrganizationOnceGoingInAndOnceComingOut(t *testing.T) {
	zetaFails := policyfeed.Result{
		Marks:     2,
		Delivered: []string{"acme"},
		Failed:    []policyfeed.OrgFailure{{Org: "zeta", Err: errors.New("token refused")}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &scripted{cancel: cancel, results: []policyfeed.Result{
		zetaFails, zetaFails, zetaFails,
		{Marks: 1, Delivered: []string{"zeta"}},
	}}
	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&out, nil))
	svc := policyfeed.NewService(store, nil, logger, policyfeed.Config{Poll: time.Millisecond, MaxBackoff: time.Millisecond})

	require.NoError(t, svc.Run(ctx))

	log := out.String()
	assert.Equal(t, 1, strings.Count(log, "failing for an organization"), log)
	assert.Equal(t, 1, strings.Count(log, "recovered for an organization"), log)
	assert.Contains(t, log, "org=zeta")
	assert.NotContains(t, log, "org=acme", "an organization that never failed is not logged")
}
