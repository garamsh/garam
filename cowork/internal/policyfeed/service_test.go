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
