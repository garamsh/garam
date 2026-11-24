// Package loopback parses a Postgres connection string for a test-only
// command and refuses it unless it reaches this machine alone: the
// guard testprincipal and the cluster-connection fixture share.
package loopback

import (
	"context"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/errorx"
)

// ErrNotLoopback refuses a connection that would leave this machine.
// It names the rule and never the connection string, which can carry
// a password.
var ErrNotLoopback = errorx.New(errorx.KindInvalidArgument,
	"the Postgres server must be reached over loopback or a unix socket")

// ParseConfig parses dsn with pgx's own parser and refuses it unless
// every host it names, fallbacks included, is on this machine. The
// returned config dials only loopback addresses whatever a name
// resolves to, so the check holds for the effective target and not
// only for the text.
func ParseConfig(dsn string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		// pgconn quotes the input in its message, password included.
		return nil, errorx.New(errorx.KindInvalidArgument, "the connection string does not parse")
	}
	conn := cfg.ConnConfig
	hosts := []string{conn.Host}
	for _, f := range conn.Fallbacks {
		hosts = append(hosts, f.Host)
	}
	for _, h := range hosts {
		if !localHost(h) {
			return nil, ErrNotLoopback
		}
	}
	dial := conn.DialFunc
	conn.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !localAddress(network, addr) {
			return nil, ErrNotLoopback
		}
		return dial(ctx, network, addr)
	}
	return cfg, nil
}

// localHost reports whether a host as written names this machine: a
// unix socket directory, a loopback address, or localhost, whose
// resolution localAddress checks at dial time.
func localHost(h string) bool {
	if strings.HasPrefix(h, "/") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return h == "localhost"
}

// localAddress reports whether a dial stays on this machine.
func localAddress(network, addr string) bool {
	if network == "unix" {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
