package cli

import (
	"bufio"
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/delivery"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/keyfile"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/workload"
	"github.com/garamsh/garam/platform/internal/workload/docker"
)

// runLockFile is what one `garam standalone run` holds in the
// supervisor's directory for as long as it runs, so two supervisors
// never act on one workload, and in the agent's directory too where it
// writes the attached adapter's files there. It is not hostLockFile,
// so the other commands, `renew` among them, still run beside it.
const runLockFile = ".run.lock"

// runSettings is what `garam standalone run` is told.
type runSettings struct {
	dirs       hostDirs
	engine     string
	machineURL string
	// adapterURL is where the adapter reaches garam from inside the
	// runtime's network namespace, which under a rootless daemon is not
	// where the host does; empty is machineURL.
	adapterURL       string
	configFile       string
	egoFile          string
	instructionsFile string
	modelEnv         string
	prefix           string
	images           workload.Images
	replace          bool
	moveMemoryTo     string
	// messageIssuer is garam's message-signing issuer; empty renders
	// no issuers entry.
	messageIssuer string
	// runtimeAddress is where a directly placed runtime's gateway
	// listens, a loopback literal (ADR-0111).
	runtimeAddress string
}

// newStandaloneRunCommand constructs `garam standalone run`: the host's
// supervisor, in the foreground. Under the standalone placement it runs
// over one rootless Docker daemon (ADR-0085): it brings the desired
// generation up or adopts the one running, serves its adapter's
// activation, and returns when the generation exits. Under the direct
// placement it attaches to the runtime the host runs at a loopback
// literal and starts nothing (ADR-0111). Either way restarting it is
// the host's init's to do.
func newStandaloneRunCommand(dirs *hostDirs) *cobra.Command {
	var s runSettings
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Supervise the agent's runtime, workspace and adapter on this host",
		RunE: func(cmd *cobra.Command, _ []string) error {
			s.dirs.supervisor = dirs.supervisor
			ctx, stop := serveSignalContext(cmd.Context())
			defer stop()
			return runStandalone(ctx, s)
		},
	}
	f := cmd.Flags()
	f.StringVar(&s.dirs.agent, "agent-dir", "", "the agent's credential directory (required)")
	f.StringVar(&s.engine, "engine", "", "the rootless Docker daemon, as unix://<socket> (required by the standalone placement)")
	f.StringVar(&s.machineURL, "machine-url", "", "garam's machine listener (required)")
	f.StringVar(&s.configFile, "config", "", "the runtime's configuration file, which names its revision (required)")
	f.StringVar(&s.egoFile, "ego-file", "", "the runtime's ego; empty is the runtime's own default")
	f.StringVar(&s.instructionsFile, "instructions-file", "", "the runtime's operator instructions; empty is none")
	f.StringVar(&s.adapterURL, "adapter-machine-url", "",
		"garam's machine listener as the adapter reaches it from its container (default --machine-url)")
	f.StringVar(&s.modelEnv, "model-env-file", "",
		"KEY=VALUE lines the runtime alone receives, readable by its owner alone (required by the standalone placement)")
	f.StringVar(&s.images.Runtime, "runtime-image", "", "the sherlock-agent image, by digest (required by the standalone placement)")
	f.StringVar(&s.images.Workspace, "workspace-image", "",
		"the sherlock-workspace image, by digest (required by the standalone placement)")
	f.StringVar(&s.images.Adapter, "adapter-image", "",
		"the garam image the adapter runs, by digest (required by the standalone placement)")
	f.StringVar(&s.runtimeAddress, "runtime-address", "",
		"the runtime's gateway as 127.0.0.1:<port> or [::1]:<port>, never a name (required by the direct placement)")
	f.StringVar(&s.prefix, "name-prefix", "garam-", "what every container and volume name begins with")
	f.BoolVar(&s.replace, "replace", false, "start a new generation even where the running one is what is desired")
	f.StringVar(&s.messageIssuer, "message-issuer", "",
		"garam's message-signing issuer, exactly as its machine.issuer names it; empty trusts no garam signature")
	f.StringVar(&s.moveMemoryTo, "move-memory-to", "",
		"drain the generation, copy and verify the agent's memory into this new volume, and run the next generation on it")
	for _, name := range []string{"agent-dir", "machine-url", "config"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
}

// placed refuses the settings the enrollment's placement cannot run
// with: one it needs and was not given, or one that would change
// nothing under it.
func (s runSettings) placed(p agent.Placement) error {
	standalone := map[string]string{
		"--engine": s.engine, "--model-env-file": s.modelEnv, "--runtime-image": s.images.Runtime,
		"--workspace-image": s.images.Workspace, "--adapter-image": s.images.Adapter,
	}
	var needed, unused map[string]string
	switch p {
	case agent.PlacementStandalone:
		needed, unused = standalone, map[string]string{"--runtime-address": s.runtimeAddress}
	case agent.PlacementDirect:
		needed, unused = map[string]string{"--runtime-address": s.runtimeAddress}, standalone
		for name, value := range map[string]string{
			"--ego-file": s.egoFile, "--instructions-file": s.instructionsFile,
			"--adapter-machine-url": s.adapterURL, "--move-memory-to": s.moveMemoryTo,
			"--message-issuer": s.messageIssuer,
		} {
			unused[name] = value
		}
	default:
		return errorx.New(errorx.KindDataLoss, "enrollment record: placement "+string(p))
	}
	for name, value := range needed {
		if value == "" {
			return errorx.New(errorx.KindInvalidArgument, name+" is required by the "+string(p)+" placement")
		}
	}
	for name, value := range unused {
		if value != "" {
			return errorx.New(errorx.KindInvalidArgument, name+" changes nothing under the "+string(p)+" placement")
		}
	}
	return nil
}

