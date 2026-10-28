package cli

import (
	"context"
	"crypto/x509"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	auditpg "github.com/garamsh/garam/internal/audit/postgres"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/health"
	"github.com/garamsh/garam/internal/keyfile"
	"github.com/garamsh/garam/internal/machine"
	machinepg "github.com/garamsh/garam/internal/machine/postgres"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/message"
	messagepg "github.com/garamsh/garam/internal/message/postgres"
	"github.com/garamsh/garam/internal/message/transit"
	"github.com/garamsh/garam/internal/operator"
	operatorpg "github.com/garamsh/garam/internal/operator/postgres"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/watch"
	watchpg "github.com/garamsh/garam/internal/watch/postgres"
)

// newServeMachineCommand constructs `garam serve machine`: the
// listener an agent, an operator or a supervisor authenticates on with
// a token (ADR-0100, ADR-0118). It terminates TLS itself and takes
// neither the browser API's proxy nor its cookie, and it runs as a
// process of its own so the browser-facing one never holds the key
// the message signing keys are sealed under (ADR-0017). Like the
// browser-facing one it reaches the policy engine over gRPC, which is
// what decides the messages it accepts.
func newServeMachineCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "machine",
		Short: "Serve the listener machine identities authenticate on",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serveMachine(cmd)
		},
	}
}

func serveMachine(cmd *cobra.Command) error {
	ctx, stop := serveSignalContext(cmd.Context())
	defer stop()

	obs, err := newObservability(serviceMachine)
	if err != nil {
		return err
	}

	pool, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	keys, err := machineKeyMaterial(
		envOr(envMachineTLSCertFile, ""),
		envOr(envMachineTLSKeyFile, ""),
		envOr(envCAKeyFile, ""),
		envOr(envMachineServerRootFile, ""),
	)
	if err != nil {
		return err
	}

	tokenLifetime, err := agentTokenLifetime()
	if err != nil {
		return err
	}

	issuer, err := message.ParseIssuer(envOr(envMachineIssuer, ""))
	if err != nil {
		return errorx.Wrap(err, string(envMachineIssuer))
	}
	signingCustodian, err := transitCustodian()
	if err != nil {
		return err
	}
	tokenURL, err := operator.ParseTokenEndpoint(envOr(envMachineTokenURL, ""))
	if err != nil {
		return errorx.Wrap(err, string(envMachineTokenURL))
	}

	// Whom an agent may send to is decided by the policy engine, which
	// runs as a process of its own and is reached over gRPC (ADR-0003).
	conn, err := dialAuthz()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	srv, err := NewMachineServer(pool, MachineConfig{
		Sealer:           keys.sealer,
		SigningCustodian: signingCustodian,
		Issuer:           issuer,
		Certificate:      keys.listener,
		ServerRoot:       keys.serverRoot,
		// The delivery parameters default narrow (ADR-0018), and the
		// defaults are what this process runs on until something asks
		// for another value.
		Delivery:           message.Config{},
		AgentTokenLifetime: tokenLifetime,
		TokenURL:           tokenURL,
		Engine:             conn,
		Logger:             obs.logger,
		Metrics:            obs.registry,
	})
	if err != nil {
		return err
	}
	srv.Addr = envOr(envMachineAddr, ":8443")

	// A probe holds no certificate and reaches no route on the TLS
	// listener, and losing this one must not take that one down.
	healthAddr := envOr(envMachineHealthAddr, ":8444")
	go func() {
		// The tokens and the agents this surface answers for are rows;
		// the decision admitting a message comes from the engine.
		if err := http.ListenAndServe(healthAddr,
			obs.machineProbeHandler(health.Database(pool), authzChannel{conn: conn})); err != nil {
			obs.logger.Error("health listener stopped", slog.String("error", err.Error()))
		}
	}()

	obs.logger.Info("serving machine", slog.String("addr", srv.Addr), slog.String("health_addr", healthAddr))
	if err := obs.serveHTTP(ctx, shutdownGrace, srv, func() error { return srv.ListenAndServeTLS("", "") }); err != nil {
		return errorx.Wrap(err, "machine serve", errorx.KindInternal)
	}
	return nil
}

