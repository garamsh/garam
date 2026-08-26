package cli

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/health"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	"github.com/garamsh/garam/internal/watch"
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
	ctx, stop := serveSignalContext(cmd.Context())
	defer stop()

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

	google, err := googleProvider(ctx, authn.GoogleConfig{
		ClientID:     envOr("GOOGLE_CLIENT_ID", ""),
		ClientSecret: envOr("GOOGLE_CLIENT_SECRET", ""),
		RedirectURL:  envOr("GOOGLE_REDIRECT_URL", ""),
	})
	if err != nil {
		return err
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
	cookieSecure, err := envBool("GARAM_COOKIE_SECURE", "true")
	if err != nil {
		return err
	}
	providers := []authn.Provider{}
	if google != nil {
		providers = append(providers, google)
	}
	// A watch is a subscription a browser opens here, and the events it
	// reads were forwarded to the machine listener, which is another
	// process (ADR-0017): this one learns of them over the relay and by
	// no other route. A process holding no stream delivers nothing, so
	// receiving runs where the streams are and nowhere else. Losing it
	// leaves the live view dark and every durable path untouched
	// (ADR-0022).
	watches, err := watch.NewService(watch.NewPostgresStore(pool), watch.NewPostgresRelay(pool), watch.Config{})
	if err != nil {
		return err
	}
	go func() {
		// A stopped relay is an error only when the stop was not asked
		// for: shutdown cancels ctx.
		if err := watches.Receive(ctx); err != nil && !errors.Is(err, context.Canceled) {
			obs.logger.Error("turn event relay stopped", slog.String("error", err.Error()))
		}
	}()

	srv := api.NewServer(
		user.NewPostgresStore(pool),
		org.NewPostgresStore(pool),
		agent.NewPostgresStore(pool),
		operator.NewPostgresStore(pool),
		message.NewPostgresStore(pool),
		watches,
		authn.NewPostgresSessionStore(pool),
		authn.NewPostgresOAuthStateStore(pool),
		authn.NewPostgresPasswordCredentialStore(pool),
		authn.NewPostgresPasswordAttemptStore(pool),
		providers,
		api.NewGRPCEvaluator(conn),
		api.NewGRPCPolicyManager(conn),
		api.Config{
			SessionTTL:   ttl,
			CookieSecure: cookieSecure,
		},
	)

	// The generated router matches before the middleware runs, so the
	// log line can name the route rather than the path. /metrics and
	// the two probes share the router; the generated slice reaches
	// neither, because oapi-codegen applies it inside each operation's
	// own wrapper rather than around the router, so recoverPanics below
	// is what covers them.
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", obs.metricsHandler())
	// A request needs both to be served: rows out of the database, and
	// the decision that admits it.
	health.Register(mux, health.Database(pool), authzChannel{conn: conn})
	// The last middleware listed is the outermost, so recovery runs
	// inside the logging: a panic becomes the 500 the line reports
	// rather than an unwind that leaves no line at all.
	handler := apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []apigen.MiddlewareFunc{api.RecoverPanics, obs.logRequests},
		// A parameter the router cannot bind is refused before any
		// handler runs, so this is the only place the surface's own
		// error body reaches that refusal.
		ErrorHandlerFunc: api.BindingError,
	})

	addr := envOr("GARAM_HTTP_ADDR", ":8080")
	// Outermost, so every route this mux carries is answered rather than
	// dropped — the spec operations, /metrics, the probes, and a path
	// that matches none of them.
	httpSrv := &http.Server{
		Addr:        addr,
		Handler:     obs.recoverPanics(otelhttp.NewHandler(handler, serviceAPI)),
		ReadTimeout: requestReadTimeout,
	}
	obs.logger.Info("serving api", slog.String("addr", addr))
	return serveHTTP(ctx, httpSrv, httpSrv.ListenAndServe)
}

// envBool reads a boolean environment variable, taking the fallback
// when it is unset. A value it cannot read is refused rather than
// defaulted: an unreadable value would otherwise land on false, and for
// GARAM_COOKIE_SECURE that is the weaker setting.
func envBool(key, fallback string) (bool, error) {
	b, err := strconv.ParseBool(envOr(key, fallback))
	if err != nil {
		return false, errorx.Wrap(err, key, errorx.KindInvalidArgument)
	}
	return b, nil
}

// googleProvider resolves the Google sign-in provider: nil unless the
// credentials are configured. OIDC discovery runs at construction, so
// a registered provider has already reached its issuer — a process
// without outbound connectivity pays that round trip only when sign-in
// through Google was asked for, and a discovery failure stops startup
// rather than the first sign-in. One credential without the other is a
// misconfiguration, not half a provider.
func googleProvider(ctx context.Context, cfg authn.GoogleConfig) (authn.Provider, error) {
	if cfg.ClientID == "" && cfg.ClientSecret == "" {
		return nil, nil
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are set together or not at all")
	}
	p, err := authn.NewGoogleProvider(ctx, cfg)
	if err != nil {
		return nil, errorx.Wrap(err, "google provider")
	}
	return p, nil
}

// authzChannel is the connection to the policy engine as readiness
// sees it (health.Dependency). Every org-scoped request is decided
// there, so a channel that cannot carry a decision is a reason to send
// requests to another replica.
type authzChannel struct{ conn *grpc.ClientConn }

func (authzChannel) Name() string { return "authz" }

// Check waits for the channel to reach Ready inside ctx, connecting it
// when it is idle. The state alone would answer for a channel that has
// never reached the engine — grpc.NewClient connects on the first call
// — and answering with an RPC would mean sending the engine one it
// acts on.
func (a authzChannel) Check(ctx context.Context) error {
	for {
		state := a.conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		a.conn.Connect()
		if !a.conn.WaitForStateChange(ctx, state) {
			return errorx.New(errorx.KindUnavailable, "authz channel is "+state.String())
		}
	}
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
	ctx, stop := serveSignalContext(cmd.Context())
	defer stop()

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
		if err := authz.ListenPolicyChanges(ctx, pool, cache); err != nil && !errors.Is(err, context.Canceled) {
			obs.logger.Error("policy change listener stopped", slog.String("error", err.Error()))
		}
	}()

	addr := envOr("GARAM_GRPC_ADDR", ":9090")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return errorx.Wrap(err, "authz listen", errorx.KindUnavailable)
	}

	// The engine speaks gRPC only, so its metrics and its probes need a
	// listener of their own — a probe carries no gRPC client, and this
	// one carries no credential either. It starts after the gRPC port
	// is bound, so an answer here means that port accepts connections.
	// Losing the listener must not take the engine down.
	metricsAddr := envOr("GARAM_METRICS_ADDR", ":9091")
	go func() {
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", obs.metricsHandler())
		// The engine decides from the cache, which fills from the
		// database and falls back to it on every miss.
		health.Register(mux, health.Database(pool))
		if err := http.ListenAndServe(metricsAddr, obs.recoverPanics(mux)); err != nil {
			obs.logger.Error("metrics listener stopped", slog.String("error", err.Error()))
		}
	}()

	srv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(obs.logRPCs),
	)
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer(cache))
	authzv1.RegisterPolicyServiceServer(srv, authz.NewPolicyGRPCServer(store, cache))
	obs.logger.Info("serving authz", slog.String("addr", addr), slog.String("metrics_addr", metricsAddr))
	if err := serveGRPC(ctx, srv, func() error { return srv.Serve(lis) }); err != nil {
		return errorx.Wrap(err, "authz serve", errorx.KindInternal)
	}
	return nil
}
