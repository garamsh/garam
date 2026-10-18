package integration_test

import (
	"context"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/wire"
)

// policyListenerAppName is what the listener's connections call
// themselves, so pg_stat_activity tells them from every other
// connection this suite holds.
const policyListenerAppName = "garam-policy-listener-integration"

// pgProxy forwards TCP to the test database. What travels through it is
// unaltered Postgres traffic, so a connection made through it is a real
// one: cut ends it the way a failover ends one, and nothing connects
// again until mend.
type pgProxy struct {
	listener net.Listener
	mu       sync.Mutex
	open     []net.Conn
	cutOff   bool
	// refused is signalled when a connection is turned away while cut.
	refused chan struct{}
}

// newPGProxy starts a proxy in front of target and returns it cut when
// the test ends.
func newPGProxy(t *testing.T, target string) *pgProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &pgProxy{listener: ln, refused: make(chan struct{}, 1)}
	t.Cleanup(func() {
		_ = ln.Close()
		p.cut()
	})
	go p.forward(target)
	return p
}

// addr is where the proxy accepts.
func (p *pgProxy) addr() string {
	return p.listener.Addr().String()
}

// forward pairs each accepted connection with one to target until the
// proxy stops accepting.
func (p *pgProxy) forward(target string) {
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp", target)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.mu.Lock()
		if p.cutOff {
			p.mu.Unlock()
			_ = client.Close()
			_ = server.Close()
			select {
			case p.refused <- struct{}{}:
			default:
			}
			continue
		}
		p.open = append(p.open, client, server)
		p.mu.Unlock()
		go func() { _, _ = io.Copy(server, client); _ = server.Close() }()
		go func() { _, _ = io.Copy(client, server); _ = client.Close() }()
	}
}

// cut closes every connection through the proxy and refuses the ones
// that follow, until mend.
func (p *pgProxy) cut() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cutOff = true
	for _, c := range p.open {
		_ = c.Close()
	}
	p.open = nil
}

// mend lets connections through again.
func (p *pgProxy) mend() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cutOff = false
}

// requireRefused waits until the proxy has turned a connection away
// since it was cut: whoever holds a connection through it has seen it
// end and is trying again.
func (p *pgProxy) requireRefused(t *testing.T) {
	t.Helper()
	select {
	case <-p.refused:
	case <-time.After(10 * time.Second):
		t.Fatal("nothing tried to connect through the cut proxy")
	}
}

// databaseAddr is the host and port GARAM_TEST_DATABASE_URL names.
func databaseAddr(t *testing.T) string {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv("GARAM_TEST_DATABASE_URL"))
	require.NoError(t, err)
	return net.JoinHostPort(cfg.ConnConfig.Host, strconv.Itoa(int(cfg.ConnConfig.Port)))
}

// listenerPool reaches the test database through proxy, under the
// application name appName, so pg_stat_activity tells its connections
// from every other one this suite holds.
func listenerPool(t *testing.T, proxy *pgProxy, appName string) *pgxpool.Pool {
	t.Helper()
	host, port, err := net.SplitHostPort(proxy.addr())
	require.NoError(t, err)
	number, err := strconv.ParseUint(port, 10, 16)
	require.NoError(t, err)
	return connect(t, os.Getenv("GARAM_TEST_DATABASE_URL"), func(cfg *pgxpool.Config) {
		cfg.ConnConfig.Host = host
		cfg.ConnConfig.Port = uint16(number)
		// A fallback address would reach the database around the proxy,
		// and a connection the test cannot cut is no drop at all.
		cfg.ConnConfig.Fallbacks = nil
		cfg.ConnConfig.RuntimeParams["application_name"] = appName
	})
}

