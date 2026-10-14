package wire_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// TestStatementToProto renders each field of a statement onto the
// contract, under both effects that must convert, and refuses an
// Effect neither constant names as internal, naming the statement. The
// two converting effects are the control on the refusal: alone, it
// would hold for a converter that refuses everything.
func TestStatementToProto(t *testing.T) {
	action := []string{"message:send"}
	resource := []string{"grn:acme:*"}
	for effect, want := range map[wire.Effect]authzv1.Effect{
		wire.EffectAllow: authzv1.Effect_EFFECT_ALLOW,
		wire.EffectDeny:  authzv1.Effect_EFFECT_DENY,
	} {
		got, err := wire.StatementToProto("machine-own-org", effect, action, resource)
		require.NoError(t, err, "effect %q", effect)
		assert.Equal(t, "machine-own-org", got.GetSid())
		assert.Equal(t, want, got.GetEffect(), "effect %q", effect)
		assert.Equal(t, action, got.GetAction())
		assert.Equal(t, resource, got.GetResource())
	}

	got, err := wire.StatementToProto("org-role-owner", wire.Effect("Maybe"), action, resource)
	require.Error(t, err)
	assert.Nil(t, got)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v", err)
	assert.Contains(t, err.Error(), "statement org-role-owner")
}
