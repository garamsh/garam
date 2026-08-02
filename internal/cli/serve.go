package cli

import (
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// newServeAPICommand constructs `garam serve api`: the OpenAPI HTTP
// backend. It reaches the authz engine over gRPC (ADR-0003 stage 2).
func newServeAPICommand() *cobra.Command {
	return &cobra.Command{
		Use:   "api",
		Short: "Serve the OpenAPI HTTP backend",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serveAPI(cmd)
		},
	}
}

func serveAPI(cmd *cobra.Command) error {
	ctx := cmd.Context()

	databaseURL := envOr("GARAM_DATABASE_URL", "")
	if databaseURL == "" {
		return errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errorx.Wrap(err, "postgres", errorx.KindUnavailable)
	}
	defer pool.Close()

	google, err := authn.NewGoogleProvider(ctx, authn.GoogleConfig{
		ClientID:     envOr("GOOGLE_CLIENT_ID", ""),
		ClientSecret: envOr("GOOGLE_CLIENT_SECRET", ""),
		RedirectURL:  envOr("GOOGLE_REDIRECT_URL", ""),
	})
	if err != nil {
		return errorx.Wrap(err, "google provider")
	}

	// Insecure transport: both processes run inside one trust
	// boundary; TLS between them is future work.
	conn, err := grpc.NewClient(
		envOr("GARAM_AUTHZ_ADDR", "localhost:9090"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return errorx.Wrap(err, "authz client", errorx.KindInvalidArgument)
	}
	defer func() { _ = conn.Close() }()

	ttl, err := time.ParseDuration(envOr("GARAM_SESSION_TTL", "168h"))
	if err != nil {
		return errorx.Wrap(err, "GARAM_SESSION_TTL", errorx.KindInvalidArgument)
	}
	srv := api.NewServer(
		user.NewPostgresStore(pool),
		org.NewPostgresStore(pool),
		authn.NewPostgresSessionStore(pool),
		[]authn.Provider{google},
		api.NewGRPCEvaluator(conn),
		api.NewGRPCPolicyManager(conn),
		api.Config{
			SessionTTL:   ttl,
			CookieSecure: envOr("GARAM_COOKIE_SECURE", "true") == "true",
		},
	)

	addr := envOr("GARAM_HTTP_ADDR", ":8080")
	cmd.Printf("serving api on %s\n", addr)
	return http.ListenAndServe(addr, apigen.Handler(srv))
}

// newServeAuthzCommand constructs `garam serve authz`: the gRPC policy
// engine, run as a separate process from the api server (ADR-0003).
func newServeAuthzCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "authz",
		Short: "Serve the policy engine over gRPC",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serveAuthz(cmd)
		},
	}
}

func serveAuthz(cmd *cobra.Command) error {
	ctx := cmd.Context()

	databaseURL := envOr("GARAM_DATABASE_URL", "")
	if databaseURL == "" {
		return errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errorx.Wrap(err, "postgres", errorx.KindUnavailable)
	}
	defer pool.Close()

	store := authz.NewPostgresStore(pool)
	cache := authz.NewPolicyCache(store)
	// Writes made here invalidate the cache directly; the listener
	// covers writes made by any other process.
	go func() {
		if err := authz.ListenPolicyChanges(ctx, pool, cache); err != nil {
			cmd.PrintErrf("authz: policy change listener stopped: %v\n", err)
		}
	}()

	addr := envOr("GARAM_GRPC_ADDR", ":9090")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return errorx.Wrap(err, "authz listen", errorx.KindUnavailable)
	}
	srv := grpc.NewServer()
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer(cache))
	authzv1.RegisterPolicyServiceServer(srv, authz.NewPolicyGRPCServer(store, cache))
	cmd.Printf("serving authz on %s\n", addr)
	if err := srv.Serve(lis); err != nil {
		return errorx.Wrap(err, "authz serve", errorx.KindInternal)
	}
	return nil
}
