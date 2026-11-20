package cli

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/errorx"
)

// openPool opens the database GARAM_DATABASE_URL names, refusing a
// command that sets none. A pool that cannot be built is unavailable,
// not the caller's mistake. The caller closes it.
func openPool(ctx context.Context) (*pgxpool.Pool, error) {
	databaseURL := envOr(envDatabaseURL, "")
	if databaseURL == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, errorx.Wrap(err, "postgres", errorx.KindUnavailable)
	}
	return pool, nil
}
