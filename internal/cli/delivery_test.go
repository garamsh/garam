// Internal test package: what the adapter refuses to start without is
// unexported wiring, and no exported surface reaches it.
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
)

// adapterArgs is one set of what the environment names the adapter.
type adapterArgs struct {
	agent, machineURL, gatewayURL, gatewayAgent string
	certFile, keyFile, issuerFile               string
}

func (a adapterArgs) build() error {
	_, err := adapterLoop(a.agent, a.machineURL, a.gatewayURL, a.gatewayAgent, a.certFile, a.keyFile, a.issuerFile)
	return err
}

// adapterKeyMaterial writes what the adapter presents and what it
// verifies garam with. The issuer is the certificate itself: what it
// carries is not what the assembly checks, and one self-signed pair
// answers both.
func adapterKeyMaterial(t *testing.T) (certFile, keyFile, issuerFile string) {
	t.Helper()
	dir := t.TempDir()
	certFile, keyFile = writeTLSKeyPair(t, dir)
	return certFile, keyFile, certFile
}

// TestAdapterLoop_RefusesWhatItCannotRunOn covers what the process will
// not start without. The working set is the control: each case below
// changes one value of it, so what refuses is that value rather than
// anything beside it.
func TestAdapterLoop_RefusesWhatItCannotRunOn(t *testing.T) {
	certFile, keyFile, issuerFile := adapterKeyMaterial(t)
	working := adapterArgs{
		agent:        machineGRN("acme", "bot-1").String(),
		machineURL:   "https://garam.example:8443",
		gatewayURL:   defaultGatewayURL,
		gatewayAgent: "default",
		certFile:     certFile,
		keyFile:      keyFile,
		issuerFile:   issuerFile,
	}
	require.NoError(t, working.build(), "everything the loop needs")

	tests := map[string]func(adapterArgs) adapterArgs{
		"no agent":                             func(a adapterArgs) adapterArgs { a.agent = ""; return a },
		"an agent that is not a GRN":           func(a adapterArgs) adapterArgs { a.agent = "bot-1"; return a },
		"no machine listener":                  func(a adapterArgs) adapterArgs { a.machineURL = ""; return a },
		"a machine listener that is not a URL": func(a adapterArgs) adapterArgs { a.machineURL = "garam.example"; return a },
		"no gateway address":                   func(a adapterArgs) adapterArgs { a.gatewayURL = ""; return a },
		"no gateway agent":                     func(a adapterArgs) adapterArgs { a.gatewayAgent = ""; return a },
		"no certificate":                       func(a adapterArgs) adapterArgs { a.certFile = ""; return a },
		"no private key":                       func(a adapterArgs) adapterArgs { a.keyFile = ""; return a },
		"no issuer":                            func(a adapterArgs) adapterArgs { a.issuerFile = ""; return a },
		"an issuer that is not one":            func(a adapterArgs) adapterArgs { a.issuerFile = a.keyFile; return a },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			err := change(working).build()
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
	certFile, keyFile, issuerFile := adapterKeyMaterial(t)

	_, err := adapterClient(certFile, keyFile, issuerFile)
	require.NoError(t, err, "owner-only key")

	require.NoError(t, os.Chmod(keyFile, 0o640))
	_, err = adapterClient(certFile, keyFile, issuerFile)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// TestAdapterClient_RefusesKeyMaterialThatIsNotThere covers the file
// that was configured and never arrived, which a deployment gets wrong
// before it gets anything else wrong.
func TestAdapterClient_RefusesKeyMaterialThatIsNotThere(t *testing.T) {
	certFile, keyFile, issuerFile := adapterKeyMaterial(t)
	absent := filepath.Join(t.TempDir(), "absent.pem")

	for name, args := range map[string][3]string{
		"a certificate that is not there": {absent, keyFile, issuerFile},
		"a key that is not there":         {certFile, absent, issuerFile},
		"an issuer that is not there":     {certFile, keyFile, absent},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := adapterClient(args[0], args[1], args[2])
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

// TestRootCommand_CarriesTheAdapter is the command tree as ADR-0003
// keeps it: one CLI, and the adapter is a subcommand of it rather than
// a binary of its own.
func TestRootCommand_CarriesTheAdapter(t *testing.T) {
	adapter, _, err := NewRootCommand().Find([]string{"adapter"})
	require.NoError(t, err)
	assert.Equal(t, "adapter", adapter.Name())
}
