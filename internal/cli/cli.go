// Package cli owns the cobra command tree for the garam binary
// (ADR-0003, ADR-0017): `serve api`, `serve authz`, `serve machine`,
// `migrate`, `probe`, and `issue-certificate`. Commands are constructed
// here; cmd/garam only executes the root.
//
// `issue-certificate` is the one bootstrap ADR-0015 leaves out of
// band: it mints an operator's own first certificate, for operator
// subjects only, and no serve path reaches it, so no serving process
// can mint an identity.
//
// Every serve command answers the two probes internal/health mounts —
// liveness and readiness — on a listener that asks for no credential:
// the api's own HTTP port, the authz engine's metrics port, and a port
// of its own for `serve machine`, whose listener admits nobody without
// a certificate.
//
// Configuration is entirely via environment variables:
//
//	GARAM_DATABASE_URL     postgres connection string (required)
//	GARAM_MIGRATIONS_DIR   migrations directory (default "migrations")
//	GARAM_HTTP_ADDR        api listen address (default ":8080")
//	GARAM_GRPC_ADDR        authz listen address (default ":9090")
//	GARAM_METRICS_ADDR     authz /metrics and probe listen address
//	                       (default ":9091"; serve api answers both on
//	                       GARAM_HTTP_ADDR)
//	GARAM_AUTHZ_ADDR       authz engine address for serve api (default "localhost:9090")
//	GARAM_SESSION_TTL      session lifetime (default "168h")
//	GARAM_COOKIE_SECURE    "true" to mark cookies Secure (default true)
//	GARAM_DEV_AUTH         "true" to enable the dev sign-in provider (default false;
//	                       requires GARAM_COOKIE_SECURE=false)
//	GARAM_DEV_REDIRECT_URL dev provider callback URL
//	                       (default "http://localhost:3000/api/auth/callback/dev")
//	GOOGLE_CLIENT_ID       Google OAuth client ID (optional; both
//	                       credentials or neither — unset, the google
//	                       provider is not registered)
//	GOOGLE_CLIENT_SECRET   Google OAuth client secret (optional)
//	GOOGLE_REDIRECT_URL    OAuth callback URL
//	GARAM_MACHINE_ADDR     machine listener address (default ":8443")
//	GARAM_MACHINE_HEALTH_ADDR  serve machine's probe listen address
//	                       (default ":8444"); the machine listener
//	                       itself admits no caller without a certificate
//	GARAM_MACHINE_TLS_CERT_FILE  the listener's own certificate, PEM
//	                       (required for serve machine)
//	GARAM_MACHINE_TLS_KEY_FILE   its private key, PEM
//	                       (required for serve machine)
//	GARAM_CA_KEY_FILE      hex-encoded key-encryption key the certificate
//	                       authorities are sealed under, readable by its
//	                       owner alone (required for serve machine and
//	                       issue-certificate)
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
	root.AddCommand(newServeCommand(), newMigrateCommand(), newProbeCommand(), newIssueCertificateCommand())
	return root
}

// newServeCommand constructs the serve parent command.
func newServeCommand() *cobra.Command {
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Run a garam server",
	}
	serve.AddCommand(newServeAPICommand(), newServeAuthzCommand(), newServeMachineCommand())
	return serve
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
