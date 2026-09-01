package cli

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"time"

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

// gatewayHeaderTimeout bounds how long the gateway has to begin
// answering. It covers opening the event stream and nothing after, so
// it is generous against a platform starting up rather than tight
// against a slow one.
const gatewayHeaderTimeout = 30 * time.Second

// newAdapterCommand constructs `garam adapter`: the delivery loop of
// docs/architecture/adapter.md, running beside one agent. It claims a
// message for that agent, posts it into the platform's gateway over
// loopback, and completes the claim when a turn reports success and
// names the message. It serves nothing, so it is a subcommand of the
// one CLI but not one of `serve` (ADR-0003, ADR-0012).
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
	gateway, err := delivery.NewHTTPGateway(gatewayClient(), gatewayURL, gatewayAgent)
	if err != nil {
		return nil, err
	}
	return delivery.NewService(queue, gateway, delivery.Config{})
}

// gatewayClient reaches the platform beside this adapter. It carries no
// Timeout for the reason adapterClient carries none, and bounds opening
// a response instead: ResponseHeaderTimeout runs until the headers
// arrive and stops there, so a stream held open for the agent's life is
// untouched while a peer that accepts and never answers is not.
func gatewayClient() *http.Client {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{}
	}
	bounded := transport.Clone()
	bounded.ResponseHeaderTimeout = gatewayHeaderTimeout
	return &http.Client{Transport: bounded}
}

// adapterClient is the connection garam sees: the agent's own
// certificate, presented per handshake so a renewed one needs no
// restart, and the garam server root as a separate input, because the
// authority that signed the agent signs no server certificate
// (ADR-0040).
//
// The client carries no timeout, and one added here breaks the adapter:
// a claim is a long poll and the event stream is held open for as long
// as the agent runs. Each call carries its own deadline instead
// (delivery.Config's ClaimTimeout and CallTimeout).
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
