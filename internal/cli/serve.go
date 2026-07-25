package cli

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	apigen "github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/api"
	"github.com/garamsh/gaon/internal/authn"
	"github.com/garamsh/gaon/internal/authz"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
)

// newServeAPICommand constructs `gaon serve api`: the OpenAPI HTTP
// backend. Stage 1 runs the authz evaluator in process (ADR-0003
// §Staged adoption); the gRPC engine replaces it in stage 2.
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

	databaseURL := envOr("GAON_DATABASE_URL", "")
	if databaseURL == "" {
		return errors.New("GAON_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	defer pool.Close()

	google, err := authn.NewGoogleProvider(ctx, authn.GoogleConfig{
		ClientID:     envOr("GOOGLE_CLIENT_ID", ""),
		ClientSecret: envOr("GOOGLE_CLIENT_SECRET", ""),
		RedirectURL:  envOr("GOOGLE_REDIRECT_URL", ""),
	})
	if err != nil {
		return fmt.Errorf("google provider: %w", err)
	}

	ttl, err := time.ParseDuration(envOr("GAON_SESSION_TTL", "168h"))
	if err != nil {
		return fmt.Errorf("GAON_SESSION_TTL: %w", err)
	}
	srv := api.NewServer(
		user.NewPostgresStore(pool),
		org.NewPostgresStore(pool),
		authn.NewPostgresSessionStore(pool),
		[]authn.Provider{google},
		api.Config{
			SessionTTL:   ttl,
			CookieSecure: envOr("GAON_COOKIE_SECURE", "true") == "true",
		},
	)

	addr := envOr("GAON_HTTP_ADDR", ":8080")
	cmd.Printf("serving api on %s\n", addr)
	return http.ListenAndServe(addr, apigen.Handler(srv))
}

// newServeAuthzCommand constructs `gaon serve authz`. The gRPC
// transport lands in stage 2 (ADR-0003); for now the command proves
// the wiring by constructing the in-process native evaluator.
func newServeAuthzCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "authz",
		Short: "Serve the policy engine (gRPC transport lands in P2)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = authz.NewStatementEvaluator(nil)
			cmd.Println("authz runs in-process in stage 1; the gRPC transport lands in P2")
			return nil
		},
	}
}
