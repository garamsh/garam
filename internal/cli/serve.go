package cli

import (
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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

	obs, err := newObservability(serviceAPI)
	if err != nil {
		return err
	}

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
		// Carries this request's trace id to the engine, so one id
		// covers both halves of the hop.
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return errorx.Wrap(err, "authz client", errorx.KindInvalidArgument)
	}
	defer func() { _ = conn.Close() }()

	ttl, err := time.ParseDuration(envOr("GARAM_SESSION_TTL", "168h"))
	if err != nil {
		return errorx.Wrap(err, "GARAM_SESSION_TTL", errorx.KindInvalidArgument)
	}
	cookieSecure := envOr("GARAM_COOKIE_SECURE", "true") == "true"
	dev, err := devProvider(envOr("GARAM_DEV_AUTH", "false") == "true", cookieSecure)
	if err != nil {
		return err
	}
	providers := []authn.Provider{google}
	if dev != nil {
		providers = append(providers, dev)
	}
	srv := api.NewServer(
		user.NewPostgresStore(pool),
		org.NewPostgresStore(pool),
		authn.NewPostgresSessionStore(pool),
		providers,
		api.NewGRPCEvaluator(conn),
		api.NewGRPCPolicyManager(conn),
		api.Config{
			SessionTTL:   ttl,
			CookieSecure: cookieSecure,
		},
	)

	// The generated router matches before the middleware runs, so the
	// log line can name the route rather than the path. /metrics
	// shares the router but not the middleware.
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", obs.metricsHandler())
	handler := apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []apigen.MiddlewareFunc{obs.logRequests},
	})

	addr := envOr("GARAM_HTTP_ADDR", ":8080")
	obs.logger.Info("serving api", slog.String("addr", addr))
	return http.ListenAndServe(addr, otelhttp.NewHandler(handler, serviceAPI))
}

// defaultDevRedirectURL returns the dev login flow to the web origin's
// /api rewrite on its conventional port.
const defaultDevRedirectURL = "http://localhost:3000/api/auth/callback/dev"

// devProvider resolves the development-only sign-in provider
// (ADR-0010): nil unless GARAM_DEV_AUTH opts in. It is localhost
// tooling, so the combination with Secure cookies — the
// production-shaped default — refuses to start.
func devProvider(enabled, cookieSecure bool) (authn.Provider, error) {
	if !enabled {
		return nil, nil
	}
	if cookieSecure {
		return nil, errorx.New(errorx.KindInvalidArgument, "GARAM_DEV_AUTH requires GARAM_COOKIE_SECURE=false")
	}
	p, err := authn.NewDevProvider(authn.DevConfig{
		RedirectURL: envOr("GARAM_DEV_REDIRECT_URL", defaultDevRedirectURL),
	})
	if err != nil {
		return nil, errorx.Wrap(err, "dev provider")
	}
	return p, nil
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

	obs, err := newObservability(serviceAuthz)
	if err != nil {
		return err
	}

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
			obs.logger.Error("policy change listener stopped", slog.String("error", err.Error()))
		}
	}()

	// The engine speaks gRPC only, so its metrics need a listener of
	// their own. Losing it must not take the engine down.
	metricsAddr := envOr("GARAM_METRICS_ADDR", ":9091")
	go func() {
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", obs.metricsHandler())
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			obs.logger.Error("metrics listener stopped", slog.String("error", err.Error()))
		}
	}()

	addr := envOr("GARAM_GRPC_ADDR", ":9090")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return errorx.Wrap(err, "authz listen", errorx.KindUnavailable)
	}
	srv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(obs.logRPCs),
	)
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer(cache))
	authzv1.RegisterPolicyServiceServer(srv, authz.NewPolicyGRPCServer(store, cache))
	obs.logger.Info("serving authz", slog.String("addr", addr), slog.String("metrics_addr", metricsAddr))
	if err := srv.Serve(lis); err != nil {
		return errorx.Wrap(err, "authz serve", errorx.KindInternal)
	}
	return nil
}
