package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
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
// loopback, completes the claim when the platform says its turn
// committed, and forwards the platform's outbox; with control settings
// it does so under its generation's activation (ADR-0084). It serves
// nothing, so it is a subcommand of the one CLI but not one of `serve`
// (ADR-0003, ADR-0012).
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

	sign, err := envBool(envAdapterSignMessages, "false")
	if err != nil {
		return err
	}
	loop, err := adapterLoop(adapterSettings{
		agent:          envOr(envAdapterAgent, ""),
		machineURL:     envOr(envAdapterMachineURL, ""),
		gatewayURL:     envOr(envAdapterGatewayURL, defaultGatewayURL),
		tokenFile:      envOr(envAdapterTokenFile, ""),
		controlToken:   envOr(envAdapterControlToken, ""),
		serverRootFile: envOr(envAdapterServerRootFile, ""),
		controlURL:     envOr(envAdapterControlURL, ""),
		controlRoot:    envOr(envAdapterControlRoot, ""),
		placementToken: envOr(envAdapterPlacementToken, ""),
		outboxDir:      envOr(envAdapterOutboxDir, ""),
		controlSocket:  envOr(envAdapterControlSocket, ""),
		sign:           sign,
	})
	if err != nil {
		return err
	}

	obs.logger.Info("carrying messages")
	// A requested stop returns nil, as it does for every serve command:
	// the pairings this process was holding go with it, their leases
	// lapse, and garam hands those messages to whoever claims next. The
	// outbox stays where the runtime wrote it.
	return loop.Run(ctx)
}

// adapterSettings is what the environment names the adapter.
type adapterSettings struct {
	agent, machineURL, gatewayURL string
	serverRootFile                string
	// tokenFile and controlToken are the adapter's credential files:
	// the agent token garam accepts and the one its lifecycle authority
	// accepts (ADR-0100). Each is read before every call to its peer.
	tokenFile, controlToken   string
	controlURL, controlRoot   string
	placementToken, outboxDir string
	// controlSocket is a unix socket every control connection dials
	// in place of controlURL's host, which TLS still verifies: a
	// standalone supervisor serves control on one (ADR-0085).
	controlSocket string
	// sign forwards garam's signature beside each message (ADR-0101).
	// It is whoever runs the adapter who knows the runtime accepts it.
	sign bool
}

// controlled refuses settings that do not name the adapter's lifecycle
// authority. Every agent's writes are fenced by an activation, which
// the authority answers, so the three control settings are required
// together, and the outbox beside them (ADR-0084, ADR-0118).
func (s adapterSettings) controlled() error {
	if s.controlURL == "" || s.controlRoot == "" || s.placementToken == "" {
		return errorx.New(errorx.KindInvalidArgument,
			"GARAM_ADAPTER_CONTROL_URL, GARAM_ADAPTER_CONTROL_ROOT_FILE and GARAM_ADAPTER_PLACEMENT_TOKEN_FILE are required")
	}
	if s.outboxDir == "" {
		return errorx.New(errorx.KindInvalidArgument, "GARAM_ADAPTER_OUTBOX_DIR is required beside the control settings")
	}
	return nil
}

// credentials answers what the adapter's calls present to garam and to
// its lifecycle authority: a token file to each (ADR-0100, ADR-0118).
func (s adapterSettings) credentials() (garam, control delivery.Credential, err error) {
	if s.tokenFile == "" || s.controlToken == "" {
		return nil, nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_ADAPTER_TOKEN_FILE and GARAM_ADAPTER_CONTROL_TOKEN_FILE are required")
	}
	tokens, err := delivery.NewTokenFile(s.tokenFile)
	if err != nil {
		return nil, nil, err
	}
	if control, err = delivery.NewTokenFile(s.controlToken); err != nil {
		return nil, nil, err
	}
	return tokens, control, nil
}

// adapterLoop builds the loop from what the environment named, or
// refuses what it cannot run on. One GRN names the agent to garam and
// to the runtime beside it, which serves the agent under that GRN. The
// control settings name its lifecycle authority, the token files are
// what it presents, and the outbox is forwarded from where it is named.
func adapterLoop(s adapterSettings) (delivery.Service, error) {
	if s.agent == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "GARAM_ADAPTER_AGENT is required")
	}
	agent, err := grn.Parse(s.agent)
	if err != nil {
		return nil, errorx.Wrap(err, string(envAdapterAgent), errorx.KindInvalidArgument)
	}
	if s.machineURL == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "GARAM_ADAPTER_MACHINE_URL is required")
	}
	if err := s.controlled(); err != nil {
		return nil, err
	}
	garam, control, err := s.credentials()
	if err != nil {
		return nil, err
	}
	client, err := adapterClient(s.serverRootFile, string(envAdapterServerRootFile), "")
	if err != nil {
		return nil, err
	}
	ends := delivery.Ends{
		Agent:       agent,
		Execution:   delivery.NewFencedExecution(),
		Credentials: delivery.NewFileCredentials(s.placementToken, garam, control),
	}
	controlClient, err := adapterClient(s.controlRoot, string(envAdapterControlRoot), s.controlSocket)
	if err != nil {
		return nil, err
	}
	if ends.Authority, err = delivery.NewHTTPAuthority(controlClient, s.controlURL, agent, control); err != nil {
		return nil, err
	}
	if ends.Outbox, err = delivery.NewOutbox(s.outboxDir); err != nil {
		return nil, err
	}
	if ends.Queue, err = delivery.NewHTTPQueue(client, s.machineURL, agent, ends.Execution, garam); err != nil {
		return nil, err
	}
	// The hop to the gateway is loopback inside one pod and carries no
	// certificate. What it buys is that the platform stays a separable
	// product rather than a library, which is the boundary ADR-0013
	// pays for.
	if ends.Gateway, err = delivery.NewHTTPGateway(gatewayClient(), s.gatewayURL, agent); err != nil {
		return nil, err
	}
	return delivery.NewService(ends, delivery.Config{ForwardSignature: s.sign})
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

// adapterClient is a connection to garam or control. The connection
// presents no certificate, since a token in each request authenticates
// it (ADR-0100), and the peer is verified against a root of its own —
// garam's server root (ADR-0040), or the root control's serving
// certificate chains to (ADR-0084).
//
// The client carries no timeout, and one added here breaks the adapter:
// a claim is a long poll and the event stream is held open for as long
// as the agent runs. Each call carries its own deadline instead
// (delivery.Config's ClaimTimeout and CallTimeout).
//
// A socket, where one is given, is what every connection dials,
// whatever address a request names; TLS still verifies the URL's
// host, so a standalone supervisor's control is reached on the unix
// socket it serves (ADR-0085).
func adapterClient(rootFile, what, socket string) (*http.Client, error) {
	if rootFile == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, what+" is required")
	}
	rootPEM, err := os.ReadFile(rootFile)
	if err != nil {
		return nil, errorx.Wrap(err, what, errorx.KindInvalidArgument)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return nil, errorx.New(errorx.KindInvalidArgument, what+": no certificate in "+rootFile)
	}
	config := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
	transport := &http.Transport{TLSClientConfig: config}
	if socket != "" {
		var dialer net.Dialer
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		}
	}
	return &http.Client{Transport: transport}, nil
}
