package integration_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/cli"
)

// idleParam is the setting cli.PoolConfig sets on every connection.
const idleParam = "idle_in_transaction_session_timeout"

// Postgres' SQLSTATE codes for the two refusals below: the session the
// timeout ended, and a row lock NOWAIT could not take.
const (
	sqlstateIdleInTransactionTimeout = "25P03"
	sqlstateLockNotAvailable         = "55P03"
)

// shortIdle is the timeout the scenario runs under, so the test does not
// wait the 60 s the pool sets. The 60 s itself is asserted separately,
// on a connection the pool config opens unchanged.
const shortIdle = "1000"

// poolWith opens a pool from cli.PoolConfig on the test database, after
// tune has changed the configuration.
func poolWith(t *testing.T, tune func(*pgxpool.Config)) *pgxpool.Pool {
	t.Helper()
	cfg, err := cli.PoolConfig(os.Getenv("GARAM_TEST_DATABASE_URL"))
	require.NoError(t, err)
	cfg.MaxConns = 4
	if tune != nil {
		tune(cfg)
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return p
}

func TestPoolConfig_EveryConnectionRunsUnderTheSixtySecondIdleTimeout(t *testing.T) {
	pool(t) // migrates and resets the database first
	p := poolWith(t, nil)

	var got string
	require.NoError(t, p.QueryRow(context.Background(), `SHOW `+idleParam).Scan(&got))
	assert.Equal(t, "1min", got)

	// A second connection of the pool carries it too.
	first, err := p.Acquire(context.Background())
	require.NoError(t, err)
	defer first.Release()
	second, err := p.Acquire(context.Background())
	require.NoError(t, err)
	defer second.Release()
	require.NotSame(t, first.Conn(), second.Conn())
	require.NoError(t, second.QueryRow(context.Background(), `SHOW `+idleParam).Scan(&got))
	assert.Equal(t, "1min", got)
}

// holdFeedRow opens a transaction on a connection of p that takes the
// row lock the feed pass holds across its calls, and leaves it idle.
func holdFeedRow(t *testing.T, p *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := p.Begin(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	var enabled bool
	require.NoError(t, tx.QueryRow(context.Background(), `SELECT enabled FROM policy_feed FOR UPDATE`).Scan(&enabled))
	return tx
}

// takeFeedRow tries to take the lock holdFeedRow took, from another
// session, and says whether it could without waiting.
func takeFeedRow(t *testing.T, p *pgxpool.Pool) bool {
	t.Helper()
	tx, err := p.Begin(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	var enabled bool
	err = tx.QueryRow(context.Background(), `SELECT enabled FROM policy_feed FOR UPDATE NOWAIT`).Scan(&enabled)
	if err == nil {
		return true
	}
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "the refusal is not Postgres's: %v", err)
	require.Equal(t, sqlstateLockNotAvailable, pgErr.Code, "refused for a reason other than the lock: %v", err)
	return false
}

func TestIdleInTransaction_ALostClientsLockIsReleasedByTheTimeout(t *testing.T) {
	pool(t)
	holder := poolWith(t, func(cfg *pgxpool.Config) { cfg.ConnConfig.RuntimeParams[idleParam] = shortIdle })
	other := poolWith(t, nil)

	tx := holdFeedRow(t, holder)
	require.False(t, takeFeedRow(t, other), "the lock was not held before the timeout, so nothing is measured")

	// The holder goes silent, as a client on a lost Node does. Once the
	// timeout has passed the second session takes the lock.
	require.Eventually(t, func() bool { return takeFeedRow(t, other) }, 10*time.Second, 200*time.Millisecond,
		"the lock a silent client held was not released")

	// The held transaction's next statement ends with Postgres's
	// idle-in-transaction error.
	var enabled bool
	err := tx.QueryRow(context.Background(), `SELECT enabled FROM policy_feed`).Scan(&enabled)
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "the held transaction ended with %v, not a Postgres error", err)
	assert.Equal(t, sqlstateIdleInTransactionTimeout, pgErr.Code, "%v", pgErr)
}

func TestIdleInTransaction_WithoutTheTimeoutTheLockStaysHeld(t *testing.T) {
	// The control: the same sequence with the setting off, as before the
	// change. The second session cannot take the lock for several times
	// what the timeout is above, and the transaction is still usable.
	pool(t)
	holder := poolWith(t, func(cfg *pgxpool.Config) { cfg.ConnConfig.RuntimeParams[idleParam] = "0" })
	other := poolWith(t, nil)

	tx := holdFeedRow(t, holder)
	require.Never(t, func() bool { return takeFeedRow(t, other) }, 4*time.Second, 200*time.Millisecond,
		"the lock was released with no timeout set")

	var enabled bool
	require.NoError(t, tx.QueryRow(context.Background(), `SELECT enabled FROM policy_feed`).Scan(&enabled))
}
