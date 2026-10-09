package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/keyfile"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/workload"
	"github.com/garamsh/garam/internal/workload/docker"
)

// runLockFile is what one `garam standalone run` holds in the
// supervisor's directory for as long as it runs, so two supervisors
// never act on one workload. It is not hostLockFile, so the other
// commands, `renew` among them, still run beside it.
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
}

// newStandaloneRunCommand constructs `garam standalone run`: the host's
// supervisor, in the foreground, over one rootless Docker daemon
// (ADR-0085). It brings the desired generation up or adopts the one
// running, serves its adapter's activation, and returns when the
// generation exits; restarting it is the host's init's to do.
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
	f.StringVar(&s.engine, "engine", "", "the rootless Docker daemon, as unix://<socket> (required)")
	f.StringVar(&s.machineURL, "machine-url", "", "garam's machine listener (required)")
	f.StringVar(&s.configFile, "config", "", "the runtime's configuration file, which names its revision (required)")
	f.StringVar(&s.egoFile, "ego-file", "", "the runtime's ego; empty is the runtime's own default")
	f.StringVar(&s.instructionsFile, "instructions-file", "", "the runtime's operator instructions; empty is none")
	f.StringVar(&s.adapterURL, "adapter-machine-url", "",
		"garam's machine listener as the adapter reaches it from its container (default --machine-url)")
	f.StringVar(&s.modelEnv, "model-env-file", "", "KEY=VALUE lines the runtime alone receives, readable by its owner alone (required)")
	f.StringVar(&s.images.Runtime, "runtime-image", "", "the sherlock-agent image, by digest (required)")
	f.StringVar(&s.images.Workspace, "workspace-image", "", "the sherlock-workspace image, by digest (required)")
	f.StringVar(&s.images.Adapter, "adapter-image", "", "the garam image the adapter runs, by digest (required)")
	f.StringVar(&s.prefix, "name-prefix", "garam-", "what every container and volume name begins with")
	f.BoolVar(&s.replace, "replace", false, "start a new generation even where the running one is what is desired")
	f.StringVar(&s.messageIssuer, "message-issuer", "",
		"garam's message-signing issuer, exactly as its machine.issuer names it; empty trusts no garam signature")
	f.StringVar(&s.moveMemoryTo, "move-memory-to", "",
		"drain the generation, copy and verify the agent's memory into this new volume, and run the next generation on it")
	for _, name := range []string{"agent-dir", "engine", "machine-url", "config", "model-env-file",
		"runtime-image", "workspace-image", "adapter-image"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return cmd
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
	lock, err := os.OpenFile(filepath.Join(dirs.supervisor, runLockFile), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return errorx.Wrap(err, "lock "+dirs.supervisor, errorx.KindInvalidArgument)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errorx.Wrap(err, "another supervisor runs over "+dirs.supervisor, errorx.KindFailedPrecondition)
	}
	enrolled, err := readEnrollment(dirs.supervisor)
	if err != nil {
		return errorx.Wrap(err, "run `garam standalone enroll` first", errorx.KindFailedPrecondition)
	}
	agentGRN, err := grn.Parse(enrolled.Grn)
	if err != nil {
		return errorx.Wrap(err, "enrollment record", errorx.KindDataLoss)
	}
	desired, err := desiredRuntime(s, agentGRN, enrolled.Epoch)
	if err != nil {
		return err
	}
	adapterBase := base
	if s.adapterURL != "" {
		if adapterBase, err = machineEndpoint(s.adapterURL); err != nil {
			return err
		}
	}
	files, err := adapterFilesOf(dirs.agent, adapterBase)
	if err != nil {
		return err
	}
	activator, err := newGaramActivator(dirs.supervisor, base, agentGRN)
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
	supervisor, err := workload.NewSupervisor(engine, activator, workload.Config{
		Dir: dirs.supervisor, Prefix: s.prefix, Desired: desired, Adapter: files,
		MessageIssuer: string(issuer),
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
	env, err := readModelEnv(s.modelEnv)
	if err != nil {
		return workload.Desired{}, err
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
// directory, which the adapter's container receives.
func adapterFilesOf(dir, machineURL string) (workload.AdapterFiles, error) {
	key, err := keyfile.Read(filepath.Join(dir, agentKeyFile))
	if err != nil {
		return workload.AdapterFiles{}, errorx.Wrap(err, "agent key")
	}
	files := workload.AdapterFiles{KeyPEM: key, MachineURL: machineURL}
	for _, f := range []struct {
		name string
		into *[]byte
	}{
		{agentCertFile, &files.CertificatePEM},
		{issuerFile, &files.IssuerPEM},
		{serverRootFile, &files.ServerRootPEM},
	} {
		raw, err := os.ReadFile(filepath.Join(dir, f.name))
		if err != nil {
			return workload.AdapterFiles{}, errorx.Wrap(err, "agent directory: "+f.name, errorx.KindFailedPrecondition)
		}
		*f.into = raw
	}
	return files, nil
}

// garamActivator calls activateAgent with the supervisor's own
// certificate, read per handshake so a renewal needs no restart.
type garamActivator struct {
	client *http.Client
	url    string
	agent  grn.GRN
}

func newGaramActivator(dir, base string, agentGRN grn.GRN) (*garamActivator, error) {
	rootPEM, err := os.ReadFile(filepath.Join(dir, serverRootFile))
	if err != nil {
		return nil, errorx.Wrap(err, "server root", errorx.KindFailedPrecondition)
	}
	current := filepath.Join(dir, supervisorFile)
	pair, err := keyfile.NewPair(current, current)
	if err != nil {
		return nil, err
	}
	client, err := standaloneClient(rootPEM, pair)
	if err != nil {
		return nil, err
	}
	return &garamActivator{client: client, url: base + "/agents/" + agentGRN.String() + "/activations", agent: agentGRN}, nil
}

func (g *garamActivator) Activate(ctx context.Context, req workload.ActivationRequest) (*workload.Activation, error) {
	body := machinegen.ActivationRequest{
		RequestId: req.RequestID, Epoch: req.Epoch, Generation: req.Generation,
		CertificatePem: req.CertificatePEM,
	}
	if req.Replaces != "" {
		body.ReplacesActivationId = &req.Replaces
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, errorx.Wrap(err, "activation", errorx.KindInternal)
	}
	var answer machinegen.Activation
	status, err := callStandalone(ctx, g.client, g.url, agent.ExecutionContractVersion, "", raw, &answer)
	if err != nil {
		return nil, err
	}
	return &workload.Activation{
		ID: answer.ActivationId, Token: answer.Token, TokenVersion: answer.TokenVersion,
		GRN: answer.Grn, Epoch: answer.Epoch, Generation: answer.Generation,
		Replayed: status == http.StatusOK,
	}, nil
}
