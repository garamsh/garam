package cli

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/cowork/internal/api"
	"github.com/garamsh/garam/cowork/internal/artifact"
	artifactpg "github.com/garamsh/garam/cowork/internal/artifact/postgres"
	"github.com/garamsh/garam/cowork/internal/blob"
	blobmemory "github.com/garamsh/garam/cowork/internal/blob/memory"
	blobs3 "github.com/garamsh/garam/cowork/internal/blob/s3"
	"github.com/garamsh/garam/cowork/internal/channel"
	channelpg "github.com/garamsh/garam/cowork/internal/channel/postgres"
	"github.com/garamsh/garam/cowork/internal/collab"
	collabgoja "github.com/garamsh/garam/cowork/internal/collab/goja"
	collabpg "github.com/garamsh/garam/cowork/internal/collab/postgres"
	fanoutmemory "github.com/garamsh/garam/cowork/internal/fanout/memory"
	"github.com/garamsh/garam/cowork/internal/page"
	pagepg "github.com/garamsh/garam/cowork/internal/page/postgres"
	platformhttp "github.com/garamsh/garam/cowork/internal/platform/http"
	"github.com/garamsh/garam/cowork/internal/task"
	taskpg "github.com/garamsh/garam/cowork/internal/task/postgres"
	"github.com/garamsh/garam/cowork/internal/usercontent"
	"github.com/garamsh/garam/lib/errorx"
)

// shutdownGrace is how long serve waits for requests in flight once
// asked to stop.
const shutdownGrace = 10 * time.Second

// Config is what serve wires the server from.
type Config struct {
	// PlatformURL is the root of the platform's browser API.
	PlatformURL string
	// WebOrigins are the host patterns a browser may open a sync
	// WebSocket from, besides cowork's own host, and frame an artifact
	// in.
	WebOrigins []string
	// Blobs is where artifact versions are kept; nil keeps none, and
	// answers every artifact call unavailable.
	Blobs blob.Store
	// UserContent signs the addresses HTML artifacts are viewed at;
	// nil views none.
	UserContent *usercontent.Signer
	// SocketRecheck is how often an open WebSocket's caller is asked
	// again; zero is api.SocketRecheck.
	SocketRecheck time.Duration
}

// services are cowork's services as one Config wires them, which the
// API and the user-content origin share.
type services struct {
	platform  *platformhttp.Client
	pages     page.Service
	collab    collab.Service
	channels  channel.Service
	tasks     task.Service
	artifacts artifact.Service
}

func wire(cfg Config, pool *pgxpool.Pool) (services, error) {
	p, err := platformhttp.New(cfg.PlatformURL)
	if err != nil {
		return services{}, err
	}
	engine, err := collabgoja.New()
	if err != nil {
		return services{}, err
	}
	// One hub serves every room: a page's sync and a channel's readers
	// name theirs apart.
	hub := fanoutmemory.New()
	pages := page.NewService(pagepg.New(pool), p)
	collabs := collab.NewService(collabpg.New(pool), hub, engine)
	channels := channel.NewService(channelpg.New(pool), p, hub)
	var viewer artifact.Viewer
	if cfg.UserContent != nil {
		viewer = cfg.UserContent
	}
	return services{
		platform: p, pages: pages, collab: collabs, channels: channels,
		tasks:     task.NewService(taskpg.New(pool), p, pages, channels),
		artifacts: artifact.NewService(artifactpg.New(pool), cfg.Blobs, p, pages, collabs, viewer),
	}, nil
}

// NewHandler assembles cowork's HTTP surface over pool as serve ships
// it, for serve and for the suites that drive the wired server.
func NewHandler(cfg Config, pool *pgxpool.Pool) (http.Handler, error) {
	s, err := wire(cfg, pool)
	if err != nil {
		return nil, err
	}
	recheck := cfg.SocketRecheck
	if recheck == 0 {
		recheck = api.SocketRecheck
	}
	return api.NewServer(s.pages, s.collab, s.channels, s.tasks, s.artifacts, s.platform, cfg.WebOrigins, recheck).Handler(pool.Ping), nil
}

