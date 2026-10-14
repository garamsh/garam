package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
)

// TestOpenPool_RefusesAnUnsetURLAndAnswersAnUnbuildablePoolUnavailable
// holds the policy every command opening the database shares: no URL
// is the caller's mistake, a URL no pool can be built from is
// unavailable, and a well-formed one opens a pool without dialling,
// which is the control on both refusals.
func TestOpenPool_RefusesAnUnsetURLAndAnswersAnUnbuildablePoolUnavailable(t *testing.T) {
	t.Setenv(string(envDatabaseURL), "")
	_, err := openPool(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
	assert.EqualError(t, err, "GARAM_DATABASE_URL is required")

	t.Setenv(string(envDatabaseURL), "postgres://garam@127.0.0.1:1/garam?pool_max_conns=none")
	_, err = openPool(context.Background())
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "got %v", err)

	t.Setenv(string(envDatabaseURL), "postgres://garam@127.0.0.1:1/garam")
	pool, err := openPool(context.Background())
	require.NoError(t, err)
	pool.Close()
}

// TestDialAuthz_TargetsTheConfiguredEngineOrTheDefault holds where the
// serve commands reach the policy engine: GARAM_AUTHZ_ADDR where set,
// localhost:9090 otherwise.
func TestDialAuthz_TargetsTheConfiguredEngineOrTheDefault(t *testing.T) {
	t.Setenv(string(envAuthzAddr), "")
	conn, err := dialAuthz()
	require.NoError(t, err)
	assert.Equal(t, "localhost:9090", conn.Target())
	require.NoError(t, conn.Close())

	t.Setenv(string(envAuthzAddr), "authz.garam.test:9090")
	conn, err = dialAuthz()
	require.NoError(t, err)
	assert.Equal(t, "authz.garam.test:9090", conn.Target())
	require.NoError(t, conn.Close())
}
