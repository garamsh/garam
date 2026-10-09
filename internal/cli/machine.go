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
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/health"
	"github.com/garamsh/garam/internal/keyfile"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/watch"
)

// newServeMachineCommand constructs `garam serve machine`: the
// listener an agent or an operator authenticates on with a client
// certificate (ADR-0014, ADR-0015). It terminates TLS itself and takes
// neither the browser API's proxy nor its cookie, and it runs as a
// process of its own so the browser-facing one never holds the key the
// certificate authorities are sealed under (ADR-0017). Like the
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

	databaseURL := envOr(envDatabaseURL, "")
	if databaseURL == "" {
		return errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errorx.Wrap(err, "postgres", errorx.KindUnavailable)
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

	lifetime, err := certificateLifetime()
	if err != nil {
		return err
	}

	// Whom an agent may send to is decided by the policy engine, which
	// runs as a process of its own and is reached over gRPC (ADR-0003).
	// Insecure transport: both processes run inside one trust boundary;
	// TLS between them is future work.
	conn, err := grpc.NewClient(
		envOr(envAuthzAddr, "localhost:9090"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Carries this request's trace id to the engine, so one id
		// covers both halves of the hop.
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return errorx.Wrap(err, "authz client", errorx.KindInvalidArgument)
	}
	defer func() { _ = conn.Close() }()

	srv, err := NewMachineServer(pool, MachineConfig{
		Sealer:      keys.sealer,
		Certificate: keys.listener,
		ServerRoot:  keys.serverRoot,
		Authority:   ca.Config{CertificateLifetime: lifetime},
		// The delivery parameters default narrow (ADR-0018), and the
		// defaults are what this process runs on until something asks
		// for another value.
		Delivery: message.Config{},
		Engine:   conn,
		Logger:   obs.logger,
		Metrics:  obs.registry,
	})
	if err != nil {
		return err
	}
	srv.Addr = envOr(envMachineAddr, ":8443")

	// A probe holds no certificate and reaches no route on the TLS
	// listener, and losing this one must not take that one down.
	healthAddr := envOr(envMachineHealthAddr, ":8444")
	go func() {
		// The authorities a presented certificate is verified against,
		// and the agents this surface answers for, are rows; the
		// decision admitting a message comes from the engine.
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
	// Sealer opens the sealed private keys of the certificate
	// authorities (ADR-0016).
	Sealer ca.Sealer
	// Certificate is what the listener presents (ADR-0040).
	Certificate machine.ServerCertificate
	// ServerRoot is the garam root handed out beside every certificate
	// issued here, for its holder to pin garam by (ADR-0040).
	ServerRoot []byte
	// Authority governs the certificates issued here.
	Authority ca.Config
	// Delivery governs the message queue (ADR-0018).
	Delivery message.Config
	// Engine is the policy engine, which decides the messages the
	// listener accepts and, from current state, the operations a hosted
	// operator reads back (ADR-0078).
	Engine grpc.ClientConnInterface
	// Logger takes one line per served request and every recovered
	// panic.
	Logger *slog.Logger
	// Metrics is where the count of requests by how they authenticated
	// is registered (ADR-0100). Nil registers it nowhere.
	Metrics prometheus.Registerer
	// Now supplies the time a certificate and a grant are measured
	// against on every decision, so a test can move it across an
	// expiry on an open connection. Zero takes time.Now.
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

	authorities := capg.New(pool)
	issuer, err := ca.NewService(authorities, cfg.Sealer, cfg.Authority)
	if err != nil {
		return nil, err
	}
	agents := agent.NewService(agent.NewPostgresStore(pool),
		certificates{issuer: issuer, serverRoot: cfg.ServerRoot})
	// An operator obtains its first certificate on this listener by
	// enrolling (ADR-0064) and renews it here afterwards (ADR-0046),
	// through the same authority and the same server root its agents
	// are given, and it exchanges its credential for tokens here
	// (ADR-0100). One store answers all three: what garam last issued
	// an operator, the token an enrollment spends, and the tokens a
	// credential is exchanged for.
	operatorStore := operator.NewPostgresStore(pool)
	operators := operator.NewService(operatorStore, operatorStore, operatorStore,
		operatorCertificates{issuer: issuer, serverRoot: cfg.ServerRoot}, operator.Config{})
	messages, err := message.NewService(message.NewPostgresStore(pool), cfg.Delivery)
	if err != nil {
		return nil, err
	}
	// The consent a cross-organization post needs is the message
	// domain's relation (ADR-0035); this process reads it and `serve
	// api` is where an organization grants and withdraws its half.
	consents := message.NewPostgresStore(pool)

	// Whether anyone is watching is read from the rows `serve api` writes
	// while it holds a stream, and a forwarded event is announced for
	// that process to deliver: garam cannot dial an adapter, so the
	// count travels on the poll this process answers, and no stream is
	// held here (ADR-0022).
	watches, err := watch.NewService(watch.NewPostgresStore(pool), watch.NewPostgresRelay(pool), watch.Config{})
	if err != nil {
		return nil, err
	}

	// One service authenticates a connection when it is made and
	// verifies certificates again on a decision, so both read the same
	// trust roots, records and clock.
	identities := machine.NewService(certificateAuthorities{store: authorities},
		identityRegistrations{operators: operatorStore, supervisors: agentpg.New(pool)}, machine.Config{Now: cfg.Now})

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
			memberGroups{store: org.NewPostgresStore(pool)}, machineapi.NewGRPCEvaluator(cfg.Engine),
			authority, identities, newProofService(pool, cfg.Engine, cfg.Now),
			newExecutionService(pool, cfg.Engine, authority, identities, issuer, cfg.ServerRoot, cfg.Now),
			newSupervisorService(pool, cfg.Engine, issuer, cfg.ServerRoot, cfg.Now)),
		machinegen.StdHTTPServerOptions{
			Middlewares: []machinegen.MiddlewareFunc{machineapi.ConfineTokens, machineapi.ConfineSupervisors, machineapi.RecoverPanics,
				obs.logMachineRequests, countMachineRequests(requests)},
			// A parameter the router cannot bind is refused before any
			// handler runs, so this is the only place the surface's own
			// error body reaches that refusal.
			ErrorHandlerFunc: machineapi.BindingError,
		})
	return obs.machineServer(identities, identityTokens{operators: operatorStore}, cfg.Certificate, handler), nil
}

