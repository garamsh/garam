package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/internal/errorx"
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
