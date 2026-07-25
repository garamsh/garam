package authn_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/authn"
	"github.com/garamsh/gaon/internal/grn"
)

func TestNewToken_Unique(t *testing.T) {
	a, err := authn.NewToken()
	require.NoError(t, err)
	b, err := authn.NewToken()
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
	assert.NotEmpty(t, a)
}

func TestHashToken_Deterministic(t *testing.T) {
	assert.Equal(t, authn.HashToken("tok"), authn.HashToken("tok"))
	assert.NotEqual(t, authn.HashToken("tok"), authn.HashToken("other"))
	assert.NotContains(t, authn.HashToken("tok"), "tok")
}

func TestSession_Expired(t *testing.T) {
	g := grn.New("acme", "default", "user", "ada")
	s := authn.NewSession(g, time.Hour)
	assert.False(t, s.Expired(time.Now()))
	assert.True(t, s.Expired(time.Now().Add(2*time.Hour)))
	assert.Equal(t, g, s.User())
}
