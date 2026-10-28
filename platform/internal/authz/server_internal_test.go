package authz

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/wire"
)

// TestEffectToProto_AgreesWithTheSharedConverter pins the coercing
// copy above to the validating one on every Effect both accept. The
// copy stays because replacing it means choosing what Explain does
// with an unrepresentable Effect, and this test is what drift between
// the two now fails (#287).
func TestEffectToProto_AgreesWithTheSharedConverter(t *testing.T) {
	for _, e := range []wire.Effect{wire.EffectAllow, wire.EffectDeny} {
		want, err := wire.EffectToProto(e)
		require.NoError(t, err)
		assert.Equal(t, want, effectToProto(e), "effect %q", e)
	}
}
