package cli_test

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// adapterEnv is what the environment names the adapter, by variable.
type adapterEnv map[string]string

// adapterVariables is every variable the adapter reads, so a run sets
// each of them and inherits none.
var adapterVariables = []string{
	"GARAM_ADAPTER_AGENT", "GARAM_ADAPTER_MACHINE_URL", "GARAM_ADAPTER_GATEWAY_URL",
	"GARAM_ADAPTER_TLS_CERT_FILE", "GARAM_ADAPTER_TLS_KEY_FILE", "GARAM_ADAPTER_TOKEN_FILE",
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

// adapterKeyMaterial writes what the adapter presents and the garam
// server root it verifies garam by (ADR-0040), which is a certificate
// of its own rather than the authority that signed the agent.
func adapterKeyMaterial(t *testing.T) (certFile, keyFile, serverRootFile string) {
	t.Helper()
	dir := t.TempDir()
	certFile, keyFile = writeTLSKeyPair(t, dir)
	return certFile, keyFile, writeServerRoot(t, dir)
}

// certificateAdapter is a certificate-mode adapter beside its lifecycle
// authority: the three control settings and an outbox.
func certificateAdapter(t *testing.T, certFile, keyFile, serverRootFile string) adapterEnv {
	t.Helper()
	return adapterEnv{
		"GARAM_ADAPTER_AGENT":                grn.New("acme", grn.DefaultProject, "agent", "bot-1").String(),
		"GARAM_ADAPTER_MACHINE_URL":          "https://garam.example:8443",
		"GARAM_ADAPTER_TLS_CERT_FILE":        certFile,
		"GARAM_ADAPTER_TLS_KEY_FILE":         keyFile,
		"GARAM_ADAPTER_SERVER_ROOT_FILE":     serverRootFile,
		"GARAM_ADAPTER_CONTROL_URL":          "https://control.example:8443",
		"GARAM_ADAPTER_CONTROL_ROOT_FILE":    serverRootFile,
		"GARAM_ADAPTER_PLACEMENT_TOKEN_FILE": filepath.Join(t.TempDir(), "token"),
		"GARAM_ADAPTER_OUTBOX_DIR":           t.TempDir(),
	}
}

// TestAdapterLoop_RefusesWhatItCannotRunOn covers what the process will
// not start without, in either credential mode ADR-0100 lets the
// deployment choose; every adapter runs beside its lifecycle authority
// (ADR-0084, ADR-0118). The working sets are the controls: each case
// below changes one value of one of them, so what refuses is that value
// rather than anything beside it.
func TestAdapterLoop_RefusesWhatItCannotRunOn(t *testing.T) {
	certFile, keyFile, serverRootFile := adapterKeyMaterial(t)
	certificates := certificateAdapter(t, certFile, keyFile, serverRootFile)
	require.NoError(t, certificates.start(t), "the certificate control: all three control settings and an outbox")
	onSocket := certificates.with("GARAM_ADAPTER_CONTROL_SOCKET", filepath.Join(t.TempDir(), "control.sock"))
	require.NoError(t, onSocket.start(t), "an adapter dials control on a socket where one is named")
	tokenFile := filepath.Join(t.TempDir(), "agent-token")
	controlToken := filepath.Join(t.TempDir(), "control-token")
	tokens := certificates.with("GARAM_ADAPTER_TLS_CERT_FILE", "").with("GARAM_ADAPTER_TLS_KEY_FILE", "").
		with("GARAM_ADAPTER_TOKEN_FILE", tokenFile).with("GARAM_ADAPTER_CONTROL_TOKEN_FILE", controlToken)
	require.NoError(t, tokens.start(t), "the token control: both token files beside the control settings")

	tests := map[string]adapterEnv{
		"no agent":                             certificates.with("GARAM_ADAPTER_AGENT", ""),
		"an agent that is not a GRN":           certificates.with("GARAM_ADAPTER_AGENT", "bot-1"),
		"no machine listener":                  certificates.with("GARAM_ADAPTER_MACHINE_URL", ""),
		"a machine listener that is not a URL": certificates.with("GARAM_ADAPTER_MACHINE_URL", "garam.example"),
		"no certificate":                       certificates.with("GARAM_ADAPTER_TLS_CERT_FILE", ""),
		"no private key":                       certificates.with("GARAM_ADAPTER_TLS_KEY_FILE", ""),
		"no server root":                       certificates.with("GARAM_ADAPTER_SERVER_ROOT_FILE", ""),
		"a server root that is not one":        certificates.with("GARAM_ADAPTER_SERVER_ROOT_FILE", keyFile),
		"no control settings at all": certificates.with("GARAM_ADAPTER_CONTROL_URL", "").
			with("GARAM_ADAPTER_CONTROL_ROOT_FILE", "").with("GARAM_ADAPTER_PLACEMENT_TOKEN_FILE", ""),
		"no control address":                     certificates.with("GARAM_ADAPTER_CONTROL_URL", ""),
		"no control root":                        certificates.with("GARAM_ADAPTER_CONTROL_ROOT_FILE", ""),
		"no placement token":                     certificates.with("GARAM_ADAPTER_PLACEMENT_TOKEN_FILE", ""),
		"no outbox beside the control settings":  certificates.with("GARAM_ADAPTER_OUTBOX_DIR", ""),
		"a control address that is not https":    certificates.with("GARAM_ADAPTER_CONTROL_URL", "http://control.example"),
		"a control root that is not one":         certificates.with("GARAM_ADAPTER_CONTROL_ROOT_FILE", keyFile),
		"a token file beside the certificate":    certificates.with("GARAM_ADAPTER_TOKEN_FILE", tokenFile),
		"a token file beside the key alone":      tokens.with("GARAM_ADAPTER_TLS_KEY_FILE", keyFile),
		"a control token beside the certificate": certificates.with("GARAM_ADAPTER_CONTROL_TOKEN_FILE", controlToken),
		"no control token beside the token file": tokens.with("GARAM_ADAPTER_CONTROL_TOKEN_FILE", ""),
		"no credential at all":                   tokens.with("GARAM_ADAPTER_TOKEN_FILE", "").with("GARAM_ADAPTER_CONTROL_TOKEN_FILE", ""),
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			err := env.start(t)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "want an invalid-argument kind, got %v", err)
		})
	}
}