// NewUserContentHandler assembles the user-content origin over pool:
// HTML artifact versions at the addresses cfg.UserContent signs, and
// nothing else.
func NewUserContentHandler(cfg Config, pool *pgxpool.Pool) (http.Handler, error) {
	if cfg.UserContent == nil {
		return nil, errorx.New(errorx.KindInvalidArgument, "the user-content origin needs its signer")
	}
	s, err := wire(cfg, pool)
	if err != nil {
		return nil, err
	}
	return cfg.UserContent.Handler(s.artifacts, cfg.WebOrigins), nil
}

// blobStore is the store envBlobStore names: s3 over the endpoint,
// bucket and keys the environment gives, memory for a development
// server, or none.
func blobStore(ctx context.Context) (blob.Store, error) {
	switch kind := envOr(envBlobStore, ""); kind {
	case "":
		slog.WarnContext(ctx, "cowork: no blob store configured; artifacts are unavailable", slog.String("set", envBlobStore))
		return nil, nil
	case "memory":
		slog.WarnContext(ctx, "cowork: artifacts are kept in memory and lost when the server stops")
		return blobmemory.New(), nil
	case "s3":
		pathStyle, _ := strconv.ParseBool(envOr(envS3PathStyle, "false"))
		store, err := blobs3.New(blobs3.Config{
			Endpoint: envOr(envS3Endpoint, ""), Region: envOr(envS3Region, ""), Bucket: envOr(envS3Bucket, ""),
			AccessKey: envOr(envS3AccessKey, ""), SecretKey: envOr(envS3SecretKey, ""), PathStyle: pathStyle,
		})
		if err != nil {
			return nil, err
		}
		if create, _ := strconv.ParseBool(envOr(envS3CreateBucket, "false")); create {
			if err := store.EnsureBucket(ctx); err != nil {
				return nil, err
			}
		}
		return store, nil
	default:
		return nil, errorx.New(errorx.KindInvalidArgument, envBlobStore+" is s3 or memory, not "+kind)
	}
}

// userContent is the signer for the origin envUserContentOrigin
// names, nil where it names none.
func userContent() (*usercontent.Signer, error) {
	origin := envOr(envUserContentOrigin, "")
	if origin == "" {
		return nil, nil
	}
	return usercontent.NewSigner(origin, []byte(envOr(envUserContentSecret, "")))
}

func newServeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Serve pages, messages, tasks, artifacts and the MCP tools",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			url, platformURL := envOr(envDatabaseURL, ""), envOr(envPlatformURL, "")
			if url == "" || platformURL == "" {
				return errorx.New(errorx.KindInvalidArgument, envDatabaseURL+" and "+envPlatformURL+" are required")
			}
			blobs, err := blobStore(ctx)
			if err != nil {
				return err
			}
			signer, err := userContent()
			if err != nil {
				return err
			}
			pool, err := pgxpool.New(ctx, url)
			if err != nil {
				return errorx.Wrap(err, "database", errorx.KindInvalidArgument)
			}
			defer pool.Close()
			cfg := Config{PlatformURL: platformURL, WebOrigins: listOf(envOr(envWebOrigins, "")), Blobs: blobs, UserContent: signer}
			handler, err := NewHandler(cfg, pool)
			if err != nil {
				return err
			}
			// Hijacked WebSockets outlive Shutdown, so their context is
			// the server's, and ends with it.
			servers := []*http.Server{{
				Addr:              envOr(envListenAddr, ":8090"),
				Handler:           handler,
				ReadHeaderTimeout: 10 * time.Second,
				BaseContext:       func(net.Listener) context.Context { return ctx },
			}}
			if signer != nil {
				content, err := NewUserContentHandler(cfg, pool)
				if err != nil {
					return err
				}
				servers = append(servers, &http.Server{
					Addr:              envOr(envUserContentAddr, ":8092"),
					Handler:           content,
					ReadHeaderTimeout: 10 * time.Second,
				})
			}
			errc := make(chan error, len(servers))
			for _, srv := range servers {
				go func() { errc <- srv.ListenAndServe() }()
				slog.InfoContext(ctx, "cowork serving", slog.String("addr", srv.Addr))
			}
			select {
			case err := <-errc:
				return errorx.Wrap(err, "serve", errorx.KindUnavailable)
			case <-ctx.Done():
			}
			shutdown, cancel := context.WithTimeout(context.Background(), shutdownGrace)
			defer cancel()
			for _, srv := range servers {
				if err := srv.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
					return errorx.Wrap(err, "shutdown", errorx.KindInternal)
				}
			}
			return nil
		},
	}
}
