package main

import (
	"context"
	"net"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/platform/internal/errorx"
)

// errNotLoopback refuses a connection that would leave this machine.
// It names the rule and never the connection string, which can carry
// a password.
var errNotLoopback = errorx.New(errorx.KindInvalidArgument,
	"the Postgres server must be reached over loopback or a unix socket")

// parseLocal parses dsn with pgx's own parser and refuses it unless
// every host it names, fallbacks included, is on this machine. The
// returned config dials only loopback addresses whatever a name
// resolves to, so the check holds for the effective target and not
// only for the text.
func parseLocal(dsn string) (*pgxpool.Config, error) {
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
			return nil, errNotLoopback
		}
	}
	dial := conn.DialFunc
	conn.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if !localAddress(network, addr) {
			return nil, errNotLoopback
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

// withDatabase answers dsn naming database name instead, and refuses
// when the parser does not read name back — a service file or an
// override that still wins is a target this command did not create.
func withDatabase(dsn, name string) (string, *pgxpool.Config, error) {
	out := dsn + " dbname=" + name
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", nil, errorx.New(errorx.KindInvalidArgument, "the connection string does not parse")
		}
		u.Path = "/" + name
		q := u.Query()
		q.Del("dbname")
		u.RawQuery = q.Encode()
		out = u.String()
	}
	cfg, err := parseLocal(out)
	if err != nil {
		return "", nil, err
	}
	if cfg.ConnConfig.Database != name {
		return "", nil, errorx.New(errorx.KindInvalidArgument,
			"the connection string selects its database in a way this command cannot override; pass a plain URL or keyword form")
	}
	return out, cfg, nil
}

// passwordOf answers the password a dsn carries, for scrubbing from
// messages; empty when it carries none.
func passwordOf(dsn string) string {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return ""
	}
	return cfg.Password
}