// TestAdapterClient_RefusesAGroupReadableKey holds the agent's own key
// to the rule ADR-0015 states for key material: the owner-only pair
// loads and the same pair with a group-readable key does not, so what
// refuses is the mode rather than anything around it.
func TestAdapterClient_RefusesAGroupReadableKey(t *testing.T) {
	certFile, keyFile, serverRootFile := adapterKeyMaterial(t)
	adapter := certificateAdapter(t, certFile, keyFile, serverRootFile)

	require.NoError(t, adapter.start(t), "owner-only key")

	require.NoError(t, os.Chmod(keyFile, 0o640))
	err := adapter.start(t)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// TestAdapterClient_RefusesKeyMaterialThatIsNotThere covers the file
// that was configured and never arrived, which a deployment gets wrong
// before it gets anything else wrong. The material that is all there is
// the control: each case below replaces one file of it with a path
// nothing wrote, so what refuses is that file rather than anything
// beside it.
func TestAdapterClient_RefusesKeyMaterialThatIsNotThere(t *testing.T) {
	certFile, keyFile, serverRootFile := adapterKeyMaterial(t)
	absent := filepath.Join(t.TempDir(), "absent.pem")
	adapter := certificateAdapter(t, certFile, keyFile, serverRootFile)

	require.NoError(t, adapter.start(t), "every file where it was configured")

	for name, env := range map[string]adapterEnv{
		"a certificate that is not there": adapter.with("GARAM_ADAPTER_TLS_CERT_FILE", absent),
		"a key that is not there":         adapter.with("GARAM_ADAPTER_TLS_KEY_FILE", absent),
		"a server root that is not there": adapter.with("GARAM_ADAPTER_SERVER_ROOT_FILE", absent),
	} {
		t.Run(name, func(t *testing.T) {
			err := env.start(t)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

// TestRootCommand_CarriesTheAdapter is the command tree as ADR-0003
// keeps it: one CLI, and the adapter is a subcommand of it rather than
// a binary of its own.
func TestRootCommand_CarriesTheAdapter(t *testing.T) {
	adapter, _, err := cli.NewRootCommand().Find([]string{"adapter"})
	require.NoError(t, err)
	assert.Equal(t, "adapter", adapter.Name())
}
