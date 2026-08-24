// Package cli owns the cobra command tree for the garam binary
// (ADR-0003, ADR-0017): `serve api`, `serve authz`, `serve machine`,
// `migrate`, `probe`, `issue-certificate`, and `adapter`. Commands are
// constructed here; cmd/garam only executes the root.
//
// `adapter` is the one command that serves nothing and connects
// outward: the delivery loop docs/architecture/adapter.md specifies,
// run beside one agent by whoever deploys that agent.
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
//	GARAM_MACHINE_TLS_KEY_FILE   its private key, PEM, readable by its
//	                       owner alone (required for serve machine).
//	                       Both files are re-read per handshake, so a
//	                       replaced pair is served without a restart
//	GARAM_MACHINE_SERVER_ROOT_FILE  the garam server root that signed the
//	                       pair above, PEM, handed to every identity that
//	                       receives a certificate (required for serve
//	                       machine)
//	GARAM_CA_KEY_FILE      hex-encoded key-encryption key the certificate
//	                       authorities are sealed under, readable by its
//	                       owner alone (required for serve machine and
//	                       issue-certificate)
//	GARAM_CERTIFICATE_LIFETIME  how long a certificate garam signs stays
//	                       valid (default "24h"; internal/ca bounds it)
//	GARAM_ADAPTER_AGENT    GRN of the agent the adapter carries messages
//	                       for (required for adapter)
//	GARAM_ADAPTER_MACHINE_URL  base URL of the machine listener it claims
//	                       from (required for adapter)
//	GARAM_ADAPTER_GATEWAY_URL  base URL of the agent platform's gateway
//	                       (default "http://127.0.0.1:8080")
//	GARAM_ADAPTER_GATEWAY_AGENT  the identifier that gateway serves its
//	                       agent under, which is not garam's GRN for it
//	                       (required for adapter)
//	GARAM_ADAPTER_TLS_CERT_FILE  the agent's own client certificate, PEM
//	                       (required for adapter)
//	GARAM_ADAPTER_TLS_KEY_FILE   its private key, PEM, readable by its
//	                       owner alone (required for adapter)
//	GARAM_ADAPTER_SERVER_ROOT_FILE  the garam server root the listener is
//	                       verified against, PEM, which is not the
//	                       authority that issued the certificate above
//	                       (required for adapter)
package cli

import (
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/internal/errorx"
)

// NewRootCommand constructs the garam command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "garam",
		Short: "garam is the workflow management core: identity, orgs, authorization",
	}
	root.AddCommand(newServeCommand(), newMigrateCommand(), newProbeCommand(),
		newIssueCertificateCommand(), newAdapterCommand())
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

// certificateLifetime is how long a certificate garam signs stays
// valid. Expiry is the only revocation there is (ADR-0014), so the
// value is a security parameter rather than a tuning knob, and the two
// commands that sign read it the same way. An unset value is zero,
// which takes internal/ca's own default rather than restating it here;
// a value outside the bounds that package states refuses the start.
func certificateLifetime() (time.Duration, error) {
	raw := envOr("GARAM_CERTIFICATE_LIFETIME", "")
	if raw == "" {
		return 0, nil
	}
	lifetime, err := time.ParseDuration(raw)
	if err != nil {
		return 0, errorx.Wrap(err, "GARAM_CERTIFICATE_LIFETIME", errorx.KindInvalidArgument)
	}
	return lifetime, nil
}
