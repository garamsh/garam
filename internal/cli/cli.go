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
// (docs/architecture/certificate-authorities.md).
//
// Every serve command answers the two probes internal/health mounts —
// liveness and readiness — on a listener that asks for no credential:
// the api's own HTTP port, the authz engine's metrics port, and a port
// of its own for `serve machine`, whose listener serves no probe route.
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
//	GARAM_AUTHZ_ADDR       authz engine address for serve api and serve
//	                       machine (default "localhost:9090")
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
//	                       itself serves no probe route
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
		// cmd/garam prints what Execute returns, so cobra printing it
		// as well is the same line twice. The caller's is the one kept,
		// because it goes through the logger the process already writes
		// its other lines with.
		SilenceErrors: true,
	}
	root.AddCommand(newServeCommand(), newMigrateCommand(), newProbeCommand(),
		newIssueCertificateCommand(), newAdapterCommand(), newStandaloneCommand())
	silenceUsageOnRun(root)
	return root
}

// silenceUsageOnRun stops a command that fails while running from
// printing its usage block, and leaves the block on one that fails
// before it runs. A command in a crash loop writes its usage text to
// the container log on every restart, and "how is this invoked" answers
// a bad flag rather than an unreadable GARAM_COOKIE_SECURE.
//
// cobra runs ValidateRequiredFlags after every hook, so the line goes
// inside RunE: from a hook it would silence `issue-certificate` without
// --out-dir, which is a usage error and keeps its block.
func silenceUsageOnRun(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		silenceUsageOnRun(child)
	}
	run := cmd.RunE
	if run == nil {
		return
	}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		c.SilenceUsage = true
		return run(c, args)
	}
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
	envAdapterTLSCertFile    envName = "GARAM_ADAPTER_TLS_CERT_FILE"
	envAdapterTLSKeyFile     envName = "GARAM_ADAPTER_TLS_KEY_FILE"
	envAdapterServerRootFile envName = "GARAM_ADAPTER_SERVER_ROOT_FILE"
	envAdapterControlURL     envName = "GARAM_ADAPTER_CONTROL_URL"
	envAdapterControlRoot    envName = "GARAM_ADAPTER_CONTROL_ROOT_FILE"
	envAdapterPlacementToken envName = "GARAM_ADAPTER_PLACEMENT_TOKEN_FILE"
	envAdapterOutboxDir      envName = "GARAM_ADAPTER_OUTBOX_DIR"
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
// long a handler may run, so a watch stream and a message wait are
// untouched and what it disconnects is a client that has stopped
// sending. Under HTTP/1.1 it is the connection's read deadline; under
// HTTP/2 a per-stream timer of the same length replaces that deadline
// once the headers are read, so a body is bounded on both (go1.26.7
// net/http). How long a body may take is not how much of it there may
// be, which is maxRequestBody's.
const requestReadTimeout = 30 * time.Second

// maxRequestBody bounds a request body before a decoder allocates for
// it. Both listeners read a body whole, so a schema's own maxLength
// bounds nothing until the string is already in memory, and thirty
// seconds of a fast link carries more than either process has. The
// largest body a specification admits is a message submission's 65536
// characters (ADR-0034), which weighs 768 KiB where a caller spends
// twelve bytes on each as an escaped surrogate pair; this is the next
// power of two above that.
const maxRequestBody = 1 << 20

// certificateLifetime is how long a certificate garam signs stays
// valid. It is what bounds an agent's, which nothing withdraws
// (ADR-0014), so the value is a security parameter rather than a
// tuning knob, and the two commands that sign read it the same way. An
// unset value is zero, which takes internal/ca's own default rather
// than restating it here;
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
