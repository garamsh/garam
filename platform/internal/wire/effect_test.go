package wire_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/wire"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// TestEffectToProto holds the refusal against the two effects that
// must convert: alone, a refusal would hold for a converter that
// refuses everything.
func TestEffectToProto(t *testing.T) {
	recognised := []struct {
		effect wire.Effect
		want   authzv1.Effect
	}{
		{wire.EffectAllow, authzv1.Effect_EFFECT_ALLOW},
		{wire.EffectDeny, authzv1.Effect_EFFECT_DENY},
	}
	for _, c := range recognised {
		got, err := wire.EffectToProto(c.effect)
		require.NoError(t, err, "effect %q", c.effect)
		assert.Equal(t, c.want, got, "effect %q", c.effect)
	}

	got, err := wire.EffectToProto(wire.Effect("Maybe"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v", err)
	assert.Equal(t, authzv1.Effect_EFFECT_UNSPECIFIED, got)
}

// TestEffectFromProto holds the parse refusal against the two wire
// values that must parse, and closes the round trip on both.
func TestEffectFromProto(t *testing.T) {
	recognised := []struct {
		proto authzv1.Effect
		want  wire.Effect
	}{
		{authzv1.Effect_EFFECT_ALLOW, wire.EffectAllow},
		{authzv1.Effect_EFFECT_DENY, wire.EffectDeny},
	}
	for _, c := range recognised {
		got, err := wire.EffectFromProto(c.proto)
		require.NoError(t, err, "proto %v", c.proto)
		assert.Equal(t, c.want, got, "proto %v", c.proto)

		back, err := wire.EffectToProto(got)
		require.NoError(t, err, "proto %v", c.proto)
		assert.Equal(t, c.proto, back, "proto %v", c.proto)
	}

	for _, rejected := range []authzv1.Effect{authzv1.Effect_EFFECT_UNSPECIFIED, authzv1.Effect(99)} {
		got, err := wire.EffectFromProto(rejected)
		require.Error(t, err, "proto %v", rejected)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "proto %v: got %v", rejected, err)
		assert.Empty(t, got, "proto %v", rejected)
	}
}
