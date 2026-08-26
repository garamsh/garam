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
// mints an operator's first certificate — which is why the command is
// a sibling of `serve` rather than a subcommand under it. That is as
// far as it reaches: `serve machine` mints agent identities and issues
// certificates on the listener it serves
// (docs/architecture/domains.md §internal/ca).
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
//	GARAM_COOKIE_SECURE    mark cookies Secure (default true); accepts
//	                       what strconv.ParseBool does and refuses the
//	                       rest rather than falling back
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

// envName is the name of an environment variable garam reads. Every
// name the module reads is declared below and reaches os.Getenv from
// there, so the set is enumerable from these declarations rather than
// from the spelling of the calls that read them: `make chart-render`
// reads this block, and a wrapper around envOr cannot shrink what that
// check covers.
type envName string

// The environment garam reads. What a deployment must set is described
// in deploy/charts/garam/README.md; this block is the set that README
// is checked against.
const (
	envDatabaseURL           envName = "GARAM_DATABASE_URL"
	envMigrationsDir         envName = "GARAM_MIGRATIONS_DIR"
	envHTTPAddr              envName = "GARAM_HTTP_ADDR"
	envGRPCAddr              envName = "GARAM_GRPC_ADDR"
	envMetricsAddr           envName = "GARAM_METRICS_ADDR"
	envAuthzAddr             envName = "GARAM_AUTHZ_ADDR"
	envSessionTTL            envName = "GARAM_SESSION_TTL"
	envCookieSecure          envName = "GARAM_COOKIE_SECURE"
	envGoogleClientID        envName = "GOOGLE_CLIENT_ID"
	envGoogleClientSecret    envName = "GOOGLE_CLIENT_SECRET"
	envGoogleRedirectURL     envName = "GOOGLE_REDIRECT_URL"
	envCAKeyFile             envName = "GARAM_CA_KEY_FILE"
	envCertificateLifetime   envName = "GARAM_CERTIFICATE_LIFETIME"
	envMachineAddr           envName = "GARAM_MACHINE_ADDR"
	envMachineHealthAddr     envName = "GARAM_MACHINE_HEALTH_ADDR"
	envMachineTLSCertFile    envName = "GARAM_MACHINE_TLS_CERT_FILE"
	envMachineTLSKeyFile     envName = "GARAM_MACHINE_TLS_KEY_FILE"
	envMachineServerRootFile envName = "GARAM_MACHINE_SERVER_ROOT_FILE"
	envAdapterAgent          envName = "GARAM_ADAPTER_AGENT"
	envAdapterMachineURL     envName = "GARAM_ADAPTER_MACHINE_URL"
	envAdapterGatewayURL     envName = "GARAM_ADAPTER_GATEWAY_URL"
	envAdapterGatewayAgent   envName = "GARAM_ADAPTER_GATEWAY_AGENT"
	envAdapterTLSCertFile    envName = "GARAM_ADAPTER_TLS_CERT_FILE"
	envAdapterTLSKeyFile     envName = "GARAM_ADAPTER_TLS_KEY_FILE"
	envAdapterServerRootFile envName = "GARAM_ADAPTER_SERVER_ROOT_FILE"
)

// envOr reads a declared name, or the fallback when it is unset or
// empty. It holds the module's only os.Getenv call, which is what
// makes the block above the whole of what the source reads.
func envOr(key envName, fallback string) string {
	if v := os.Getenv(string(key)); v != "" {
		return v
	}
	return fallback
}

// requestReadTimeout bounds how long a request has to arrive, not how
// long a handler may run: net/http applies it to reading, so a stalled
// client is disconnected at the bound while a watch stream and a
// message wait — both far longer than this — are untouched. Generous
// against the largest body either listener admits (65536 characters,
// ADR-0034) and against the machine listener's handshake, which is read
// on the same connection.
const requestReadTimeout = 30 * time.Second

// certificateLifetime is how long a certificate garam signs stays
// valid. Expiry is the only revocation there is (ADR-0014), so the
// value is a security parameter rather than a tuning knob, and the two
// commands that sign read it the same way. An unset value is zero,
// which takes internal/ca's own default rather than restating it here;
// a value outside the bounds that package states refuses the start.
func certificateLifetime() (time.Duration, error) {
	raw := envOr(envCertificateLifetime, "")
	if raw == "" {
		return 0, nil
	}
	lifetime, err := time.ParseDuration(raw)
	if err != nil {
		return 0, errorx.Wrap(err, string(envCertificateLifetime), errorx.KindInvalidArgument)
	}
	return lifetime, nil
}
