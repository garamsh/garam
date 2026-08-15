package authn_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
)

func TestDevProvider_AuthCodeURL(t *testing.T) {
	p, err := authn.NewDevProvider(authn.DevConfig{
		RedirectURL: "http://localhost:3000/api/auth/callback/dev",
	})
	require.NoError(t, err)
	assert.Equal(t, "dev", p.Name())

	u := p.AuthCodeURL("state-1", "verifier-1")
	assert.Contains(t, u, "http://localhost:3000/api/auth/callback/dev?")
	assert.Contains(t, u, "code=dev")
	assert.Contains(t, u, "state=state-1")
}

func TestDevProvider_Exchange(t *testing.T) {
	p, err := authn.NewDevProvider(authn.DevConfig{
		RedirectURL: "http://localhost:3000/api/auth/callback/dev",
	})
	require.NoError(t, err)

	identity, err := p.Exchange(context.Background(), "dev", "")
	require.NoError(t, err)
	assert.Equal(t, "dev", identity.Subject)
	assert.Equal(t, "Dev", identity.Nickname)
}

func TestDevProvider_ExchangeWrongCode(t *testing.T) {
	p, err := authn.NewDevProvider(authn.DevConfig{
		RedirectURL: "http://localhost:3000/api/auth/callback/dev",
	})
	require.NoError(t, err)

	_, err = p.Exchange(context.Background(), "forged", "")
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), fmt.Sprintf("kind: %v", err))
}

func TestNewDevProvider_RejectsRelativeURL(t *testing.T) {
	_, err := authn.NewDevProvider(authn.DevConfig{RedirectURL: "/auth/callback/dev"})
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), fmt.Sprintf("kind: %v", err))
}
