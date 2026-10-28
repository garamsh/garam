package authn_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/authn"
)

// TestNewDeviceGrant_DrawsAUserCodeOfConsonantsAPersonCanType: the
// code reads as two groups of four from RFC 8628 §6.1's alphabet, and
// the code a person types back normalizes.
func TestNewDeviceGrant_DrawsAUserCodeOfConsonantsAPersonCanType(t *testing.T) {
	a, err := authn.NewDeviceGrant()
	require.NoError(t, err)
	b, err := authn.NewDeviceGrant()
	require.NoError(t, err)
	assert.Regexp(t, `^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`, a.UserCode)
	assert.NotEqual(t, a.DeviceCode, b.DeviceCode)
	_, ok := authn.NormalizeUserCode(a.UserCode)
	assert.True(t, ok)
}

// TestNormalizeUserCode_ReadsACodeAsTypedAndRefusesWhatNoGrantDraws:
// case, the dash and surrounding space do not matter; a vowel or a
// wrong length cannot be a code.
func TestNormalizeUserCode_ReadsACodeAsTypedAndRefusesWhatNoGrantDraws(t *testing.T) {
	for _, typed := range []string{"BCDF-GHJK", "bcdfghjk", " bcdf-GHJK "} {
		code, ok := authn.NormalizeUserCode(typed)
		assert.True(t, ok, typed)
		assert.Equal(t, "BCDFGHJK", code, typed)
	}
	for _, typed := range []string{"ABCD-GHJK", "BCDF-GHJ", "BCDF-GHJKL", "", "BCDF GHJK"} {
		_, ok := authn.NormalizeUserCode(typed)
		assert.False(t, ok, typed)
	}
}
