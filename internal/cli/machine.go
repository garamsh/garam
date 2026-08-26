package cli

import (
	"context"
	"crypto/x509"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
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

	databaseURL := envOr("GARAM_DATABASE_URL", "")
	if databaseURL == "" {
		return errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errorx.Wrap(err, "postgres", errorx.KindUnavailable)
	}
	defer pool.Close()

	pair, sealer, serverRoot, err := machineKeyMaterial(
		envOr("GARAM_MACHINE_TLS_CERT_FILE", ""),
		envOr("GARAM_MACHINE_TLS_KEY_FILE", ""),
		envOr("GARAM_CA_KEY_FILE", ""),
		envOr("GARAM_MACHINE_SERVER_ROOT_FILE", ""),
	)
	if err != nil {
		return err
	}

	lifetime, err := certificateLifetime()
	if err != nil {
		return err
	}
	authorities := ca.NewPostgresStore(pool)
	issuer, err := ca.NewService(authorities, sealer, ca.Config{CertificateLifetime: lifetime})
	if err != nil {
		return err
	}
	agents := agent.NewService(agent.NewPostgresStore(pool),
		certificates{issuer: issuer, serverRoot: serverRoot})
	// An operator renews its own certificate on this listener, through
	// the same authority and the same server root its agents are given
	// (ADR-0046).
	operators := operator.NewService(operator.NewPostgresStore(pool),
		operatorCertificates{issuer: issuer, serverRoot: serverRoot}, operator.Config{})
	// The delivery parameters default narrow (ADR-0018), and the
	// defaults are what this process runs on until something asks for
	// another value.
	messages, err := message.NewService(message.NewPostgresStore(pool), message.Config{})
	if err != nil {
		return err
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
		return err
	}

	// Whom an agent may send to is decided by the policy engine, which
	// runs as a process of its own and is reached over gRPC (ADR-0003).
	// Insecure transport: both processes run inside one trust boundary;
	// TLS between them is future work.
	conn, err := grpc.NewClient(
		envOr("GARAM_AUTHZ_ADDR", "localhost:9090"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Carries this request's trace id to the engine, so one id
		// covers both halves of the hop.
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return errorx.Wrap(err, "authz client", errorx.KindInvalidArgument)
	}
	defer func() { _ = conn.Close() }()

	// The generated router matches before the middleware runs, so the
	// log line can name the route rather than the path. The last
	// middleware listed is the outermost, so recovery runs inside the
	// logging: a panic becomes the 500 the line reports rather than an
	// unwind that leaves no line at all. The slice reaches the spec
	// operations alone; recoverPanics below covers the rest of what
	// this server serves.
	handler := machinegen.HandlerWithOptions(
		machineapi.NewServer(agents, operators, messages, consents, watches,
			memberGroups{store: org.NewPostgresStore(pool)},
			machineapi.NewGRPCEvaluator(conn)),
		machinegen.StdHTTPServerOptions{
			Middlewares: []machinegen.MiddlewareFunc{machineapi.RecoverPanics, obs.logMachineRequests},
		})
	srv := machine.NewServer(
		machine.NewService(certificateAuthorities{store: authorities}),
		pair,
		otelhttp.NewHandler(handler, serviceMachine),
	)
	// Assigned rather than passed in, because NewServer puts the
	// identity check outside the handler it is given and that check
	// runs per request too.
	srv.Handler = obs.recoverPanics(srv.Handler)
	srv.Addr = envOr("GARAM_MACHINE_ADDR", ":8443")
	// A refused handshake is the only refusal this listener makes, and
	// net/http reports one here rather than through a handler.
	srv.ErrorLog = slog.NewLogLogger(obs.logger.Handler(), slog.LevelWarn)

	// The listener above requires a client certificate, which a probe
	// does not hold, so the probes answer on a listener of their own —
	// the shape `serve authz` already uses for what a machine reaches
	// without a session. Nothing unauthenticated joins the TLS
	// listener, and losing this one must not take that one down.
	healthAddr := envOr("GARAM_MACHINE_HEALTH_ADDR", ":8444")
	go func() {
		mux := http.NewServeMux()
		// The authorities a presented certificate is verified against,
		// and the agents this surface answers for, are rows; the
		// decision admitting a message comes from the engine.
		health.Register(mux, health.Database(pool), authzChannel{conn: conn})
		if err := http.ListenAndServe(healthAddr, obs.recoverPanics(mux)); err != nil {
			obs.logger.Error("health listener stopped", slog.String("error", err.Error()))
		}
	}()

	obs.logger.Info("serving machine", slog.String("addr", srv.Addr), slog.String("health_addr", healthAddr))
	if err := serveHTTP(ctx, srv, func() error { return srv.ListenAndServeTLS("", "") }); err != nil {
		return errorx.Wrap(err, "machine serve", errorx.KindInternal)
	}
	return nil
}

// machineKeyMaterial loads what the process serves TLS on: the pair the
// listener presents, the key-encryption key every certificate authority
// is sealed under, and the garam server root every machine identity
// pins garam by (ADR-0040). Each arrives as a file, because key
// material never travels in the environment (ADR-0015).
//
// The pair holds the two paths rather than the certificate they held at
// startup, so replacing the files replaces what the next connection is
// served. The read here is what refuses a start the listener could
// never have served a handshake from: the listener's own key is key
// material and is read under the same owner-only rule as the
// key-encryption key, and a group-readable one refuses the start rather
// than terminating every machine identity's connection with it.
//
// This surface signs certificates, so the key it opens an authority
// with is what the process runs on: a missing, group-readable,
// malformed or wrong-length one refuses the start rather than the
// first certificate asked for (ADR-0016).
//
// The server root is a certificate, so it is public and is read
// plainly. It is read once: a root outlives every certificate it signs
// by years, and replacing one is a restart.
func machineKeyMaterial(certFile, keyFile, caKeyFile, serverRootFile string) (*keyfile.Pair, ca.Sealer, []byte, error) {
	if certFile == "" || keyFile == "" {
		return nil, nil, nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_MACHINE_TLS_CERT_FILE and GARAM_MACHINE_TLS_KEY_FILE are required")
	}
	if serverRootFile == "" {
		return nil, nil, nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_MACHINE_SERVER_ROOT_FILE is required: an agent pins the root that signed this listener (ADR-0040)")
	}
	sealer, err := ca.NewSealerFromKeyFile(caKeyFile)
	if err != nil {
		return nil, nil, nil, errorx.Wrap(err, "GARAM_CA_KEY_FILE")
	}
	pair, err := keyfile.NewPair(certFile, keyFile)
	if err != nil {
		return nil, nil, nil, errorx.Wrap(err, "machine listener certificate")
	}
	serverRoot, err := os.ReadFile(serverRootFile)
	if err != nil {
		return nil, nil, nil, errorx.Wrap(err, "GARAM_MACHINE_SERVER_ROOT_FILE", errorx.KindInvalidArgument)
	}
	// A root an agent cannot verify garam against reaches every agent
	// and works for none, so it is parsed here rather than handed on as
	// whatever the file held — through the pool the adapter reads the
	// same file into, so the two agree on what counts as one.
	if !x509.NewCertPool().AppendCertsFromPEM(serverRoot) {
		return nil, nil, nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_MACHINE_SERVER_ROOT_FILE: no certificate in "+serverRootFile)
	}
	return pair, sealer, serverRoot, nil
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
	return authority.Certificate(), nil
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

func (c operatorCertificates) Issue(ctx context.Context, subject grn.GRN) (*operator.Certificate, error) {
	issued, err := c.issuer.Issue(ctx, subject)
	if err != nil {
		return nil, err
	}
	return &operator.Certificate{
		Serial:         operator.Serial(issued.Serial()),
		CertificatePEM: issued.CertificatePEM(),
		PrivateKeyPEM:  issued.PrivateKeyPEM(),
		IssuerPEM:      issued.IssuerPEM(),
		ServerRootPEM:  c.serverRoot,
		NotAfter:       issued.NotAfter(),
	}, nil
}
