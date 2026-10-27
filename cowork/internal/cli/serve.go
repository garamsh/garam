package cli

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/cowork/internal/api"
	"github.com/garamsh/garam/cowork/internal/collab"
	collabgoja "github.com/garamsh/garam/cowork/internal/collab/goja"
	collabmemory "github.com/garamsh/garam/cowork/internal/collab/memory"
	collabpg "github.com/garamsh/garam/cowork/internal/collab/postgres"
	"github.com/garamsh/garam/cowork/internal/page"
	pagepg "github.com/garamsh/garam/cowork/internal/page/postgres"
	platformhttp "github.com/garamsh/garam/cowork/internal/platform/http"
	"github.com/garamsh/garam/platform/sdk/errorx"
)

// shutdownGrace is how long serve waits for requests in flight once
// asked to stop.
const shutdownGrace = 10 * time.Second

// Config is what serve wires the server from.
type Config struct {
	// PlatformURL is the root of the platform's browser API.
	PlatformURL string
	// WebOrigins are the host patterns a browser may open a sync
	// WebSocket from, besides cowork's own host.
	WebOrigins []string
}

// NewHandler assembles cowork's HTTP surface over pool as serve ships
// it. A test that drives the wired server calls this rather than
// assembling its own.
func NewHandler(cfg Config, pool *pgxpool.Pool) (http.Handler, error) {
	p, err := platformhttp.New(cfg.PlatformURL)
	if err != nil {
		return nil, err
	}
	engine, err := collabgoja.New()
	if err != nil {
		return nil, err
	}
	pages := page.NewService(pagepg.New(pool), p)
	collabs := collab.NewService(collabpg.New(pool), collabmemory.New(), engine)
	return api.NewServer(pages, collabs, p, cfg.WebOrigins).Handler(pool.Ping), nil
}

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve the page tree and real-time editing",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			url, platformURL := envOr(envDatabaseURL, ""), envOr(envPlatformURL, "")
			if url == "" || platformURL == "" {
				return errorx.New(errorx.KindInvalidArgument, envDatabaseURL+" and "+envPlatformURL+" are required")
			}
			pool, err := pgxpool.New(ctx, url)
			if err != nil {
				return errorx.Wrap(err, "database", errorx.KindInvalidArgument)
			}
			defer pool.Close()
			handler, err := NewHandler(Config{PlatformURL: platformURL, WebOrigins: listOf(envOr(envWebOrigins, ""))}, pool)
			if err != nil {
				return err
			}
			// Hijacked WebSockets outlive Shutdown, so their context is
			// the server's, and ends with it.
			srv := &http.Server{
				Addr:              envOr(envListenAddr, ":8090"),
				Handler:           handler,
				ReadHeaderTimeout: 10 * time.Second,
				BaseContext:       func(net.Listener) context.Context { return ctx },
			}
			errc := make(chan error, 1)
			go func() { errc <- srv.ListenAndServe() }()
			slog.InfoContext(ctx, "cowork serving", slog.String("addr", srv.Addr))
			select {
			case err := <-errc:
				return errorx.Wrap(err, "serve", errorx.KindUnavailable)
			case <-ctx.Done():
			}
			shutdown, cancel := context.WithTimeout(context.Background(), shutdownGrace)
			defer cancel()
			if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return errorx.Wrap(err, "shutdown", errorx.KindInternal)
			}
			return nil
		},
	}
}
