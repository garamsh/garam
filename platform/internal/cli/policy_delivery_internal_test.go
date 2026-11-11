package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
)

func setPolicyEnv(t *testing.T, addr, ca, key, issuer string) {
	t.Helper()
	t.Setenv(string(envPolicyAddr), addr)
	t.Setenv(string(envPolicyCAFile), ca)
	t.Setenv(string(envPolicySigningKeyFile), key)
	t.Setenv(string(envPolicyTokenIssuer), issuer)
}

func TestReadPolicyDeliveryConfig_UnsetAddressMeansNoLoop(t *testing.T) {
	// The other names are set: without an address nothing reads them.
	setPolicyEnv(t, "", "/ca.pem", "/key.pem", "iss")

	cfg, err := readPolicyDeliveryConfig()

	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestReadPolicyDeliveryConfig_AnAddressRequiresTheRest(t *testing.T) {
	tests := []struct {
		name                          string
		ca, key, issuer, wantMentions string
	}{
		{"no CA file", "", "/key.pem", "iss", "GARAM_POLICY_CA_FILE"},
		{"no signing key", "/ca.pem", "", "iss", "GARAM_POLICY_SIGNING_KEY_FILE"},
		{"no issuer", "/ca.pem", "/key.pem", "", "GARAM_POLICY_TOKEN_ISSUER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setPolicyEnv(t, "policy:8443", tt.ca, tt.key, tt.issuer)

			cfg, err := readPolicyDeliveryConfig()

			require.Error(t, err)
			assert.Nil(t, cfg)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
			assert.Contains(t, err.Error(), tt.wantMentions)
		})
	}

	t.Run("control: all four set", func(t *testing.T) {
		setPolicyEnv(t, "policy:8443", "/ca.pem", "/key.pem", "iss")

		cfg, err := readPolicyDeliveryConfig()

		require.NoError(t, err)
		assert.Equal(t, &policyDeliveryConfig{
			addr: "policy:8443", caFile: "/ca.pem", keyFile: "/key.pem", issuer: "iss",
		}, cfg)
	})
}
