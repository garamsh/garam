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
	"github.com/stretchr/testify/require"
)

// pgProxy forwards TCP to the test database. What travels through it is
// unaltered Postgres traffic, so a connection made through it is a real
// one: cut ends it the way a failover ends one, and nothing connects
// again until mend.
type pgProxy struct {
	listener net.Listener
	mu       sync.Mutex
	open     []net.Conn
	cutOff   bool
}

// newPGProxy starts a proxy in front of target and returns it cut when
// the test ends.
func newPGProxy(t *testing.T, target string) *pgProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &pgProxy{listener: ln}
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
