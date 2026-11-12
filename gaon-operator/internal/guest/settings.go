package guest

import (
	"fmt"
	"io"
	"log/slog"
	"strconv"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// Where the image holds each program, and the loopback addresses the
// runtime and the workspace listen on (the standalone runner's).
const (
	sherlockPath  = "/sherlock"
	garamPath     = "/usr/local/bin/garam"
	gatewayAddr   = "127.0.0.1:8080"
	workspaceAddr = "127.0.0.1:8081"
)

// The settings the operator renders into the guest's environment. They
// are addresses, identifiers and one file path: a credential is a file
// below CredentialsDir, never a value here.
const (
	envAgent         = "GUEST_AGENT_GRN"
	envEpoch         = "GUEST_ASSIGNMENT_EPOCH"
	envMachineURL    = "GUEST_MACHINE_URL"
	envControlURL    = "GUEST_CONTROL_URL"
	envRuntimeConfig = "GUEST_RUNTIME_CONFIG"
)

// inherited are the variables a program takes from the supervisor's
// environment, as the images set them.
var inherited = []string{"PATH", "SSL_CERT_FILE", "SHERLOCK_TOOLS_DIR"}

// FromEnv reads one generation's settings and returns it with the three
// programs in start order: runtime, workspace, adapter.
func FromEnv(getenv func(string) string, stdout, stderr io.Writer, log *slog.Logger) (Config, error) {
	agent, err := parseAgent(getenv(envAgent))
	if err != nil {
		return Config{}, err
	}
	epoch := getenv(envEpoch)
	if n, err := strconv.ParseUint(epoch, 10, 63); err != nil || strconv.FormatUint(n, 10) != epoch {
		return Config{}, errorx.New(errorx.KindInvalidArgument, envEpoch+" is not a canonical decimal string")
	}
	machineURL, controlURL := getenv(envMachineURL), getenv(envControlURL)
	if machineURL == "" {
		return Config{}, errorx.New(errorx.KindInvalidArgument, envMachineURL+" is required")
	}
	if controlURL == "" {
		return Config{}, errorx.New(errorx.KindInvalidArgument, envControlURL+" is required")
	}

	var base []string
	for _, k := range inherited {
		if v := getenv(k); v != "" {
			base = append(base, k+"="+v)
		}
	}
	with := func(extra ...string) []string { return append(append([]string{}, base...), extra...) }
	file := func(name string) string { return CredentialsDir + name }

	runtime := []string{
		"agent", "--agent-id", agent, "--assignment-epoch", epoch,
		"--memory-path", MemoryDir + "/memory.db",
		"--addr", gatewayAddr, "--workspace-addr", workspaceAddr,
	}
	if config := getenv(envRuntimeConfig); config != "" {
		runtime = append(runtime, "--config", config)
	}
	return Config{
		Programs: []Program{
			{Name: "runtime", Path: sherlockPath, Args: runtime, Env: with()},
			{Name: "workspace", Path: sherlockPath, Args: []string{"workspace"}, Env: with(
				"SHERLOCK_WORKSPACE="+WorkspaceDir, fmt.Sprintf("SHERLOCK_EXEC_UID=%d", UID))},
			{Name: "adapter", Path: garamPath, Args: []string{"adapter"}, Env: with(
				"GARAM_ADAPTER_AGENT="+agent,
				"GARAM_ADAPTER_MACHINE_URL="+machineURL,
				"GARAM_ADAPTER_SERVER_ROOT_FILE="+file(GaramRootFile),
				"GARAM_ADAPTER_CONTROL_URL="+controlURL,
				"GARAM_ADAPTER_CONTROL_ROOT_FILE="+file(ControlRootFile),
				"GARAM_ADAPTER_PLACEMENT_TOKEN_FILE="+file(PlacementTokenFile),
				"GARAM_ADAPTER_OUTBOX_DIR="+MemoryDir+"/outbox",
				"GARAM_ADAPTER_TOKEN_FILE="+file(AgentTokenFile),
				"GARAM_ADAPTER_CONTROL_TOKEN_FILE="+file(ControlTokenFile))},
		},
		ReadyAddr: fmt.Sprintf(":%d", ReadyPort),
		Stdout:    stdout,
		Stderr:    stderr,
		Log:       log,
	}, nil
}

// parseAgent returns s when it is a canonical agent GRN.
func parseAgent(s string) (string, error) {
	g, err := grn.Parse(s)
	if err != nil || g.ServiceType != "agent" || g.Locator != "" || g.String() != s {
		return "", errorx.New(errorx.KindInvalidArgument, envAgent+" is not a canonical agent GRN")
	}
	return s, nil
}