// placementOf reads the placement the enrollment recorded. A record an
// earlier release wrote names none, and every one of those is a
// standalone host's.
func placementOf(e *machinegen.StandaloneEnrollment) (agent.Placement, error) {
	if e.Placement == "" {
		return agent.PlacementStandalone, nil
	}
	p, err := agent.ParsePlacement(string(e.Placement))
	if err != nil || p == agent.PlacementManaged {
		return "", errorx.New(errorx.KindDataLoss, "enrollment record: placement "+string(e.Placement))
	}
	return p, nil
}

// runtimeAddressOf reads a directly placed runtime's address: the
// loopback literal 127.0.0.1 or ::1 and a port, never a name. localhost
// resolves to either family and the two are distinct addresses, so two
// runtimes could each bind one of them (ADR-0111).
func runtimeAddressOf(named string) (netip.AddrPort, error) {
	address, err := netip.ParseAddrPort(named)
	if err != nil || address.Port() == 0 ||
		address.Addr() != netip.AddrFrom4([4]byte{127, 0, 0, 1}) && address.Addr() != netip.IPv6Loopback() {
		return netip.AddrPort{}, errorx.New(errorx.KindInvalidArgument,
			"--runtime-address is 127.0.0.1:<port> or [::1]:<port>, never a name")
	}
	return address, nil
}

// runtimeStatusReader reads an attached runtime's status through the
// adapter's own reader of it, so the supervisor and the adapter hold
// the runtime to one parse.
type runtimeStatusReader struct {
	gateway *delivery.HTTPGateway
}

func (r runtimeStatusReader) Status(ctx context.Context) (*workload.RuntimeStatus, error) {
	status, err := r.gateway.Status(ctx)
	if err != nil {
		return nil, err
	}
	return &workload.RuntimeStatus{
		Epoch: status.Epoch, Generation: status.Generation, Revision: status.Revision, State: string(status.State),
	}, nil
}

// lockRun takes dir's run lock without waiting, and answers what
// releases it. One another run holds refuses this one.
func lockRun(dir string) (func(), error) {
	lock, err := os.OpenFile(filepath.Join(dir, runLockFile), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, errorx.Wrap(err, "lock "+dir, errorx.KindInvalidArgument)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, errorx.Wrap(err, "another supervisor runs over "+dir, errorx.KindFailedPrecondition)
	}
	return func() { _ = lock.Close() }, nil
}

// runStandalone composes the supervisor from what the host holds and
// runs it until the generation ends or ctx does.
func runStandalone(ctx context.Context, s runSettings) error {
	base, err := machineEndpoint(s.machineURL)
	if err != nil {
		return err
	}
	dirs, err := resolvedDirs(s.dirs)
	if err != nil {
		return err
	}
	release, err := lockRun(dirs.supervisor)
	if err != nil {
		return err
	}
	defer release()
	enrolled, err := readEnrollment(dirs.supervisor)
	if err != nil {
		return errorx.Wrap(err, "run `garam standalone enroll` first", errorx.KindFailedPrecondition)
	}
	agentGRN, err := grn.Parse(enrolled.Grn)
	if err != nil {
		return errorx.Wrap(err, "enrollment record", errorx.KindDataLoss)
	}
	placement, err := placementOf(enrolled)
	if err != nil {
		return err
	}
	if err := s.placed(placement); err != nil {
		return err
	}
	desired, err := desiredRuntime(s, agentGRN, enrolled.Epoch)
	if err != nil {
		return err
	}
	var replaces string
	if enrolled.ReplacesActivationId != nil {
		replaces = enrolled.ReplacesActivationId.String()
	}
	if placement == agent.PlacementDirect {
		return runAttached(ctx, s, dirs, base, desired, replaces)
	}
	adapterBase := base
	if s.adapterURL != "" {
		if adapterBase, err = machineEndpoint(s.adapterURL); err != nil {
			return err
		}
	}
	session, err := newSupervisorTokens(dirs.supervisor, base, agentGRN)
	if err != nil {
		return err
	}
	files, err := adapterFilesOf(dirs.agent, adapterBase)
	if err != nil {
		return err
	}
	issuer, err := messageIssuerOf(s.messageIssuer)
	if err != nil {
		return err
	}
	engine, err := docker.New(s.engine)
	if err != nil {
		return err
	}
	defer func() { _ = engine.Close() }()
	supervisor, err := workload.NewSupervisor(engine, session, workload.Config{
		Dir: dirs.supervisor, Prefix: s.prefix, Desired: desired, Adapter: files,
		MessageIssuer: string(issuer), Tokens: session, Replaces: replaces,
	})
	if err != nil {
		return err
	}
	return supervisor.Run(ctx)
}

