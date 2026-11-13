package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/gaon-operator/internal/config"
)

var full = map[string]string{
	"GAON_API_URL": "https://gaon.example", "GAON_CA_FILE": "/c/ca",
	"GAON_CLIENT_CERT_FILE": "/c/crt", "GAON_CLIENT_KEY_FILE": "/c/key",
	"GARAM_MACHINE_URL": "https://garam.example", "GARAM_ROOT_FILE": "/c/g",
	"CONTROL_URL": "https://control.example", "CONTROL_ROOT_FILE": "/c/ctl",
	"OPERATOR_KEY_FILE": "/c/op", "OPERATOR_CLIENT_ID": "op1",
	"AGENT_IMAGE":     "registry.example/agent:1",
	"GARAM_TOKEN_URL": "https://garam.example/oauth/token", "CONTROL_GRN": "grn:garam:org1/iam:operator/ctl1",
}

func get(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_ReadsEverySetting(t *testing.T) {
	c, err := config.Load(get(full))

	require.NoError(t, err)
	assert.Equal(t, "https://gaon.example", c.GaonURL)
	assert.Equal(t, "registry.example/agent:1", c.AgentImage)
	assert.Equal(t, "op1", c.OperatorClientID)
	assert.Equal(t, "https://garam.example/oauth/token", c.GaramTokenURL)
	assert.Equal(t, "grn:garam:org1/iam:operator/ctl1", c.ControlGRN)
	assert.Equal(t, config.DefaultHealthAddr, c.HealthAddr)
}

func TestLoad_HealthAddrIsOptionalAndOverridable(t *testing.T) {
	withAddr := map[string]string{"HEALTH_ADDR": "127.0.0.1:9"}
	for k, v := range full {
		withAddr[k] = v
	}
	c, err := config.Load(get(withAddr))
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:9", c.HealthAddr)
}

func TestLoad_TheNewSettingsAreRequired(t *testing.T) {
	for _, k := range []string{"GARAM_TOKEN_URL", "CONTROL_GRN"} {
		partial := map[string]string{}
		for key, v := range full {
			partial[key] = v
		}
		delete(partial, k)
		_, err := config.Load(get(partial))
		require.Error(t, err, k)
		assert.Contains(t, err.Error(), k)
	}
}

func TestLoad_NamesEveryMissingSettingAndNoValue(t *testing.T) {
	partial := map[string]string{}
	for k, v := range full {
		partial[k] = v
	}
	delete(partial, "CONTROL_URL")
	partial["AGENT_IMAGE"] = "  "

	_, err := config.Load(get(partial))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "CONTROL_URL")
	assert.Contains(t, err.Error(), "AGENT_IMAGE")
	assert.NotContains(t, err.Error(), "gaon.example")
}

func TestLoad_TheGuestAddressesDefaultToTheOperatorsOwn(t *testing.T) {
	c, err := config.Load(get(full))
	require.NoError(t, err)
	assert.Equal(t, "https://garam.example", c.GuestMachineURL)
	assert.Equal(t, "https://control.example", c.GuestControlURL)

	with := map[string]string{
		"GUEST_MACHINE_URL": "https://garam.guest", "GUEST_CONTROL_URL": "https://control.guest",
	}
	for k, v := range full {
		with[k] = v
	}
	c, err = config.Load(get(with))
	require.NoError(t, err)
	assert.Equal(t, "https://garam.guest", c.GuestMachineURL)
	assert.Equal(t, "https://control.guest", c.GuestControlURL)
}