// machineServer is the listener `serve machine` serves: TLS a client
// certificate authenticates on (ADR-0014, ADR-0040), the identity check
// machine.NewServer puts around next, the bound on what one request may
// weigh, and recoverPanics outermost so a panic in any of them is
// answered rather than dropping the connection.
//
// The wrapping is assigned rather than passed in, because NewServer
// puts that check outside the handler it is given and the check runs
// per request too.
func (o *observability) machineServer(identities machine.Service, tokens machine.Tokens, certificate machine.ServerCertificate, next http.Handler) *http.Server {
	srv := machine.NewServer(identities, tokens, certificate,
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
// carries /metrics beside the probes, as `serve authz`'s does, because
// the count of requests by how they authenticated is what decides when
// the certificate path may go (ADR-0100). recoverPanics is outermost,
// as on every listener a serve command runs.
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
	// sealer opens the key-encryption key every certificate authority
	// is sealed under (ADR-0016).
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

// certificateAuthorities reads the authority a presented certificate
// has to chain to (machine.TrustRoot). Organizations are root-owned,
// so the organization segment of the identity the certificate names is
// what names the only CA that may sign for it (ADR-0001, ADR-0014).
type certificateAuthorities struct{ store ca.Store }

func (c certificateAuthorities) Get(ctx context.Context, subject grn.GRN) ([]byte, error) {
	authority, err := c.store.Get(ctx,
		grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeOrg, subject.Organization))
	if err != nil {
		return nil, err
	}
	return authority.Certificate, nil
}

// memberGroups reads the groups a machine identity belongs to
// (machineapi.Groups), which decide whether a policy attached to a
// group reaches what it sends. Organizations are root-owned, so the
// organization segment of the identity's own GRN names the one whose
// groups can hold it (ADR-0001, ADR-0014).
type memberGroups struct{ store org.Store }

func (m memberGroups) List(ctx context.Context, subject grn.GRN) ([]grn.GRN, error) {
	return m.store.ListGroupsByMember(ctx,
		grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeOrg, subject.Organization), subject)
}

// certificates is the certificate authority as the agent domain signs
// through it (agent.Issuer), and the garam server root travelling
// beside what it signs. The two cooperate through grn.GRN alone, so the
// composition root is where what one issues becomes what the other
// hands back — and it is the only place that holds both the authority
// that signs an agent and the root that agent verifies garam against
// (ADR-0040).
type certificates struct {
	issuer     ca.Service
	serverRoot []byte
}

func (c certificates) Issue(ctx context.Context, subject grn.GRN) (*agent.Certificate, error) {
	issued, err := c.issuer.Issue(ctx, subject)
	if err != nil {
		return nil, err
	}
	return &agent.Certificate{
		CertificatePEM: issued.CertificatePEM(),
		PrivateKeyPEM:  issued.PrivateKeyPEM(),
		IssuerPEM:      issued.IssuerPEM(),
		ServerRootPEM:  c.serverRoot,
		NotAfter:       issued.NotAfter(),
	}, nil
}

// operatorCertificates is that same authority in the shape the operator
// domain signs through (operator.Issuer). It carries the serial as
// well: what an operator presents on the renewal that replaces a
// certificate is which certificate it is, and only the issuer knows
// that (ADR-0046).
type operatorCertificates struct {
	issuer     ca.Service
	serverRoot []byte
}

func (c operatorCertificates) Issue(ctx context.Context, subject grn.GRN) (*operator.IssuedCertificate, error) {
	issued, err := c.issuer.Issue(ctx, subject)
	if err != nil {
		return nil, err
	}
	return &operator.IssuedCertificate{
		Certificate:   c.certificate(&issued.Certificate),
		PrivateKeyPEM: issued.PrivateKeyPEM(),
	}, nil
}

func (c operatorCertificates) Sign(ctx context.Context, subject grn.GRN, request operator.CertificateRequest) (*operator.Certificate, error) {
	signed, err := c.issuer.Sign(ctx, subject, request)
	if err != nil {
		return nil, err
	}
	certificate := c.certificate(signed)
	return &certificate, nil
}

// certificate renders what the authority signed in the shape the
// operator domain hands back, with the server root this process was
// given beside it (ADR-0040).
func (c operatorCertificates) certificate(signed *ca.Certificate) operator.Certificate {
	return operator.Certificate{
		Subject:        signed.Subject(),
		Serial:         operator.Serial(signed.Serial()),
		CertificatePEM: signed.CertificatePEM(),
		IssuerPEM:      signed.IssuerPEM(),
		ServerRootPEM:  c.serverRoot,
		NotAfter:       signed.NotAfter(),
	}
}
