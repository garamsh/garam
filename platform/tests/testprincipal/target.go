package main

import (
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/tests/loopback"
)

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
	cfg, err := loopback.ParseConfig(out)
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
