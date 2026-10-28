package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/health"
)

// TestHealthDatabase_AnswersForTheDatabaseItWasGiven pairs the two
// cases against real infrastructure: the readiness check every serve
// command carries passes against the database that is there and fails
// against the one that is not. A pool is lazy, so the difference
// appears at the check and nowhere earlier.
func TestHealthDatabase_AnswersForTheDatabaseItWasGiven(t *testing.T) {
	ctx := context.Background()

	require.NoError(t, health.Database(pool(t)).Check(ctx))

	// Port 1 is reserved and nothing serves Postgres on it.
	gone, err := pgxpool.New(ctx, "postgres://garam:garam@127.0.0.1:1/garam?sslmode=disable")
	require.NoError(t, err)
	t.Cleanup(gone.Close)

	err = health.Database(gone).Check(ctx)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
}
