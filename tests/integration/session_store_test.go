package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authn"
	authnpg "github.com/garamsh/garam/internal/authn/postgres"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
)

func TestSessionStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	users := userpg.New(p)
	s := authnpg.NewSessionStore(p)
	userGRN := grnOf("user", "ada")
	require.NoError(t, users.Create(ctx, user.New(userGRN, "ada")))

	token, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, token, authn.NewSession(userGRN, time.Hour)))

	got, err := s.Get(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, userGRN, got.User)

	_, err = s.Get(ctx, "unknown-token")
	assert.ErrorIs(t, err, authn.ErrSessionNotFound)

	// Expired sessions behave as absent.
	stale, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, stale, authn.NewSession(userGRN, -time.Hour)))
	_, err = s.Get(ctx, stale)
	assert.ErrorIs(t, err, authn.ErrSessionNotFound)

	require.NoError(t, s.Delete(ctx, token))
	_, err = s.Get(ctx, token)
	assert.ErrorIs(t, err, authn.ErrSessionNotFound)
	// Delete is idempotent.
	require.NoError(t, s.Delete(ctx, token))
}

// TestSessionStore_CreatePrunesExpired holds that an expired session's
// row is removed rather than merely read as absent, which is what bounds
// the table across every sign-in ever made. The row is read from the
// table, because Get declines an expired session either way.
//
// The standing session is the control, and it is created before the
// sweeping call rather than by it: a session the same statement inserts
// is out of reach of that statement's delete, so it would survive a
// sweep that took the whole table with it and prove nothing.
func TestSessionStore_CreatePrunesExpired(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	users := userpg.New(p)
	s := authnpg.NewSessionStore(p)
	userGRN := grnOf("user", "ada")
	require.NoError(t, users.Create(ctx, user.New(userGRN, "ada")))

	standing, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, standing, authn.NewSession(userGRN, time.Hour)))

	stale, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, stale, authn.NewSession(userGRN, -time.Hour)))
	// The row survives its own create; the next one is what removes it.
	require.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", authn.HashToken(stale)))

	fresh, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, fresh, authn.NewSession(userGRN, time.Hour)))

	assert.Equal(t, 0, countRows(t, p,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", authn.HashToken(stale)),
		"the expired session was pruned")
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", authn.HashToken(standing)),
		"a session that is still live outlives the sweep")
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", authn.HashToken(fresh)))
}