// MachineConfig is what `garam serve machine` builds its server from
// beside the database. The command reads each part from the process's
// configuration; a test supplies its own.
type MachineConfig struct {
	// Sealer opens the sealed private keys messages are signed with
	// (ADR-0101) where those are sealed into their rows.
	Sealer ca.Sealer
	// SigningCustodian holds every message signing key created from now
	// on, and the keys sealed under Sealer still sign while current
	// (ADR-0104). Nil seals new keys under Sealer too, as before.
	SigningCustodian message.Custodian
	// Issuer is the value every message signature names as its issuer:
	// this listener's external https origin (ADR-0101).
	Issuer message.Issuer
	// Certificate is what the listener presents (ADR-0040).
	Certificate machine.ServerCertificate
	// ServerRoot is the garam root answered beside every agent token and
	// supervisor credential issued here, for its holder to pin garam by
	// (ADR-0040).
	ServerRoot []byte
	// Delivery governs the message queue (ADR-0018).
	Delivery message.Config
	// Engine is the policy engine, which decides the messages the
	// listener accepts and, from current state, the operations a hosted
	// operator reads back (ADR-0078).
	Engine grpc.ClientConnInterface
	// Logger takes one line per served request and every recovered
	// panic.
	Logger *slog.Logger
	// AgentTokenLifetime is how long an agent token is accepted
	// (ADR-0100); zero is the default, and agent.NewService bounds it.
	AgentTokenLifetime time.Duration
	// TokenURL is the token endpoint URL an operator's assertion names
	// as its audience, which operator.ParseTokenEndpoint read
	// (ADR-0102). Empty serves no assertion exchange.
	TokenURL string
	// Metrics is where the count of requests by how they authenticated
	// is registered (ADR-0100). Nil registers it nowhere.
	Metrics prometheus.Registerer
	// Now supplies the time a token and a grant are measured against on
	// every decision, so a test can move it across an expiry on an open
	// connection. Zero takes time.Now.
	Now func() time.Time
}

// NewMachineServer assembles the server `garam serve machine` runs:
// the generated router with its middleware and binding-error body,
// behind the listener machineServer builds. The caller sets the
// address and serves it.
func NewMachineServer(pool *pgxpool.Pool, cfg MachineConfig) (*http.Server, error) {
	obs := &observability{logger: cfg.Logger}
	requests, err := newMachineRequestCounter(cfg.Metrics)
	if err != nil {
		return nil, err
	}
	exchanges, err := newTokenExchangeCounter(cfg.Metrics)
	if err != nil {
		return nil, err
	}

	// An operator exchanges its key for tokens here (ADR-0102).
	operatorStore := operatorpg.New(pool)
	operators := operator.NewService(operatorStore, operator.Config{TokenEndpoint: cfg.TokenURL})
	// An agent token's start is backdated by tokenBackdate, and its
	// audience is an operator the organization registered (ADR-0100).
	agentStore := agentpg.New(pool)
	agents, err := agent.NewService(agentStore, agentStore, registeredOperators{operators: operatorStore},
		agent.Config{TokenLifetime: cfg.AgentTokenLifetime, Backdate: tokenBackdate, ServerRootPEM: cfg.ServerRoot, Now: cfg.Now})
	if err != nil {
		return nil, err
	}
	messages, err := message.NewService(messagepg.New(pool), cfg.Delivery)
	if err != nil {
		return nil, err
	}
	// Each hand-out is signed under a key the configured custodian
	// holds, valid for the lease ceiling the queue holds a claim to
	// (ADR-0101). With none configured the key is sealed under the
	// key-encryption key; with one, a key sealed before it still signs
	// until its replacement begins (ADR-0104).
	sealed := message.NewFileCustodian(cfg.Sealer)
	var keyring message.Keyring
	if cfg.SigningCustodian == nil {
		keyring, err = message.NewKeyring(messagepg.New(pool), cfg.Issuer, cfg.Delivery, sealed)
	} else {
		keyring, err = message.NewKeyring(messagepg.New(pool), cfg.Issuer, cfg.Delivery, cfg.SigningCustodian, sealed)
	}
	if err != nil {
		return nil, err
	}
	// The consent a cross-organization post needs is the message
	// domain's relation (ADR-0035); this process reads it and `serve
	// api` is where an organization grants and withdraws its half.
	consents := messagepg.New(pool)

	// Whether anyone is watching is read from the rows `serve api` writes
	// while it holds a stream, and a forwarded event is announced for
	// that process to deliver: garam cannot dial an adapter, so the
	// count travels on the poll this process answers, and no stream is
	// held here (ADR-0022).
	watches, err := watch.NewService(watchpg.New(pool), watchpg.NewRelay(pool), watch.Config{})
	if err != nil {
		return nil, err
	}

	// A lifecycle decision reads the caller's token dates and record
	// again, on the same clock.
	recheck := machine.NewRecheck(identityRegistrations{operators: operatorStore, supervisors: agentpg.New(pool)}, cfg.Now)
	// The listener notes how each request authenticated, and the issuing
	// routes what they issued (ADR-0109).
	evidence := machine.NewEvidence(machinepg.New(pool), evidenceWindow, cfg.Now)

	// The generated router matches before the middleware runs, so the
	// log line can name the route rather than the path. The last
	// middleware listed is the outermost, so recovery runs inside the
	// logging: a panic becomes the 500 the line reports rather than an
	// unwind that leaves no line at all. Confining a supervisor to its
	// routes is innermost, beside the handlers it guards. The slice
	// reaches the spec
	// operations alone; machineServer covers the rest of what this
	// server serves.
	authority := NewAuthorityService(pool, cfg.Engine)
	handler := machinegen.HandlerWithOptions(
		machineapi.NewServer(agents, operators, messages, consents, watches,
			memberGroups{store: orgpg.New(pool)}, machineapi.NewGRPCEvaluator(cfg.Engine),
			authority, recheck, newProofService(pool, cfg.Engine, cfg.Now),
			newExecutionService(pool, cfg.Engine, authority, cfg.Now),
			newSupervisorService(pool, cfg.Engine, cfg.ServerRoot, cfg.Now),
			newAdmissionService(pool, authority, cfg.Now), keyring).
			WithCredentialAudit(auditpg.NewCredentialStore(pool)),
		machinegen.StdHTTPServerOptions{
			Middlewares: []machinegen.MiddlewareFunc{machineapi.ConfineSupervisors, machineapi.RecoverPanics,
				obs.logMachineRequests, countMachineRequests(requests), countTokenExchanges(exchanges), recordIssuances(evidence)},
			// A parameter the router cannot bind is refused before any
			// handler runs, so this is the only place the surface's own
			// error body reaches that refusal.
			ErrorHandlerFunc: machineapi.BindingError,
		})
	return obs.machineServer(identityTokens{operators: operatorStore, agents: agentStore, supervisors: agentpg.New(pool)}, evidence, cfg.Certificate, handler), nil
}

