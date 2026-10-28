package cli

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/garam/internal/errorx"
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

// dialAuthz builds the client the serve commands reach the policy
// engine through, at GARAM_AUTHZ_ADDR (ADR-0003). grpc.NewClient
// connects on the first call, so a refusal here is a malformed target
// and never an unreachable engine. The caller closes it.
//
// Insecure transport: both processes run inside one trust boundary;
// TLS between them is future work.
func dialAuthz() (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		envOr(envAuthzAddr, "localhost:9090"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Carries this request's trace id to the engine, so one id
		// covers both halves of the hop.
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, errorx.Wrap(err, "authz client", errorx.KindInvalidArgument)
	}
	return conn, nil
}
