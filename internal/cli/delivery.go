package cli

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/keyfile"
)

// defaultGatewayURL is where the agent platform's gateway answers when
// nothing says otherwise: the loopback address it binds unless mTLS is
// configured for it (ADR-0012, ADR-0013).
const defaultGatewayURL = "http://127.0.0.1:8080"

// newAdapterCommand constructs `garam adapter`: the delivery loop of
// docs/architecture/adapter.md, running beside one agent. It claims a
// message for that agent, posts it into the platform's gateway over
// loopback, and completes the claim when a turn reports success and
// names the message.
//
// It is a subcommand of the one CLI, which is the topology ADR-0003
// fixed and ADR-0017 changed the process count of and nothing else. It
// serves nothing, so it is not under `serve`: the connection to garam
// starts here, because no orchestrator exposes an agent to garam
// (ADR-0012).
func newAdapterCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "adapter",
		Short: "Carry messages between garam and the agent beside it",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return adapt(cmd)
		},
	}
}

func adapt(cmd *cobra.Command) error {
	ctx, stop := serveSignalContext(cmd.Context())
	defer stop()

	obs, err := newObservability(serviceAdapter)
	if err != nil {
		return err
	}

	loop, err := adapterLoop(
		envOr(envAdapterAgent, ""),
		envOr(envAdapterMachineURL, ""),
		envOr(envAdapterGatewayURL, defaultGatewayURL),
		envOr(envAdapterGatewayAgent, ""),
		envOr(envAdapterTLSCertFile, ""),
		envOr(envAdapterTLSKeyFile, ""),
		envOr(envAdapterServerRootFile, ""),
	)
	if err != nil {
		return err
	}

	obs.logger.Info("carrying messages")
	// A requested stop returns nil, as it does for every serve command:
	// the pairings this process was holding go with it, their leases
	// lapse, and garam hands those messages to whoever claims next.
	return loop.Run(ctx)
}

// adapterLoop builds the loop from what the environment named, or
// refuses what it cannot run on. Everything is required but the
// gateway's address, which has a default: the two identifiers are
// different names for one agent — garam's GRN and what the gateway
// itself serves — and neither can be guessed from the other.
func adapterLoop(agentRaw, machineURL, gatewayURL, gatewayAgent, certFile, keyFile, serverRootFile string) (delivery.Service, error) {
	if agentRaw == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "GARAM_ADAPTER_AGENT is required")
	}
	agent, err := grn.Parse(agentRaw)
	if err != nil {
		return nil, errorx.Wrap(err, string(envAdapterAgent), errorx.KindInvalidArgument)
	}
	if machineURL == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "GARAM_ADAPTER_MACHINE_URL is required")
	}
	if gatewayAgent == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "GARAM_ADAPTER_GATEWAY_AGENT is required")
	}
	client, err := adapterClient(certFile, keyFile, serverRootFile)
	if err != nil {
		return nil, err
	}
	queue, err := delivery.NewHTTPQueue(client, machineURL, agent)
	if err != nil {
		return nil, err
	}
	// The hop to the gateway is loopback inside one pod and carries no
	// certificate. What it buys is that the platform stays a separable
	// product rather than a library, which is the boundary ADR-0013
	// pays for.
	gateway, err := delivery.NewHTTPGateway(&http.Client{}, gatewayURL, gatewayAgent)
	if err != nil {
		return nil, err
	}
	return delivery.NewService(queue, gateway, delivery.Config{})
}

// adapterClient is the connection garam sees: the agent's own
// certificate, and the garam server root as what garam's listener is
// verified against. The two are different roots — the authority that
// signed the agent signs no server certificate — so the root is an
// input of its own rather than the issuer travelling beside the
// certificate (ADR-0040).
//
// The certificate is presented per handshake rather than frozen at
// startup, for the reason the listener reads its own per handshake: a
// renewed one is presented on the next connection and this process
// keeps running.
//
// The private key is key material, so it is read under the owner-only
// rule every key file here is read under (ADR-0015); the certificate
// and the root beside it are public and are read plainly.
//
// The client carries no timeout: a claim is a long poll and the event
// stream is held open for as long as the agent runs, so what bounds a
// call is the context rather than the client.
func adapterClient(certFile, keyFile, serverRootFile string) (*http.Client, error) {
	if certFile == "" || keyFile == "" || serverRootFile == "" {
		return nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_ADAPTER_TLS_CERT_FILE, GARAM_ADAPTER_TLS_KEY_FILE and GARAM_ADAPTER_SERVER_ROOT_FILE are required")
	}
	pair, err := keyfile.NewPair(certFile, keyFile)
	if err != nil {
		return nil, errorx.Wrap(err, "adapter certificate")
	}
	serverRootPEM, err := os.ReadFile(serverRootFile)
	if err != nil {
		return nil, errorx.Wrap(err, "adapter server root", errorx.KindInvalidArgument)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(serverRootPEM) {
		return nil, errorx.New(errorx.KindInvalidArgument, "adapter server root: no certificate in "+serverRootFile)
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return pair.Get()
		},
		RootCAs:    roots,
		MinVersion: tls.VersionTLS13,
	}}}, nil
}
