package cli

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/errorx"
)

// IdleInTransactionTimeout ends a session that waits this long between
// two statements of a transaction. A client Node lost mid-transaction
// leaves its backend idle in the transaction, holding its row locks
// until Postgres notices the peer is gone, and the source sets no bound
// on that. 60 s is twice the policy client's 30 s call deadline, so one
// gap between statements covers a call made inside a transaction (the
// feed pass holds `policy_feed` across such calls) plus slack
// (ADR-0178 §The lock a lost Node leaves behind).
const IdleInTransactionTimeout = 60 * time.Second

// idleInTransactionParam is the server setting IdleInTransactionTimeout
// sets, which Postgres reads in milliseconds.
const idleInTransactionParam = "idle_in_transaction_session_timeout"

// PoolConfig is the configuration of the pool databaseURL names, with
// IdleInTransactionTimeout set as a runtime parameter, which pgx sends
// in the startup message of every connection the pool opens. A value
// the URL itself names for the setting is the deployment's and is kept.
func PoolConfig(databaseURL string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	if _, set := cfg.ConnConfig.RuntimeParams[idleInTransactionParam]; !set {
		cfg.ConnConfig.RuntimeParams[idleInTransactionParam] =
			strconv.FormatInt(IdleInTransactionTimeout.Milliseconds(), 10)
	}
	return cfg, nil
}

// openPool opens the database GARAM_DATABASE_URL names, refusing a
// command that sets none. A pool that cannot be built is unavailable,
// not the caller's mistake. The caller closes it.
func openPool(ctx context.Context) (*pgxpool.Pool, error) {
	databaseURL := envOr(envDatabaseURL, "")
	if databaseURL == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
	}
	cfg, err := PoolConfig(databaseURL)
	if err != nil {
		return nil, errorx.Wrap(err, "postgres", errorx.KindUnavailable)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errorx.Wrap(err, "postgres", errorx.KindUnavailable)
	}
	return pool, nil
}
