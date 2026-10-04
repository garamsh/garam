package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/watch"
)

// watchRelayAppName is what the relay's connections call themselves, so
// pg_stat_activity tells them from every other connection this suite
// holds.
const watchRelayAppName = "garam-watch-relay-integration"

// delivered is one event the relay handed to deliver, with the agent it
// named.
type delivered struct {
	agent grn.GRN
	event watch.Event
}

// TestPostgresRelay_ReopensItsConnectionAfterADrop is the failover case
// for the relay: the connection it listens on is cut, and an event
// announced after the proxy mends arrives without the relay having been
// started again.
//
// The event announced during the gap never arriving is asserted rather
// than assumed: the first event delivered after the mend is the one
// announced then, so nothing was kept to replay. ADR-0022 accepts that
// loss — a watcher sees from when it opened — and it is what makes
// reconnecting here need no resynchronisation, where the policy
// listener's does.
//
// Nothing here is stood in for. The connection dropped is a real
// Postgres connection, cut at the socket the way a failover cuts one,
// and the reconnections that follow fail for as long as nothing
// accepts. Only the relay's connection runs through the proxy: the
// announcements below are the other process this relay exists to hear
// about.
func TestPostgresRelay_ReopensItsConnectionAfterADrop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := pool(t)
	watched := scopedGRN("acme", agent.ServiceType, "0a1b2c3d4e5f6071")

	proxy := newPGProxy(t, databaseAddr(t))
	relay := watch.NewPostgresRelay(listenerPool(t, proxy, watchRelayAppName))
	events := make(chan delivered, 8)
	go func() {
		_ = relay.Receive(ctx, func(agent grn.GRN, e watch.Event) {
			events <- delivered{agent: agent, event: e}
		})
	}()
	requireListenerSessions(t, p, watchRelayAppName, watch.TurnEventChannel, 1)

	// The announcer is the other process's relay, over a connection the
	// test does not cut.
	announcer := watch.NewPostgresRelay(p)
	announce := func(body string) {
		t.Helper()
		e, err := watch.NewEvent("turn.output", body)
		require.NoError(t, err)
		require.NoError(t, announcer.Announce(ctx, watched, e, nil))
	}

	announce("before the drop")
	select {
	case d := <-events:
		require.Equal(t, watched, d.agent)
		require.Equal(t, "before the drop", d.event.Body())
	case <-time.After(10 * time.Second):
		t.Fatal("nothing reached the relay before the drop")
	}

	proxy.cut()
	// Postgres drops the session with the socket, so the announcement
	// below is made with nothing listening on the channel at all.
	requireListenerSessions(t, p, watchRelayAppName, watch.TurnEventChannel, 0)

	announce("during the drop")
	proxy.mend()
	requireListenerSessions(t, p, watchRelayAppName, watch.TurnEventChannel, 1)

	announce("after the drop")
	select {
	case d := <-events:
		assert.Equal(t, watched, d.agent)
		assert.Equal(t, "after the drop", d.event.Body(),
			"the announcement made while nothing listened was replayed")
	case <-time.After(10 * time.Second):
		t.Fatal("the reconnected relay delivered nothing")
	}
}
