package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/gaon-operator/internal/cli"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRun_RefusesWithoutSettingsAndNamesThem(t *testing.T) {
	var out strings.Builder

	err := cli.Run(t.Context(), env(nil), &out)

	require.Error(t, err)
	assert.Contains(t, out.String(), "GAON_API_URL")
	assert.NotErrorIs(t, err, cli.ErrNotWired)
}

func TestRun_RefusesToStartAndNamesWhatIsNotWired(t *testing.T) {
	all := map[string]string{}
	for _, k := range []string{
		"GAON_API_URL", "GAON_CA_FILE", "GAON_CLIENT_CERT_FILE", "GAON_CLIENT_KEY_FILE",
		"GARAM_MACHINE_URL", "GARAM_ROOT_FILE", "CONTROL_URL", "CONTROL_ROOT_FILE",
		"OPERATOR_KEY_FILE", "OPERATOR_CLIENT_ID", "AGENT_IMAGE",
	} {
		all[k] = "x"
	}
	var out strings.Builder

	err := cli.Run(t.Context(), env(all), &out)

	require.ErrorIs(t, err, cli.ErrNotWired)
	assert.Contains(t, out.String(), "garam machine client")
	assert.Contains(t, out.String(), "control service feed client")
}
