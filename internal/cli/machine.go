package cli

import (
	"context"
	"crypto/tls"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/org"
)

// newServeMachineCommand constructs `garam serve machine`: the
// listener an agent or an operator authenticates on with a client
// certificate (ADR-0014, ADR-0015). It terminates TLS itself and takes
// neither the browser API's proxy nor its cookie, and it runs as a
// process of its own so the browser-facing one never holds the key the
// certificate authorities are sealed under (ADR-0017).
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
	ctx := cmd.Context()

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

	cert, sealer, err := machineKeyMaterial(
		envOr("GARAM_MACHINE_TLS_CERT_FILE", ""),
		envOr("GARAM_MACHINE_TLS_KEY_FILE", ""),
		envOr("GARAM_CA_KEY_FILE", ""),
	)
	if err != nil {
		return err
	}

	authorities := ca.NewPostgresStore(pool)
	issuer, err := ca.NewService(authorities, sealer, ca.Config{})
	if err != nil {
		return err
	}
	agents := agent.NewService(agent.NewPostgresStore(pool), certificates{issuer: issuer})

	// The generated router matches before the middleware runs, so the
	// log line can name the route rather than the path.
	handler := machinegen.HandlerWithOptions(machineapi.NewServer(agents), machinegen.StdHTTPServerOptions{
		Middlewares: []machinegen.MiddlewareFunc{obs.logMachineRequests},
	})
	srv := machine.NewServer(
		machine.NewService(certificateAuthorities{store: authorities}),
		cert,
		otelhttp.NewHandler(handler, serviceMachine),
	)
	srv.Addr = envOr("GARAM_MACHINE_ADDR", ":8443")
	// A refused handshake is the only refusal this listener makes, and
	// net/http reports one here rather than through a handler.
	srv.ErrorLog = slog.NewLogLogger(obs.logger.Handler(), slog.LevelWarn)

	obs.logger.Info("serving machine", slog.String("addr", srv.Addr))
	if err := srv.ListenAndServeTLS("", ""); err != nil {
		return errorx.Wrap(err, "machine serve", errorx.KindInternal)
	}
	return nil
}

// machineKeyMaterial loads the certificate the listener terminates TLS
// with, and the key-encryption key every certificate authority is
// sealed under. Both are files, because key material never travels in
// the environment (ADR-0015).
//
// This surface signs certificates, so the key it opens an authority
// with is what the process runs on: a missing, group-readable,
// malformed or wrong-length one refuses the start rather than the
// first certificate asked for (ADR-0016).
func machineKeyMaterial(certFile, keyFile, caKeyFile string) (tls.Certificate, ca.Sealer, error) {
	if certFile == "" || keyFile == "" {
		return tls.Certificate{}, nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_MACHINE_TLS_CERT_FILE and GARAM_MACHINE_TLS_KEY_FILE are required")
	}
	sealer, err := ca.NewSealerFromKeyFile(caKeyFile)
	if err != nil {
		return tls.Certificate{}, nil, errorx.Wrap(err, "GARAM_CA_KEY_FILE")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, nil, errorx.Wrap(err, "machine listener certificate", errorx.KindInvalidArgument)
	}
	return cert, sealer, nil
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

// certificates is the certificate authority as the agent domain signs
// through it (agent.Issuer). The two cooperate through grn.GRN alone,
// so the composition root is where what one issues becomes what the
// other hands back.
type certificates struct{ issuer ca.Service }

func (c certificates) Issue(ctx context.Context, subject grn.GRN) (*agent.Certificate, error) {
	issued, err := c.issuer.Issue(ctx, subject)
	if err != nil {
		return nil, err
	}
	return &agent.Certificate{
		CertificatePEM: issued.CertificatePEM(),
		PrivateKeyPEM:  issued.PrivateKeyPEM(),
		IssuerPEM:      issued.IssuerPEM(),
		NotAfter:       issued.NotAfter(),
	}, nil
}
