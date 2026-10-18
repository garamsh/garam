package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDialAuthz_TargetsTheConfiguredEngineOrTheDefault holds where the
// serve commands reach the policy engine: GARAM_AUTHZ_ADDR where set,
// localhost:9090 otherwise.
func TestDialAuthz_TargetsTheConfiguredEngineOrTheDefault(t *testing.T) {
	// Internal: no exported surface reads the channel's target.
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
