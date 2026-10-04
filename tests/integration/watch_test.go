package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/watch"
)

// watchRows is how many rows the watch table holds, lapsed ones
// included. It is what says whether anything was left behind for an
// agent nobody is watching.
func watchRows(t *testing.T, p *pgxpool.Pool) int {
	t.Helper()
	var rows int
	require.NoError(t, p.QueryRow(context.Background(), "SELECT count(*) FROM agent_watches").Scan(&rows))
	return rows
}

// TestWatchSurface_AWatchInOneProcessIsCountedAndFedByAnother is the
// whole of ADR-0022's shape over one database: a browser opens a stream
// on the process serving browsers, the poll the adapter holds on the
// machine listener says so, and what the adapter forwards there arrives
// on that stream. The two services are the two processes garam runs
// (ADR-0017) and share nothing but Postgres.
//
// Every count sits beside the same call made with nobody watching. That
// control is what the design rests on: an agent nobody looks at costs
// nothing and is answered in the same shape as one somebody does.
func TestWatchSurface_AWatchInOneProcessIsCountedAndFedByAnother(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	watched, adapter := provisioned(t, url, operator)

	// The process serving browsers: its own watch service over the same
	// database, delivering what any process announces to the streams it
	// holds. Nothing here shares memory with the listener above.
	browser, err := watch.NewService(watch.NewPostgresStore(p), watch.NewPostgresRelay(p), watch.Config{})
	require.NoError(t, err)
	receiving, stopReceiving := context.WithCancel(ctx)
	t.Cleanup(stopReceiving)
	go func() { _ = browser.Receive(receiving) }()

	events := url + "/agents/" + watched.String() + "/turn-events"

	// Nobody is watching. The poll says so, forwarding reaches no one, and
	// the database holds nothing about the agent.
	first := claimOne(t, adapter, url, watched, "key-1")
	assert.Zero(t, first.Watchers, "an agent nobody watches was answered a watcher")
	status, body := machineSend(t, adapter, http.MethodPost, events,
		`{"kind":"turn.start","body":"before anyone looked"}`)
	require.Equal(t, http.StatusAccepted, status, string(body))
	assert.Zero(t, watchRows(t, p), "an agent nobody watches left state behind")
	completeOne(t, adapter, url, watched, first)

	// A person opens the agent in the console.
	sub, err := browser.Watch(ctx, watched)
	require.NoError(t, err)

	second := claimOne(t, adapter, url, watched, "key-2")
	assert.Equal(t, 1, second.Watchers, "a watch opened in another process was not counted")
	renewed := renewOne(t, adapter, url, watched, second)
	assert.Equal(t, 1, renewed.Watchers, "the renewal a caller makes mid-turn carried no count")

	// What the agent says now reaches that stream, and what it said
	// before anyone looked does not: nothing was kept to replay.
	status, body = machineSend(t, adapter, http.MethodPost, events,
		`{"kind":"turn.output","body":"first line\nsecond line"}`)
	require.Equal(t, http.StatusAccepted, status, string(body))
	select {
	case e := <-sub.Events():
		assert.Equal(t, "turn.output", e.Kind(), "the stream carried an event announced before it opened")
		assert.Equal(t, "first line\nsecond line", e.Body())
	case <-time.After(10 * time.Second):
		t.Fatal("nothing reached the stream")
	}
	assert.Equal(t, 1, watchRows(t, p), "the open stream is one row, and a turn event would be another")
	completeOne(t, adapter, url, watched, second)

	// The person leaves. The count returns to zero and nothing is left
	// for the agent.
	require.NoError(t, sub.Close(ctx))
	assert.Zero(t, watchRows(t, p), "a watch outlived the stream it belonged to")
	third := claimOne(t, adapter, url, watched, "key-3")
	assert.Zero(t, third.Watchers, "a closed stream is still counted")
	completeOne(t, adapter, url, watched, third)
}

