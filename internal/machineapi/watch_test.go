package machineapi_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/watch"
)

// watchLeases is a watch.Store in memory: the watches open, which is
// what a poll's watcher count is read from.
type watchLeases struct {
	mu   sync.Mutex
	byID map[watch.WatchID]grn.GRN
}

func newWatchLeases() *watchLeases {
	return &watchLeases{byID: map[watch.WatchID]grn.GRN{}}
}

func (l *watchLeases) Open(_ context.Context, id watch.WatchID, agent grn.GRN, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.byID[id] = agent
	return nil
}

func (l *watchLeases) Refresh(_ context.Context, id watch.WatchID, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, live := l.byID[id]; !live {
		return watch.ErrWatchLapsed
	}
	return nil
}

func (l *watchLeases) Close(_ context.Context, id watch.WatchID) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byID, id)
	return nil
}

func (l *watchLeases) Count(_ context.Context, agent grn.GRN) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	count := 0
	for _, watched := range l.byID {
		if watched == agent {
			count++
		}
	}
	return count, nil
}

// announced is a watch.Relay recording what this surface forwarded.
// Nothing receives here: the process holding a stream is another one
// (ADR-0017), so what a test on this side can assert is what left it.
type announced struct {
	mu     sync.Mutex
	events []watch.Event
}

func (a *announced) Announce(_ context.Context, _ grn.GRN, e watch.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, e)
	return nil
}

func (a *announced) Receive(ctx context.Context, _ func(grn.GRN, watch.Event)) error {
	<-ctx.Done()
	return nil
}

// made returns what was announced.
func (a *announced) made() []watch.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]watch.Event(nil), a.events...)
}

// watching returns the watch service the surface answers from, over a
// relay the test reads what was forwarded from.
func watching(t *testing.T, relay watch.Relay) watch.Service {
	t.Helper()
	svc, err := watch.NewService(newWatchLeases(), relay, watch.Config{})
	require.NoError(t, err)
	return svc
}

// unwatched is the watch service of a surface nobody is watching
// through, for the tests that are about something else.
func unwatched(t *testing.T) watch.Service {
	t.Helper()
	return watching(t, &announced{})
}

// TestServer_ForwardsTheTurnEventsOfTheAgentItAuthenticatedAs covers
// what leaves this surface when an adapter forwards. The refusals sit
// beside the accepted forward, because a 403 holds for a surface that
// refuses everyone and a 400 for one that parses nothing.
func TestServer_ForwardsTheTurnEventsOfTheAgentItAuthenticatedAs(t *testing.T) {
	caller := agentGRN("acme", "0a1b2c3d4e5f6071")
	other := agentGRN("acme", "9f8e7d6c5b4a3021")
	relay := &announced{}
	url, client := listener(t, &agentService{}, &messageService{}, consenting(), watching(t, relay), &memberGroups{}, &engine{}, caller)

	status, body := send(t, client, http.MethodPost, url+"/agents/"+caller.String()+"/turn-events",
		`{"kind":"turn.output","body":"first line\nsecond line"}`)
	require.Equal(t, http.StatusAccepted, status, body)
	assert.Empty(t, body, "an answer that reports something about a stream nothing kept")
	forwarded := relay.made()
	require.Len(t, forwarded, 1)
	assert.Equal(t, "turn.output", forwarded[0].Kind())
	assert.Equal(t, "first line\nsecond line", forwarded[0].Body(),
		"a body spanning lines reached the relay changed")

	// The events of another agent are not this caller's to forward, and
	// nothing about them is announced.
	status, body = send(t, client, http.MethodPost, url+"/agents/"+other.String()+"/turn-events",
		`{"kind":"turn.output","body":"not mine to say"}`)
	assert.Equal(t, http.StatusForbidden, status, body)
	assert.JSONEq(t, `{"kind":"permission_denied","message":"`+watch.ErrNotAgent.Error()+`"}`, body)
	assert.Len(t, relay.made(), 1, "an event nobody was allowed to forward was announced")

	// An event that cannot be framed is refused where it arrives rather
	// than carried and dropped later.
	status, body = send(t, client, http.MethodPost, url+"/agents/"+caller.String()+"/turn-events",
		`{"kind":"","body":"hello"}`)
	assert.Equal(t, http.StatusBadRequest, status, body)
	assert.Contains(t, body, `"kind":"invalid_argument"`)

	status, body = send(t, client, http.MethodPost, url+"/agents/"+caller.String()+"/turn-events", `not json`)
	assert.Equal(t, http.StatusBadRequest, status, body)
	assert.Contains(t, body, `"kind":"invalid_argument"`)

	// A path segment that is no GRN names no agent.
	status, body = send(t, client, http.MethodPost, url+"/agents/bot-1/turn-events",
		`{"kind":"turn.output","body":"hello"}`)
	assert.Equal(t, http.StatusBadRequest, status, body)
	assert.Contains(t, body, `"kind":"invalid_argument"`)

	assert.Len(t, relay.made(), 1, "an event that was refused was announced anyway")
}
