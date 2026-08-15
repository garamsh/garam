// Internal test package: the dev-auth flag wiring is a white-box seam
// — the serve command cannot run in a unit test, so the provider
// assembly is exercised directly.
package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
)

func TestDevProvider_Disabled(t *testing.T) {
	p, err := devProvider(false, false)
	require.NoError(t, err)
	assert.Nil(t, p)
}

func TestDevProvider_Enabled(t *testing.T) {
	p, err := devProvider(true, false)
	require.NoError(t, err)
	assert.Equal(t, "dev", p.Name())
}

func TestDevProvider_RefusesSecureCookies(t *testing.T) {
	_, err := devProvider(true, true)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}
