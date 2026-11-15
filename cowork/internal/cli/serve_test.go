package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformpolicy "github.com/garamsh/garam/cowork/internal/platform/policy"
	"github.com/garamsh/garam/cowork/internal/policy"
	"github.com/garamsh/garam/cowork/internal/policy/policytest"
	"github.com/garamsh/garam/lib/errorx"
)

func TestWire_WithoutAPolicyClientCoworkRefusesToStart(t *testing.T) {
	_, err := wire(Config{PlatformURL: "http://platform.invalid"}, nil)

	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// testPolicyClient is a client of an in-process policy service.
func testPolicyClient(t *testing.T) *policy.Client {
	t.Helper()
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	server := policytest.Serve(t, endpoint)
	cfg := policy.Config{Addr: server.Addr, CAFile: server.CAFile, TokenURL: endpoint.URL, Issuer: endpoint.Issuer, KeyFile: keyFile}
	tokens, err := policy.NewTokens(cfg, time.Now)
	require.NoError(t, err)
	client, err := policy.Dial(cfg, tokens)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestWire_WithAPolicyClientCoworkAsksThePolicyService(t *testing.T) {
	s, err := wire(Config{PlatformURL: "http://platform.invalid", PolicyClient: testPolicyClient(t)}, nil)

	require.NoError(t, err)
	assert.IsType(t, &platformpolicy.Platform{}, s.platform)
}

var policySettings = []string{envPolicyAddr, envPolicyCAFile, envMachineTokenURL, envIdentityIssuer, envDeploymentKeyFile}

func TestPolicyConfig_AllFiveSetStarts(t *testing.T) {
	for _, key := range policySettings {
		t.Setenv(key, key+"-value")
	}

	cfg, err := policyConfig()

	require.NoError(t, err)
	assert.Equal(t, policy.Config{
		Addr: envPolicyAddr + "-value", CAFile: envPolicyCAFile + "-value",
		TokenURL: envMachineTokenURL + "-value", Issuer: envIdentityIssuer + "-value", KeyFile: envDeploymentKeyFile + "-value",
	}, cfg)
}

func TestPolicyConfig_ASettingMissingRefusesToStartAndIsNamed(t *testing.T) {
	for _, missing := range policySettings {
		t.Run(missing, func(t *testing.T) {
			for _, key := range policySettings {
				t.Setenv(key, key+"-value")
			}
			t.Setenv(missing, "")

			_, err := policyConfig()

			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
			for _, key := range policySettings {
				if key == missing {
					assert.Contains(t, err.Error(), key)
				} else {
					assert.NotContains(t, err.Error(), key)
				}
			}
		})
	}
}

func TestPolicyConfig_NothingSetNamesAllFive(t *testing.T) {
	for _, key := range policySettings {
		t.Setenv(key, "")
	}

	_, err := policyConfig()

	require.Error(t, err)
	for _, key := range policySettings {
		assert.Contains(t, err.Error(), key)
	}
}
