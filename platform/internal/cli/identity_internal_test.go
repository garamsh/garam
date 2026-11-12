package cli

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
)

// TestIdentityIssuer_RefusesTwoIssuersThatDiffer holds ADR-0129 item 3:
// the policy service trusts one issuer, so the identity's issuer and
// the feed's, both set, are the same name or start-up fails.
func TestIdentityIssuer_RefusesTwoIssuersThatDiffer(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	t.Setenv(string(envPolicySigningKeyFile), "")

	t.Setenv(string(envIdentityIssuer), "https://garam.test/api")
	t.Setenv(string(envPolicyTokenIssuer), "https://other.test/api")
	_, err := identityIssuer(nil, logger)
	require.Error(t, err)
	assert.Equal(t, errorx.KindInvalidArgument, errorx.KindOf(err))
	assert.Contains(t, err.Error(), string(envPolicyTokenIssuer))

	t.Setenv(string(envPolicyTokenIssuer), "https://garam.test/api")
	issuer, err := identityIssuer(nil, logger)
	require.NoError(t, err, "the same name is one issuer")
	assert.Nil(t, issuer, "with no key the issuer is off")

	t.Setenv(string(envPolicyTokenIssuer), "")
	_, err = identityIssuer(nil, logger)
	assert.NoError(t, err, "the feed's issuer unset names no second one")
}