// runAttached composes the supervisor's attach mode for a directly
// placed runtime and runs it until the generation it forwarded ends or
// ctx does (ADR-0111). It holds no engine, and the adapter is handed
// tokens in the agent's directory.
func runAttached(ctx context.Context, s runSettings, dirs hostDirs, base string, desired workload.Desired, replaces string) error {
	address, err := runtimeAddressOf(s.runtimeAddress)
	if err != nil {
		return err
	}
	release, err := lockRun(dirs.agent)
	if err != nil {
		return err
	}
	defer release()
	session, err := newSupervisorTokens(dirs.supervisor, base, desired.Agent)
	if err != nil {
		return err
	}
	gateway, err := delivery.NewHTTPGateway(gatewayClient(), "http://"+address.String(), desired.Agent)
	if err != nil {
		return err
	}
	supervisor, err := workload.NewSupervisor(nil, session, workload.Config{
		Dir: dirs.supervisor, Prefix: s.prefix, Desired: desired, Tokens: session, Replaces: replaces,
		Attach: &workload.Attachment{Status: runtimeStatusReader{gateway: gateway}, AdapterDir: dirs.agent},
	})
	if err != nil {
		return err
	}
	return supervisor.Run(ctx)
}

// messageIssuerOf is garam's message-signing issuer as the host names
// it, held to garam's one spelling, since the runtime compares it byte
// for byte. Nothing named is no issuer: the issuer is an identity, not
// an address, so no address the host reaches garam at stands in for it,
// and a host not ready to verify stays as it was.
func messageIssuerOf(named string) (message.Issuer, error) {
	if named == "" {
		return "", nil
	}
	issuer, err := message.ParseIssuer(named)
	if err != nil {
		return "", errorx.Wrap(err, "--message-issuer")
	}
	return issuer, nil
}

// desiredRuntime reads what the host asks to run: the runtime's
// configuration, the revision it names, and the model credential.
func desiredRuntime(s runSettings, agentGRN grn.GRN, epoch string) (workload.Desired, error) {
	config, err := os.ReadFile(s.configFile)
	if err != nil {
		return workload.Desired{}, errorx.Wrap(err, "runtime configuration", errorx.KindInvalidArgument)
	}
	var named struct {
		Revision string `yaml:"revision"`
	}
	if err := yaml.Unmarshal(config, &named); err != nil {
		return workload.Desired{}, errorx.Wrap(err, "runtime configuration", errorx.KindInvalidArgument)
	}
	if named.Revision == "" {
		return workload.Desired{}, errorx.New(errorx.KindInvalidArgument,
			"the runtime configuration names no revision, so nothing could tell its status apart")
	}
	// A directly placed runtime takes its environment from the host,
	// and placed refuses a model environment for it.
	var env []string
	if s.modelEnv != "" {
		if env, err = readModelEnv(s.modelEnv); err != nil {
			return workload.Desired{}, err
		}
	}
	desired := workload.Desired{
		Agent: agentGRN, Epoch: epoch, Config: config, Revision: named.Revision,
		ModelEnv: env, Images: s.images, Replace: s.replace, MoveMemoryTo: s.moveMemoryTo,
	}
	for _, f := range []struct {
		path string
		into *[]byte
		what string
	}{{s.egoFile, &desired.Ego, "ego"}, {s.instructionsFile, &desired.Instructions, "operator instructions"}} {
		if f.path == "" {
			continue
		}
		raw, err := os.ReadFile(f.path)
		if err != nil {
			return workload.Desired{}, errorx.Wrap(err, f.what, errorx.KindInvalidArgument)
		}
		*f.into = raw
	}
	return desired, nil
}

// readModelEnv reads KEY=VALUE lines from a file only its owner can
// read, skipping blank lines and # comments.
func readModelEnv(path string) ([]string, error) {
	raw, err := keyfile.Read(path)
	if err != nil {
		return nil, errorx.Wrap(err, "model environment")
	}
	var env []string
	lines := bufio.NewScanner(bytes.NewReader(raw))
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, _, ok := strings.Cut(line, "="); !ok || key == "" {
			return nil, errorx.New(errorx.KindInvalidArgument, "model environment: a line is not KEY=VALUE")
		}
		env = append(env, line)
	}
	return env, nil
}

// adapterFilesOf reads what the enrollment installed in the agent's
// directory: the garam server root alone, since the adapter is handed
// tokens (ADR-0100).
func adapterFilesOf(dir, machineURL string) (workload.AdapterFiles, error) {
	root, err := os.ReadFile(filepath.Join(dir, serverRootFile))
	if err != nil {
		return workload.AdapterFiles{}, errorx.Wrap(err, "agent directory: "+serverRootFile, errorx.KindFailedPrecondition)
	}
	return workload.AdapterFiles{ServerRootPEM: root, MachineURL: machineURL}, nil
}
