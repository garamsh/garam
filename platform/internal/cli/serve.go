package cli

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/api"
	auditpg "github.com/garamsh/garam/platform/internal/audit/postgres"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	clusterpg "github.com/garamsh/garam/platform/internal/cluster/postgres"
	"github.com/garamsh/garam/platform/internal/health"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
	"github.com/garamsh/garam/platform/internal/watch"
	watchpg "github.com/garamsh/garam/platform/internal/watch/postgres"
)

// newServeAPICommand constructs `garam serve api`: the OpenAPI HTTP
// backend. Its decisions come from the policy service (ADR-0146).
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

	// A user's delegated authority is the policy service's decision
	// (ADR-0146), so a process without one does not start.
	decisions, relay, closePolicy, err := dialPolicyClients()
	if err != nil {
		return err
	}
	defer closePolicy()

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

	sessions := authnpg.NewSessionStore(pool)
	issuer, err := identityIssuer(sessions, obs.logger)
	if err != nil {
		return err
	}
	srv := api.NewServer(
		userpg.New(pool),
		orgpg.New(pool),
		agentpg.New(pool),
		operatorpg.New(pool),
		messagepg.New(pool),
		watches,
		sessions,
		authnpg.NewOAuthStateStore(pool),
		authnpg.NewPasswordCredentialStore(pool),
		authnpg.NewPasswordAttemptStore(pool),
		providers,
		decisions,
		authorityPresence{orgs: orgpg.New(pool)},
		NewAuthorityService(pool, decisions),
		NewStandaloneService(pool, decisions),
		api.Config{
			SessionTTL:   ttl,
			CookieSecure: cookieSecure,
			TokenURL:     tokenURL,
		},
	).WithCredentialAudit(auditpg.NewCredentialStore(pool)).
		WithDeviceGrant(authnpg.NewDeviceStore(pool)).
		WithIdentity(issuer).
		WithPolicyRelay(relay).
		WithClusters(clusterpg.New(pool), clusterpg.New(pool))

	gate, stopDelivery, err := startPolicyDelivery(ctx, pool, obs.logger, obs.registry)
	if err != nil {
		return err
	}
	defer stopDelivery()
	srv.WithFeed(gate)

	addr := envOr(envHTTPAddr, ":8080")
	httpSrv := apiServer(addr, obs.apiHandler(srv, health.Database(pool)))
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
