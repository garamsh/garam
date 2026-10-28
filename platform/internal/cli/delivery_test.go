package cli_test

import (
	"context"
	"maps"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cli"
)

// adapterEnv is what the environment names the adapter, by variable.
type adapterEnv map[string]string

// adapterVariables is every variable the adapter reads, so a run sets
// each of them and inherits none.
var adapterVariables = []string{
	"GARAM_ADAPTER_AGENT", "GARAM_ADAPTER_MACHINE_URL", "GARAM_ADAPTER_GATEWAY_URL",
	"GARAM_ADAPTER_TOKEN_FILE",
	"GARAM_ADAPTER_CONTROL_TOKEN_FILE", "GARAM_ADAPTER_SERVER_ROOT_FILE", "GARAM_ADAPTER_CONTROL_URL",
	"GARAM_ADAPTER_CONTROL_ROOT_FILE", "GARAM_ADAPTER_PLACEMENT_TOKEN_FILE", "GARAM_ADAPTER_OUTBOX_DIR",
	"GARAM_ADAPTER_CONTROL_SOCKET", "GARAM_ADAPTER_SIGN_MESSAGES",
}

// with is a copy of e with name set to value; an empty value unsets it.
func (e adapterEnv) with(name, value string) adapterEnv {
	out := maps.Clone(e)
	out[name] = value
	return out
}

// start runs `garam adapter` over e under a context already done, so a
// start nothing refuses returns at once, as a requested stop does.
func (e adapterEnv) start(t *testing.T) error {
	t.Helper()
	for _, name := range adapterVariables {
		t.Setenv(name, e[name])
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runRootContext(ctx, t, "adapter")
	return err
}

// adapterKeyMaterial writes the garam server root the adapter verifies
// garam by (ADR-0040), and beside it a file that is not a root.
func adapterKeyMaterial(t *testing.T) (serverRootFile, notARoot string) {
	t.Helper()
	dir := t.TempDir()
	_, keyFile := writeTLSKeyPair(t, dir)
	return writeServerRoot(t, dir), keyFile
}

// tokenAdapter is an adapter beside its lifecycle authority: its two
// token files, the three control settings and an outbox.
func tokenAdapter(t *testing.T, serverRootFile string) adapterEnv {
	t.Helper()
	return adapterEnv{
		"GARAM_ADAPTER_AGENT":                grn.New("acme", grn.DefaultProject, "agent", "bot-1").String(),
		"GARAM_ADAPTER_MACHINE_URL":          "https://garam.example:8443",
		"GARAM_ADAPTER_TOKEN_FILE":           filepath.Join(t.TempDir(), "agent-token"),
		"GARAM_ADAPTER_CONTROL_TOKEN_FILE":   filepath.Join(t.TempDir(), "control-token"),
		"GARAM_ADAPTER_SERVER_ROOT_FILE":     serverRootFile,
		"GARAM_ADAPTER_CONTROL_URL":          "https://control.example:8443",
		"GARAM_ADAPTER_CONTROL_ROOT_FILE":    serverRootFile,
		"GARAM_ADAPTER_PLACEMENT_TOKEN_FILE": filepath.Join(t.TempDir(), "token"),
		"GARAM_ADAPTER_OUTBOX_DIR":           t.TempDir(),
	}
}

// TestAdapterLoop_RefusesWhatItCannotRunOn covers what the process will
// not start without: its two token files (ADR-0100, ADR-0118), and its
// lifecycle authority beside it (ADR-0084). The working set is the
// control: each case below changes one value of it, so what refuses is
// that value rather than anything beside it.
func TestAdapterLoop_RefusesWhatItCannotRunOn(t *testing.T) {
	serverRootFile, notARoot := adapterKeyMaterial(t)
	tokens := tokenAdapter(t, serverRootFile)
	require.NoError(t, tokens.start(t), "the control: both token files beside the control settings")
	onSocket := tokens.with("GARAM_ADAPTER_CONTROL_SOCKET", filepath.Join(t.TempDir(), "control.sock"))
	require.NoError(t, onSocket.start(t), "an adapter dials control on a socket where one is named")

	tests := map[string]adapterEnv{
		"no agent":                             tokens.with("GARAM_ADAPTER_AGENT", ""),
		"an agent that is not a GRN":           tokens.with("GARAM_ADAPTER_AGENT", "bot-1"),
		"no machine listener":                  tokens.with("GARAM_ADAPTER_MACHINE_URL", ""),
		"a machine listener that is not a URL": tokens.with("GARAM_ADAPTER_MACHINE_URL", "garam.example"),
		"no server root":                       tokens.with("GARAM_ADAPTER_SERVER_ROOT_FILE", ""),
		"a server root that is not one":        tokens.with("GARAM_ADAPTER_SERVER_ROOT_FILE", notARoot),
		"no control settings at all": tokens.with("GARAM_ADAPTER_CONTROL_URL", "").
			with("GARAM_ADAPTER_CONTROL_ROOT_FILE", "").with("GARAM_ADAPTER_PLACEMENT_TOKEN_FILE", ""),
		"no control address":                    tokens.with("GARAM_ADAPTER_CONTROL_URL", ""),
		"no control root":                       tokens.with("GARAM_ADAPTER_CONTROL_ROOT_FILE", ""),
		"no placement token":                    tokens.with("GARAM_ADAPTER_PLACEMENT_TOKEN_FILE", ""),
		"no outbox beside the control settings": tokens.with("GARAM_ADAPTER_OUTBOX_DIR", ""),
		"a control address that is not https":   tokens.with("GARAM_ADAPTER_CONTROL_URL", "http://control.example"),
		"a control root that is not one":        tokens.with("GARAM_ADAPTER_CONTROL_ROOT_FILE", notARoot),
		"no agent token beside the control one": tokens.with("GARAM_ADAPTER_TOKEN_FILE", ""),
		"no control token beside the agent one": tokens.with("GARAM_ADAPTER_CONTROL_TOKEN_FILE", ""),
		"no credential at all":                  tokens.with("GARAM_ADAPTER_TOKEN_FILE", "").with("GARAM_ADAPTER_CONTROL_TOKEN_FILE", ""),
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			err := env.start(t)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "want an invalid-argument kind, got %v", err)
		})
	}
}

// TestAdapterClient_RefusesKeyMaterialThatIsNotThere covers the server
// root that was configured and never arrived, which a deployment gets
// wrong before it gets anything else wrong. The root that is there is
// the control.
func TestAdapterClient_RefusesKeyMaterialThatIsNotThere(t *testing.T) {
	serverRootFile, _ := adapterKeyMaterial(t)
	adapter := tokenAdapter(t, serverRootFile)
	require.NoError(t, adapter.start(t), "the control: the root where it was configured")

	err := adapter.with("GARAM_ADAPTER_SERVER_ROOT_FILE", filepath.Join(t.TempDir(), "absent.pem")).start(t)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// TestRootCommand_CarriesTheAdapter is the command tree as ADR-0003
// keeps it: one CLI, and the adapter is a subcommand of it rather than
// a binary of its own.
func TestRootCommand_CarriesTheAdapter(t *testing.T) {
	adapter, _, err := cli.NewRootCommand().Find([]string{"adapter"})
	require.NoError(t, err)
	assert.Equal(t, "adapter", adapter.Name())
}
