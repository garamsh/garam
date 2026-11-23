package cli_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/cli"
)

// TestOpenPool_RefusesAnUnsetURLAndAnswersAnUnbuildablePoolUnavailable
// holds the policy every command opening the database shares, through
// `serve api`: no URL is the caller's mistake, a URL no pool can be
// built from is unavailable, and a well-formed one opens a pool without
// dialling, which is the control on both refusals.
func TestOpenPool_RefusesAnUnsetURLAndAnswersAnUnbuildablePoolUnavailable(t *testing.T) {
	err := refusedStart(t, map[string]string{"GARAM_DATABASE_URL": ""})
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
	assert.EqualError(t, err, "GARAM_DATABASE_URL is required")

	err = refusedStart(t, map[string]string{"GARAM_DATABASE_URL": unreachableDatabase + "?pool_max_conns=none"})
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "got %v", err)

	// The control on both refusals: a well-formed URL opens a pool
	// without dialling, and the start goes on to the next variable.
	err = refusedStart(t, map[string]string{})
	assert.Contains(t, err.Error(), "GARAM_SESSION_TTL")
}

// TestPoolConfig_SetsTheIdleInTransactionTimeoutOnEveryConnection holds
// that the setting reaches pgx as a runtime parameter, which pgx sends
// in each connection's startup message, and that a value the URL names
// is the deployment's and is kept. The control is the parse the setting
// is added to, which carries none.
func TestPoolConfig_SetsTheIdleInTransactionTimeoutOnEveryConnection(t *testing.T) {
	const param = "idle_in_transaction_session_timeout"

	bare, err := pgxpool.ParseConfig(unreachableDatabase)
	require.NoError(t, err)
	assert.NotContains(t, bare.ConnConfig.RuntimeParams, param, "the control already carries the setting")

	cfg, err := cli.PoolConfig(unreachableDatabase)
	require.NoError(t, err)
	assert.Equal(t, "60000", cfg.ConnConfig.RuntimeParams[param])

	cfg, err = cli.PoolConfig(unreachableDatabase + "?" + param + "=5000")
	require.NoError(t, err)
	assert.Equal(t, "5000", cfg.ConnConfig.RuntimeParams[param], "a value the URL names is replaced")

	_, err = cli.PoolConfig(unreachableDatabase + "?pool_max_conns=none")
	assert.Error(t, err)
}
