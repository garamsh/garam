// Package cli owns the cobra command tree for the garam binary
// (ADR-0003): `serve api`, `serve authz`, and `migrate`. Commands are
// constructed here; cmd/garam only executes the root.
//
// Configuration is entirely via environment variables:
//
//	GARAM_DATABASE_URL     postgres connection string (required)
//	GARAM_MIGRATIONS_DIR   migrations directory (default "migrations")
//	GARAM_HTTP_ADDR        api listen address (default ":8080")
//	GARAM_GRPC_ADDR        authz listen address (default ":9090")
//	GARAM_METRICS_ADDR     authz /metrics listen address (default ":9091";
//	                       serve api answers /metrics on GARAM_HTTP_ADDR)
//	GARAM_AUTHZ_ADDR       authz engine address for serve api (default "localhost:9090")
//	GARAM_SESSION_TTL      session lifetime (default "168h")
//	GARAM_COOKIE_SECURE    "true" to mark cookies Secure (default true)
//	GARAM_DEV_AUTH         "true" to enable the dev sign-in provider (default false;
//	                       requires GARAM_COOKIE_SECURE=false)
//	GARAM_DEV_REDIRECT_URL dev provider callback URL
//	                       (default "http://localhost:3000/api/auth/callback/dev")
//	GOOGLE_CLIENT_ID       Google OAuth client ID (required for serve api)
//	GOOGLE_CLIENT_SECRET   Google OAuth client secret (required for serve api)
//	GOOGLE_REDIRECT_URL    OAuth callback URL (required for serve api)
package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// NewRootCommand constructs the garam command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "garam",
		Short: "garam is the workflow management core: identity, orgs, authorization",
	}
	root.AddCommand(newServeCommand(), newMigrateCommand())
	return root
}

// newServeCommand constructs the serve parent command.
func newServeCommand() *cobra.Command {
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Run a garam server",
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