// TestWatchStore_ALapsedWatchIsNotCountedAndIsSweptOnTheNextOpen covers
// what a process that died holding a stream leaves behind. Postgres
// measures the lease, so a watch nobody closed stops being counted
// without any instance deciding when.
func TestWatchStore_ALapsedWatchIsNotCountedAndIsSweptOnTheNextOpen(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	store := watch.NewPostgresStore(p)
	watched := scopedGRN("acme", agent.ServiceType, "0a1b2c3d4e5f6071")
	other := scopedGRN("acme", agent.ServiceType, "9f8e7d6c5b4a3021")

	require.NoError(t, store.Open(ctx, "watch-1", watched, 200*time.Millisecond))
	count, err := store.Count(ctx, watched)
	require.NoError(t, err)
	require.Equal(t, 1, count, "a live watch was not counted")

	assert.Eventually(t, func() bool {
		count, err := store.Count(ctx, watched)
		require.NoError(t, err)
		return count == 0
	}, 5*time.Second, 20*time.Millisecond, "a watch nobody refreshed stayed counted")
	assert.Equal(t, 1, watchRows(t, p), "the lapsed row went before anything wrote")

	// Rows are written in one place, so that is where the lapsed ones go.
	require.NoError(t, store.Open(ctx, "watch-2", other, time.Minute))
	assert.Equal(t, 1, watchRows(t, p), "a lapsed watch survived the next open")

	require.ErrorIs(t, store.Refresh(ctx, "watch-1", time.Minute), watch.ErrWatchLapsed)
	require.NoError(t, store.Refresh(ctx, "watch-2", time.Minute), "a live watch could not be carried forward")

	require.NoError(t, store.Close(ctx, "watch-2"))
	require.NoError(t, store.Close(ctx, "watch-2"), "closing twice is not the same end")
	assert.Zero(t, watchRows(t, p))
}

// TestWatchStore_ClassifiesAWatchOnSomethingThatIsNotAnAgent covers the
// check 000011 carries: what a watch names is an agent, and no foreign
// key says so because a watch may never be what keeps an agent's row
// alive. The store answers the refusal as an invalid argument rather
// than as the driver error's default kind.
//
// The watch that opens is the control: the same call on an agent GRN
// lands, so the refusal is the service type in the GRN and not the
// call.
func TestWatchStore_ClassifiesAWatchOnSomethingThatIsNotAnAgent(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	store := watch.NewPostgresStore(p)

	require.NoError(t, store.Open(ctx, "watch-on-an-agent", scopedGRN("acme", agent.ServiceType, "0a1b2c3d4e5f6071"), time.Minute),
		"a watch on an agent opens")
	t.Cleanup(func() { require.NoError(t, store.Close(ctx, "watch-on-an-agent")) })

	// 23514 on agent_watches_agent_service_type.
	err := store.Open(ctx, "watch-on-a-user", scopedGRN("acme", "user", "ada"), time.Minute)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument),
		"a watch on something that is not an agent is the caller's mistake")
}

// TestWatchRelay_RefusesAnEventLargerThanOneAnnouncementCarries pins the
// limit the relay rests on. The event of the same length that is carried
// is the control: the refusal is the encoded size and not the length of
// the body.
func TestWatchRelay_RefusesAnEventLargerThanOneAnnouncementCarries(t *testing.T) {
	ctx := context.Background()
	relay := watch.NewPostgresRelay(pool(t))
	watched := scopedGRN("acme", agent.ServiceType, "0a1b2c3d4e5f6071")

	ordinary, err := watch.NewEvent("turn.output", strings.Repeat("a", watch.MaxBodyLength))
	require.NoError(t, err)
	require.NoError(t, relay.Announce(ctx, watched, ordinary), "an event of the bounded length was not carried")

	// Every character of this body takes two once encoded, so a body the
	// same length no longer fits.
	dense, err := watch.NewEvent("turn.output", strings.Repeat("\n", watch.MaxBodyLength))
	require.NoError(t, err)
	err = relay.Announce(ctx, watched, dense)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// claimOne posts a message to the agent and claims it, which is the turn
// an adapter forwards the events of.
func claimOne(t *testing.T, adapter *http.Client, url string, watched grn.GRN, key string) machinegen.Claim {
	t.Helper()
	status, body := machineSend(t, adapter, http.MethodPost, url+"/messages", submissionTo(watched, key))
	require.Equal(t, http.StatusCreated, status, string(body))

	status, body = machineSend(t, adapter, http.MethodPost, url+"/agents/"+watched.String()+"/claims", "")
	require.Equal(t, http.StatusOK, status, string(body))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &claimed))
	return claimed
}

// renewOne extends the hold, which is the call a caller makes while the
// turn runs.
func renewOne(t *testing.T, adapter *http.Client, url string, watched grn.GRN, claim machinegen.Claim) machinegen.Claim {
	t.Helper()
	status, body := machineSend(t, adapter, http.MethodPost,
		url+"/agents/"+watched.String()+"/claims/"+claim.Id+"/renewal", "")
	require.Equal(t, http.StatusOK, status, string(body))
	var renewed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &renewed))
	return renewed
}

// completeOne reports the claimed message delivered, so the next claim
// answers with the next message rather than this one again.
func completeOne(t *testing.T, adapter *http.Client, url string, watched grn.GRN, claim machinegen.Claim) {
	t.Helper()
	status, body := machineSend(t, adapter, http.MethodPost,
		url+"/agents/"+watched.String()+"/claims/"+claim.Id+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))
}