// machineServer is the listener `serve machine` serves: server TLS
// (ADR-0040), the token check machine.NewServer puts around next, the
// bound on what one request may weigh, and recoverPanics outermost so a
// panic in any of them is answered rather than dropping the connection.
//
// The wrapping is assigned rather than passed in, because NewServer
// puts that check outside the handler it is given and the check runs
// per request too.
func (o *observability) machineServer(tokens machine.Tokens, evidence *machine.Evidence, certificate machine.ServerCertificate, next http.Handler) *http.Server {
	srv := machine.NewServer(tokens, evidence, certificate,
		otelhttp.NewHandler(next, serviceMachine))
	srv.Handler = o.recoverPanics(http.MaxBytesHandler(srv.Handler, maxRequestBody))
	// A handshake that fails outright — a client that speaks no TLS
	// 1.3, or a certificate the listener cannot present — is reported
	// here rather than through a handler.
	srv.ErrorLog = slog.NewLogLogger(o.logger.Handler(), slog.LevelWarn)
	srv.ReadTimeout = requestReadTimeout
	return srv
}

// machineProbeHandler is what `serve machine` answers on the port
// beside its TLS one, because the listener above answers a probe on no
// route: what it serves is the machine specification's operations. It
// carries /metrics beside the probes, as `serve authz`'s does: the
// count of requests by how they authenticated (ADR-0100). recoverPanics
// is outermost, as on every listener a serve command runs.
func (o *observability) machineProbeHandler(deps ...health.Dependency) http.Handler {
	mux := http.NewServeMux()
	health.Register(mux, deps...)
	mux.Handle("GET /metrics", o.metricsHandler())
	return o.recoverPanics(mux)
}

