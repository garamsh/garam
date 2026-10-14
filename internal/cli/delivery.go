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
		certFile:       envOr(envAdapterTLSCertFile, ""),
		keyFile:        envOr(envAdapterTLSKeyFile, ""),
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
	agent, machineURL, gatewayURL     string
	certFile, keyFile, serverRootFile string
	// tokenFile and controlToken are the token mode's files: the agent
	// token garam accepts and, where the adapter is fenced, the one its
	// lifecycle authority accepts (ADR-0100). Each is read before every
	// call to its peer.
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

// fenced reports which mode the settings choose (ADR-0084): all three
// control settings select the fenced one, none the legacy one, and
// anything between is refused rather than read as either.
func (s adapterSettings) fenced() (bool, error) {
	set := 0
	for _, v := range []string{s.controlURL, s.controlRoot, s.placementToken} {
		if v != "" {
			set++
		}
	}
	switch set {
	case 0:
		if s.controlSocket != "" {
			return false, errorx.New(errorx.KindInvalidArgument, "GARAM_ADAPTER_CONTROL_SOCKET is set only beside the control settings")
		}
		return false, nil
	case 3:
		if s.outboxDir == "" {
			return false, errorx.New(errorx.KindInvalidArgument, "GARAM_ADAPTER_OUTBOX_DIR is required beside the control settings")
		}
		return true, nil
	}
	return false, errorx.New(errorx.KindInvalidArgument,
		"GARAM_ADAPTER_CONTROL_URL, GARAM_ADAPTER_CONTROL_ROOT_FILE and GARAM_ADAPTER_PLACEMENT_TOKEN_FILE are set together or not at all")
}

// credentials answers what the adapter's calls present to garam and to
// its lifecycle authority: the agent's certificate to both, or a token
// file to each (ADR-0100). Exactly one of the two modes is named, and
// neither falls back to the other. The control credential is nil for
// a legacy adapter; the pair is nil in the token mode.
func (s adapterSettings) credentials(fenced bool) (garam, control delivery.Credential, pair *keyfile.Pair, err error) {
	if s.tokenFile == "" {
		if s.controlToken != "" {
			return nil, nil, nil, errorx.New(errorx.KindInvalidArgument,
				"GARAM_ADAPTER_CONTROL_TOKEN_FILE is set only beside GARAM_ADAPTER_TOKEN_FILE")
		}
		if pair, err = adapterPair(s.certFile, s.keyFile); err != nil {
			return nil, nil, nil, err
		}
		garam = delivery.NewCertificate(pair.Get)
		if fenced {
			control = garam
		}
		return garam, control, pair, nil
	}
	if s.certFile != "" || s.keyFile != "" {
		return nil, nil, nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_ADAPTER_TOKEN_FILE and the GARAM_ADAPTER_TLS_* files are two modes, and only one is set")
	}
	switch {
	case fenced && s.controlToken == "":
		return nil, nil, nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_ADAPTER_CONTROL_TOKEN_FILE is required beside the control settings and GARAM_ADAPTER_TOKEN_FILE")
	case !fenced && s.controlToken != "":
		return nil, nil, nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_ADAPTER_CONTROL_TOKEN_FILE is set only beside the control settings")
	}
	tokens, err := delivery.NewTokenFile(s.tokenFile)
	if err != nil {
		return nil, nil, nil, err
	}
	if fenced {
		if control, err = delivery.NewTokenFile(s.controlToken); err != nil {
			return nil, nil, nil, err
		}
	}
	return tokens, control, nil, nil
}

// adapterLoop builds the loop from what the environment named, or
// refuses what it cannot run on. One GRN names the agent to garam and
// to the runtime beside it, which serves the agent under that GRN. The
// control settings choose the fenced or legacy mode, the credential
// files the certificate or token mode, and the outbox is forwarded from
// wherever it is named.
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
	fenced, err := s.fenced()
	if err != nil {
		return nil, err
	}
	garam, control, pair, err := s.credentials(fenced)
	if err != nil {
		return nil, err
	}
	client, err := adapterClient(pair, s.serverRootFile, string(envAdapterServerRootFile), "")
	if err != nil {
		return nil, err
	}
	ends := delivery.Ends{
		Agent:       agent,
		Execution:   delivery.NewLegacyExecution(),
		Credentials: delivery.NewFileCredentials(s.placementToken, garam),
	}
	if fenced {
		ends.Execution = delivery.NewFencedExecution()
		ends.Credentials = delivery.NewFileCredentials(s.placementToken, garam, control)
		controlClient, err := adapterClient(pair, s.controlRoot, string(envAdapterControlRoot), s.controlSocket)
		if err != nil {
			return nil, err
		}
		if ends.Authority, err = delivery.NewHTTPAuthority(controlClient, s.controlURL, agent, control); err != nil {
			return nil, err
		}
	}
	if s.outboxDir != "" {
		if ends.Outbox, err = delivery.NewOutbox(s.outboxDir); err != nil {
			return nil, err
		}
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

// adapterPair is the agent's certificate and key, read per handshake
// so a renewed one needs no restart.
func adapterPair(certFile, keyFile string) (*keyfile.Pair, error) {
	if certFile == "" || keyFile == "" {
		return nil, errorx.New(errorx.KindInvalidArgument,
			"GARAM_ADAPTER_TLS_CERT_FILE and GARAM_ADAPTER_TLS_KEY_FILE are required, or GARAM_ADAPTER_TOKEN_FILE in their place")
	}
	pair, err := keyfile.NewPair(certFile, keyFile)
	if err != nil {
		return nil, errorx.Wrap(err, "adapter certificate")
	}
	return pair, nil
}

// adapterClient is a connection to garam or control. In the
// certificate mode the agent's certificate authenticates it, presented
// per handshake so a renewed one needs no restart; in the token mode
// pair is nil and the connection presents none, since a token in each
// request authenticates it (ADR-0100). Either way the peer is verified
// against a root of its own — garam's server root, which the authority
// that signed the agent never signs (ADR-0040), or the root control's
// serving certificate chains to (ADR-0084).
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
func adapterClient(pair *keyfile.Pair, rootFile, what, socket string) (*http.Client, error) {
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
	if pair != nil {
		config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return pair.Get()
		}
	}
	transport := &http.Transport{TLSClientConfig: config}
	if socket != "" {
		var dialer net.Dialer
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		}
	}
	return &http.Client{Transport: transport}, nil
}
