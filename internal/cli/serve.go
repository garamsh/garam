package cli

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	auditpg "github.com/garamsh/garam/internal/audit/postgres"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/health"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	userpg "github.com/garamsh/garam/internal/user/postgres"
	"github.com/garamsh/garam/internal/watch"
	watchpg "github.com/garamsh/garam/internal/watch/postgres"
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

	pool, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	google, err := googleProvider(ctx, authn.GoogleConfig{
		ClientID:     envOr(envGoogleClientID, ""),
		ClientSecret: envOr(envGoogleClientSecret, ""),
		RedirectURL:  envOr(envGoogleRedirectURL, ""),
	})
	if err != nil {
		return err
	}

	conn, err := dialAuthz()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	ttl, err := time.ParseDuration(envOr(envSessionTTL, "168h"))
	if err != nil {
		return errorx.Wrap(err, string(envSessionTTL), errorx.KindInvalidArgument)
	}
	cookieSecure, err := envBool(envCookieSecure, "true")
	if err != nil {
		return err
	}
	tokenURL, err := operator.ParseTokenEndpoint(envOr(envMachineTokenURL, ""))
	if err != nil {
		return errorx.Wrap(err, string(envMachineTokenURL))
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
	watches, err := watch.NewService(watchpg.New(pool), watchpg.NewRelay(pool), watch.Config{})
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
		userpg.New(pool),
		orgpg.New(pool),
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
		authorityPresence{orgs: orgpg.New(pool)},
		NewAuthorityService(pool, conn),
		NewStandaloneService(pool, conn),
		api.Config{
			SessionTTL:   ttl,
			CookieSecure: cookieSecure,
			TokenURL:     tokenURL,
		},
	).WithCredentialAudit(auditpg.NewCredentialStore(pool))

	addr := envOr(envHTTPAddr, ":8080")
	// A request needs both to be served: rows out of the database, and
	// the decision that admits it.
	httpSrv := apiServer(addr, obs.apiHandler(srv, health.Database(pool), authzChannel{conn: conn}))
	obs.logger.Info("serving api", slog.String("addr", addr))
	return obs.serveHTTP(ctx, shutdownGrace, httpSrv, httpSrv.ListenAndServe)
}

// apiServer is the listener `serve api` answers on. It closes each
// connection after one response rather than keeping it alive. The
// console's `/api` rewrite reaches it through a pool whose idle
// sockets never time out and which ignores a Keep-Alive hint, so a
// request reusing a socket as the read deadline closes it was reset
// and answered 500 (#1428). The race is removed rather than narrowed:
// a longer IdleTimeout only moves it.
func apiServer(addr string, handler http.Handler) *http.Server {
	srv := &http.Server{
		Addr:        addr,
		Handler:     handler,
		ReadTimeout: requestReadTimeout,
	}
	srv.SetKeepAlivesEnabled(false)
	return srv
}

// apiHandler is everything `serve api` answers on its one port: the
// spec operations, and /metrics and the two probes beside them.
//
// The generated router matches before the middleware runs, so the log
// line can name the route rather than the path. The last middleware
// listed is the outermost, so recovery runs inside the logging and a
// panic becomes the 500 the line reports. recoverPanics is outermost of
// all, so every route the mux carries is answered rather than dropped,
// and the body bound sits under it for the same reason: one wrapping
// covers every route rather than each decoder in turn.
func (o *observability) apiHandler(srv apigen.ServerInterface, deps ...health.Dependency) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", o.metricsHandler())
	health.Register(mux, deps...)
	handler := apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{
		BaseRouter:  mux,
		Middlewares: []apigen.MiddlewareFunc{api.RecoverPanics, o.logRequests},
		// A parameter the router cannot bind is refused before any
		// handler runs, so this is the only place the surface's own
		// error body reaches that refusal.
		ErrorHandlerFunc: api.BindingError,
	})
	return o.recoverPanics(http.MaxBytesHandler(otelhttp.NewHandler(handler, serviceAPI), maxRequestBody))
}

// envBool reads a boolean environment variable, taking the fallback
// when it is unset. A value it cannot read is refused rather than
// defaulted: an unreadable value would otherwise land on false, and for
// GARAM_COOKIE_SECURE that is the weaker setting.
func envBool(key envName, fallback string) (bool, error) {
	b, err := strconv.ParseBool(envOr(key, fallback))
	if err != nil {
		return false, errorx.Wrap(err, string(key), errorx.KindInvalidArgument)
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

	pool, err := openPool(ctx)
	if err != nil {
		return err
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

	addr := envOr(envGRPCAddr, ":9090")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return errorx.Wrap(err, "authz listen", errorx.KindUnavailable)
	}

	// It starts after the gRPC port is bound, so an answer here means
	// that port accepts connections. Losing the listener must not take
	// the engine down.
	metricsAddr := envOr(envMetricsAddr, ":9091")
	go func() {
		// The engine decides from the cache, which fills from the
		// database and falls back to it on every miss.
		if err := http.ListenAndServe(metricsAddr, obs.engineProbeHandler(health.Database(pool))); err != nil {
			obs.logger.Error("metrics listener stopped", slog.String("error", err.Error()))
		}
	}()

	srv := obs.engineServer()
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer(cache))
	authzv1.RegisterPolicyServiceServer(srv, authz.NewPolicyGRPCServer(store, cache))
	obs.logger.Info("serving authz", slog.String("addr", addr), slog.String("metrics_addr", metricsAddr))
	if err := obs.serveGRPC(ctx, shutdownGrace, srv, func() error { return srv.Serve(lis) }); err != nil {
		return errorx.Wrap(err, "authz serve", errorx.KindInternal)
	}
	return nil
}

// engineProbeHandler is what `serve authz` answers on the port beside
// its gRPC one: the two probes, and /metrics, because the engine speaks
// gRPC only — a probe carries no gRPC client, and neither does a
// scraper (api-surface.md §The surfaces). This port carries no
// credential either.
//
// recoverPanics is outermost, as on every listener a serve command
// runs: a route added to this mux is covered by having been added.
func (o *observability) engineProbeHandler(deps ...health.Dependency) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", o.metricsHandler())
	health.Register(mux, deps...)
	return o.recoverPanics(mux)
}

// engineServer is the gRPC server `serve authz` serves the policy
// engine on.
//
// The first interceptor is the outermost, so recovery wraps the logging
// rather than the other way round: a panic is answered before it
// reaches a frame that would end the process, and the line it leaves is
// the only one.
func (o *observability) engineServer() *grpc.Server {
	return grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(o.recoverRPCs, o.logRPCs),
	)
}

// authorityPresence is whether an organization still holds a retired
// certificate authority, as the browser listener reads it
// (api.AuthorityReader): presence and nothing else, the one read of
// certificate_authorities left (ADR-0118). Deleting the organization
// destroys that sealed row, and this is what the deletion preview warns
// of; nothing here opens or decodes it.
type authorityPresence struct {
	orgs *orgpg.Store
}

func (a authorityPresence) HasAuthority(ctx context.Context, orgGRN grn.GRN) (bool, error) {
	return a.orgs.HoldsCertificateAuthority(ctx, orgGRN)
}