// machineKeys is what `serve machine` serves TLS on. Each arrives as a
// file (ADR-0015), and every field is read before the listener opens.
type machineKeys struct {
	// listener is the certificate pair the machine listener presents,
	// held as its two paths so a replaced file is served on the next
	// connection (ADR-0040).
	listener *keyfile.Pair
	// sealer opens the key-encryption key the message signing keys are
	// sealed under (ADR-0104).
	sealer ca.Sealer
	// serverRoot is the garam root every machine identity pins garam
	// by (ADR-0040).
	serverRoot []byte
}

// machineKeyMaterial reads the key material into a machineKeys,
// refusing a start the listener could never have served a handshake
// from.
func machineKeyMaterial(certFile, keyFile, caKeyFile, serverRootFile string) (machineKeys, error) {
	if certFile == "" || keyFile == "" {
		return machineKeys{}, errorx.New(errorx.KindInvalidArgument,
			"GARAM_MACHINE_TLS_CERT_FILE and GARAM_MACHINE_TLS_KEY_FILE are required")
	}
	if serverRootFile == "" {
		return machineKeys{}, errorx.New(errorx.KindInvalidArgument,
			"GARAM_MACHINE_SERVER_ROOT_FILE is required: an agent pins the root that signed this listener (ADR-0040)")
	}
	sealer, err := ca.NewSealerFromKeyFile(caKeyFile)
	if err != nil {
		return machineKeys{}, errorx.Wrap(err, string(envCAKeyFile))
	}
	pair, err := keyfile.NewPair(certFile, keyFile)
	if err != nil {
		return machineKeys{}, errorx.Wrap(err, "machine listener certificate")
	}
	serverRoot, err := os.ReadFile(serverRootFile)
	if err != nil {
		return machineKeys{}, errorx.Wrap(err, string(envMachineServerRootFile), errorx.KindInvalidArgument)
	}
	// A root an agent cannot verify garam against reaches every agent
	// and works for none, so it is parsed here rather than handed on as
	// whatever the file held.
	if !x509.NewCertPool().AppendCertsFromPEM(serverRoot) {
		return machineKeys{}, errorx.New(errorx.KindInvalidArgument,
			"GARAM_MACHINE_SERVER_ROOT_FILE: no certificate in "+serverRootFile)
	}
	return machineKeys{listener: pair, sealer: sealer, serverRoot: serverRoot}, nil
}

// transitCustodian reads the Transit provider's configuration, and
// answers nil where the deployment sets none of it (ADR-0104). Setting
// part of it is refused, so a deployment never runs believing its keys
// are in Transit while garam seals them under the key-encryption key.
func transitCustodian() (message.Custodian, error) {
	settings := transit.Settings{
		Address:   envOr(envTransitAddr, ""),
		CAFile:    envOr(envTransitCAFile, ""),
		TokenFile: envOr(envTransitTokenFile, ""),
		Key:       envOr(envTransitSigningKey, ""),
		Mount:     envOr(envTransitMount, ""),
	}
	if settings.Address == "" {
		if settings != (transit.Settings{}) {
			return nil, errorx.New(errorx.KindInvalidArgument,
				"GARAM_TRANSIT_* is set without GARAM_TRANSIT_ADDR, which turns the Transit provider on")
		}
		return nil, nil
	}
	if settings.CAFile == "" || settings.TokenFile == "" || settings.Key == "" {
		return nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_TRANSIT_CA_FILE, GARAM_TRANSIT_TOKEN_FILE and GARAM_TRANSIT_SIGNING_KEY are required beside GARAM_TRANSIT_ADDR")
	}
	custodian, err := transit.New(settings)
	if err != nil {
		return nil, errorx.Wrap(err, "GARAM_TRANSIT_*")
	}
	return custodian, nil
}

// memberGroups reads the groups a machine identity belongs to
// (machineapi.Groups), which decide whether a policy attached to a
// group reaches what it sends. Organizations are root-owned, so the
// organization segment of the identity's own GRN names the one whose
// groups can hold it (ADR-0001, ADR-0014).
type memberGroups struct{ store org.Store }

func (m memberGroups) List(ctx context.Context, subject grn.GRN) ([]grn.GRN, error) {
	return m.store.ListGroupsByMember(ctx, grn.Org(subject.Organization), subject)
}

// tokenBackdate is how far garam backdates an agent token's issue
// time, so a token is usable at once on a peer or a database whose
// clock trails this process's. It moves the start alone.
const tokenBackdate = time.Minute
