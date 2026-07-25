// Package cli owns the cobra command tree for the gaon binary
// (ADR-0003): `serve api`, `serve authz`, and `migrate`. Commands are
// constructed here; cmd/gaon only executes the root.
//
// Configuration is entirely via environment variables:
//
//	GAON_DATABASE_URL     postgres connection string (required)
//	GAON_MIGRATIONS_DIR   migrations directory (default "migrations")
//	GAON_HTTP_ADDR        api listen address (default ":8080")
//	GAON_GRPC_ADDR        authz listen address (default ":9090")
//	GAON_AUTHZ_ADDR       authz engine address for serve api (default "localhost:9090")
//	GAON_SESSION_TTL      session lifetime (default "168h")
//	GAON_COOKIE_SECURE    "true" to mark cookies Secure (default true)
//	GOOGLE_CLIENT_ID      Google OAuth client ID (required for serve api)
//	GOOGLE_CLIENT_SECRET  Google OAuth client secret (required for serve api)
//	GOOGLE_REDIRECT_URL   OAuth callback URL (required for serve api)
package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// NewRootCommand constructs the gaon command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "gaon",
		Short: "gaon is the workflow management core: identity, orgs, authorization",
	}
	root.AddCommand(newServeCommand(), newMigrateCommand())
	return root
}

// newServeCommand constructs the serve parent command.
func newServeCommand() *cobra.Command {
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Run a gaon server",
	}
	serve.AddCommand(newServeAPICommand(), newServeAuthzCommand())
	return serve
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
