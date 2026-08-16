package cli

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/org"
)

// newServeMachineCommand constructs `garam serve machine`: the
// listener an agent or an operator authenticates on with a client
// certificate (ADR-0014, ADR-0015). It terminates TLS itself and takes
// neither the browser API's proxy nor its cookie, which is why it is a
// listener of its own rather than a route.
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

	cert, err := machineKeyMaterial(
		envOr("GARAM_MACHINE_TLS_CERT_FILE", ""),
		envOr("GARAM_MACHINE_TLS_KEY_FILE", ""),
		envOr("GARAM_CA_KEY_FILE", ""),
	)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("GET /identity", machine.IdentityHandler())
	srv := machine.NewServer(
		machine.NewService(certificateAuthorities{store: ca.NewPostgresStore(pool)}),
		cert,
		otelhttp.NewHandler(obs.logMachineRequests(mux), serviceMachine),
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
// with, and requires the key-encryption key every certificate
// authority is sealed under. Both are files, because key material
// never travels in the environment (ADR-0015).
//
// The key is read and dropped. Nothing on this listener signs a
// certificate yet, so what the read buys is the refusal: the process
// that owns the certificate authorities does not start without the key
// that opens them (ADR-0016), rather than failing at the first
// certificate it is asked for.
func machineKeyMaterial(certFile, keyFile, caKeyFile string) (tls.Certificate, error) {
	if certFile == "" || keyFile == "" {
		return tls.Certificate{}, errorx.New(errorx.KindInvalidArgument,
			"GARAM_MACHINE_TLS_CERT_FILE and GARAM_MACHINE_TLS_KEY_FILE are required")
	}
	if _, err := ca.NewSealerFromKeyFile(caKeyFile); err != nil {
		return tls.Certificate{}, errorx.Wrap(err, "GARAM_CA_KEY_FILE")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, errorx.Wrap(err, "machine listener certificate", errorx.KindInvalidArgument)
	}
	return cert, nil
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
