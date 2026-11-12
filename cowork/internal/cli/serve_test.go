package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformhttp "github.com/garamsh/garam/cowork/internal/platform/http"
	platformpolicy "github.com/garamsh/garam/cowork/internal/platform/policy"
	"github.com/garamsh/garam/cowork/internal/policy"
	"github.com/garamsh/garam/cowork/internal/policy/policytest"
	"github.com/garamsh/garam/lib/errorx"
)

func TestWire_WithNothingConfiguredCoworkAsksThePlatform(t *testing.T) {
	s, err := wire(Config{PlatformURL: "http://platform.invalid"}, nil)

	require.NoError(t, err)
	assert.IsType(t, &platformhttp.Client{}, s.platform)
}

func TestWire_WithAPolicyClientCoworkAsksThePolicyService(t *testing.T) {
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	server := policytest.Serve(t, endpoint)
	cfg := policy.Config{Addr: server.Addr, CAFile: server.CAFile, TokenURL: endpoint.URL, KeyFile: keyFile}
	tokens, err := policy.NewTokens(cfg, time.Now)
	require.NoError(t, err)
	client, err := policy.Dial(cfg, tokens)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	s, err := wire(Config{PlatformURL: "http://platform.invalid", PolicyClient: client}, nil)

	require.NoError(t, err)
	assert.IsType(t, &platformpolicy.Platform{}, s.platform)
}

func TestPolicyConfig_AnAddressWithoutTheRestRefusesToStart(t *testing.T) {
	t.Setenv(envPolicyAddr, "policy.invalid:443")
	t.Setenv(envPolicyCAFile, "")
	t.Setenv(envMachineTokenURL, "")
	t.Setenv(envDeploymentKeyFile, "")

	_, err := policyConfig()

	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

func TestPolicyConfig_NothingSetIsNoPolicyClient(t *testing.T) {
	for _, key := range []string{envPolicyAddr, envPolicyCAFile, envMachineTokenURL, envDeploymentKeyFile} {
		t.Setenv(key, "")
	}

	cfg, err := policyConfig()
	require.NoError(t, err)
	client, err := policyClient(cfg)

	require.NoError(t, err)
	assert.Nil(t, client)
}