// requireListenerSessions waits until Postgres reports want sessions
// named appName registered on channel, which is the connection's state
// as the server holds it rather than as the test assumes it.
func requireListenerSessions(t *testing.T, p *pgxpool.Pool, appName, channel string, want int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var sessions int
		err := p.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE application_name = $1 AND query = $2`,
			appName, "LISTEN "+channel).Scan(&sessions)
		return err == nil && sessions == want
	}, 10*time.Second, 20*time.Millisecond, "postgres should report %d session(s) listening on %s", want, channel)
}

// TestListenPolicyChanges_ResynchronisesAfterADroppedConnection is the
// failover case: the connection the listener holds is dropped, the
// org's policies change while it is down, and the reconnected listener
// has to answer with the changed set.
//
// The notification those changes raise is delivered to whoever is
// listening at the time and is gone afterwards, so a listener that only
// reattached would answer from a cache that missed it. That is why the
// policy the cache holds is replaced rather than added to: a stale
// cache answers "before", a cache that failed to reload answers with an
// error or nothing, and only a resynchronised one answers "after".
//
// Nothing here is stood in for. The connection dropped is a real
// Postgres connection, cut at the socket the way a failover cuts one,
// and the reconnections that follow fail for as long as nothing
// accepts. Only the listener's connection runs through the proxy: the
// writes below are the other process this listener exists to hear
// about.
func TestListenPolicyChanges_ResynchronisesAfterADroppedConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)
	author := grnOf("user", "ada")

	proxy := newPGProxy(t, databaseAddr(t))
	listening := listenerPool(t, proxy, policyListenerAppName)
	cache := authz.NewPolicyCache(s)
	go func() { _ = authz.ListenPolicyChanges(ctx, listening, cache) }()
	requireListenerSessions(t, p, policyListenerAppName, authz.PolicyChannel, 1)

	// An entry to invalidate, and then a write to invalidate it with:
	// the cache answering "before" is the listener delivering, since
	// nothing else drops what the empty read cached.
	empty, err := cache.Statements(ctx, orgGRN, nil)
	require.NoError(t, err)
	require.Empty(t, empty)

	before, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(before, orgGRN, "before"), policyDoc("before", wire.EffectAllow), author)
	require.NoError(t, err)
	_, err = s.SetPolicyStatus(ctx, before, wire.PolicyStatusActive)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		got, err := cache.Statements(ctx, orgGRN, nil)
		return err == nil && len(got) == 1 && got[0].Sid == "before"
	}, 10*time.Second, 20*time.Millisecond, "the listener should invalidate the org it is sent")

	proxy.cut()
	// Postgres drops the session with the socket, so the writes below
	// are made with nothing listening on the channel at all.
	requireListenerSessions(t, p, policyListenerAppName, authz.PolicyChannel, 0)
	// A refused retry puts the listener past queued setup notifications.
	// Warm only then, so no old invalidation can drop the control's entry.
	proxy.requireRefused(t)

	// What the cache holds while the listener is away, which only a
	// reload can replace.
	warm, err := cache.Statements(ctx, orgGRN, nil)
	require.NoError(t, err)
	require.Len(t, warm, 1)
	require.Equal(t, "before", warm[0].Sid)

	_, err = s.SetPolicyStatus(ctx, before, wire.PolicyStatusArchived)
	require.NoError(t, err)
	after, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(after, orgGRN, "after"), policyDoc("after", wire.EffectAllow), author)
	require.NoError(t, err)
	_, err = s.SetPolicyStatus(ctx, after, wire.PolicyStatusActive)
	require.NoError(t, err)

	// The control: with the connection down the cache still answers
	// what it read above, which is what makes the assertion below a
	// resynchronisation rather than a late notification.
	stale, err := cache.Statements(ctx, orgGRN, nil)
	require.NoError(t, err)
	require.Equal(t, warm, stale)

	proxy.mend()

	assert.Eventually(t, func() bool {
		got, err := cache.Statements(ctx, orgGRN, nil)
		return err == nil && len(got) == 1 && got[0].Sid == "after"
	}, 20*time.Second, 50*time.Millisecond,
		"the reconnected listener should drop what the cache read while it was away, so what it missed is read again")
}
