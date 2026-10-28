package integration_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// handoffState is a state of the length the receiving app draws.
var handoffState = strings.Repeat("s", 43)

// signIn stores a browser session for a fresh user and answers its
// token.
func signIn(t *testing.T, ctx context.Context, s *authnpg.SessionStore, users *userpg.Store, name string, ttl time.Duration) (string, grn.GRN) {
	t.Helper()
	g := grnOf("user", name)
	require.NoError(t, users.Create(ctx, user.New(g, name)))
	token, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, token, authn.NewSession(g, ttl)))
	return token, g
}

// handOff issues a handoff from token and redeems it, answering the
// handed-off session's token.
func handOff(t *testing.T, ctx context.Context, s *authnpg.SessionStore, token string) string {
	t.Helper()
	code, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.IssueHandoff(ctx, token, code, handoffState, authn.HandoffTTL))
	next, err := authn.NewToken()
	require.NoError(t, err)
	_, err = s.RedeemHandoff(ctx, code, handoffState, next)
	require.NoError(t, err)
	return next
}

// TestSessionHandoff_RedeemsOnceForASessionEndingWithTheSignIn holds
// what ADR-0131 gives the receiving app: a session of the same person,
// expiring when the sign-in does, bought once per code.
func TestSessionHandoff_RedeemsOnceForASessionEndingWithTheSignIn(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authnpg.NewSessionStore(p)
	site, ada := signIn(t, ctx, s, userpg.New(p), "ada", time.Hour)
	signedIn, err := s.Get(ctx, site)
	require.NoError(t, err)

	code, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.IssueHandoff(ctx, site, code, handoffState, authn.HandoffTTL))
	assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM session_handoffs WHERE code_hash = $1", code),
		"the code is stored as its hash alone")

	console, err := authn.NewToken()
	require.NoError(t, err)
	got, err := s.RedeemHandoff(ctx, code, handoffState, console)
	require.NoError(t, err)
	assert.Equal(t, ada, got.User)
	assert.WithinDuration(t, signedIn.ExpiresAt, got.ExpiresAt, time.Microsecond)
	read, err := s.Get(ctx, console)
	require.NoError(t, err)
	assert.Equal(t, ada, read.User)

	again, err := authn.NewToken()
	require.NoError(t, err)
	_, err = s.RedeemHandoff(ctx, code, handoffState, again)
	assert.ErrorIs(t, err, authn.ErrHandoffNotLive)
	_, err = s.Get(ctx, again)
	assert.ErrorIs(t, err, authn.ErrSessionNotFound)
}

// TestSessionHandoff_ConcurrentRedemptionsBuyOneSession races two
// redemptions of one code: the statement deletes the handoff and
// creates the session together, so one of them is answered.
func TestSessionHandoff_ConcurrentRedemptionsBuyOneSession(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authnpg.NewSessionStore(p)
	site, _ := signIn(t, ctx, s, userpg.New(p), "ada", time.Hour)
	code, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.IssueHandoff(ctx, site, code, handoffState, authn.HandoffTTL))

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next, err := authn.NewToken()
			if err == nil {
				_, err = s.RedeemHandoff(ctx, code, handoffState, next)
			}
			errs[i] = err
		}()
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		} else {
			assert.ErrorIs(t, err, authn.ErrHandoffNotLive)
		}
	}
	assert.Equal(t, 1, won)
}

// TestSessionHandoff_RefusesALapsedCodeAndAnotherState redeems a code
// past its lifetime and a live one with the wrong state; the live code
// redeeming with its own state afterwards is the control.
func TestSessionHandoff_RefusesALapsedCodeAndAnotherState(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authnpg.NewSessionStore(p)
	site, _ := signIn(t, ctx, s, userpg.New(p), "ada", time.Hour)

	lapsed, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.IssueHandoff(ctx, site, lapsed, handoffState, -time.Second))
	_, err = s.RedeemHandoff(ctx, lapsed, handoffState, "next-1")
	assert.ErrorIs(t, err, authn.ErrHandoffNotLive)

	live, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.IssueHandoff(ctx, site, live, handoffState, authn.HandoffTTL))
	_, err = s.RedeemHandoff(ctx, live, strings.Repeat("x", 43), "next-2")
	assert.ErrorIs(t, err, authn.ErrHandoffNotLive)
	_, err = s.RedeemHandoff(ctx, live, handoffState, "next-3")
	assert.NoError(t, err)
}

// TestSessionHandoff_IssuesOnlyFromALiveBrowserSession asks for a code
// on an expired session and on an unknown token, beside a live one.
func TestSessionHandoff_IssuesOnlyFromALiveBrowserSession(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authnpg.NewSessionStore(p)
	users := userpg.New(p)
	stale, _ := signIn(t, ctx, s, users, "ada", -time.Hour)
	live, _ := signIn(t, ctx, s, users, "bob", time.Hour)

	assert.ErrorIs(t, s.IssueHandoff(ctx, stale, "code-1", handoffState, authn.HandoffTTL), authn.ErrSessionNotFound)
	assert.ErrorIs(t, s.IssueHandoff(ctx, "unknown", "code-2", handoffState, authn.HandoffTTL), authn.ErrSessionNotFound)
	assert.NoError(t, s.IssueHandoff(ctx, live, "code-3", handoffState, authn.HandoffTTL))
}

// TestSessionHandoff_SigningOutAnywhereEndsTheSignIn signs out on the
// handed-off session and on the sign-in, each in a sign-in of its own:
// either one ends the sign-in on every host. Another person's sign-in,
// untouched, is the control that the delete is the sign-in's alone.
func TestSessionHandoff_SigningOutAnywhereEndsTheSignIn(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authnpg.NewSessionStore(p)
	users := userpg.New(p)
	other, _ := signIn(t, ctx, s, users, "carol", time.Hour)

	t.Run("on the app the sign-in was handed to", func(t *testing.T) {
		site, _ := signIn(t, ctx, s, users, "ada", time.Hour)
		console := handOff(t, ctx, s, site)
		cowork := handOff(t, ctx, s, console)

		require.NoError(t, s.Delete(ctx, console))
		for _, token := range []string{site, console, cowork} {
			_, err := s.Get(ctx, token)
			assert.ErrorIs(t, err, authn.ErrSessionNotFound)
		}
	})
	t.Run("on the site", func(t *testing.T) {
		site, _ := signIn(t, ctx, s, users, "bob", time.Hour)
		console := handOff(t, ctx, s, site)

		require.NoError(t, s.Delete(ctx, site))
		_, err := s.Get(ctx, console)
		assert.ErrorIs(t, err, authn.ErrSessionNotFound)
	})
	_, err := s.Get(ctx, other)
	assert.NoError(t, err)
}

// TestSessionHandoff_RevokingAHandedOffSessionEndsTheSignIn revokes the
// console's session from the sessions list: the sign-in it came from
// goes with it, or a revoked browser could be handed a session again.
func TestSessionHandoff_RevokingAHandedOffSessionEndsTheSignIn(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authnpg.NewSessionStore(p)
	users := userpg.New(p)
	site, ada := signIn(t, ctx, s, users, "ada", time.Hour)
	other, _ := signIn(t, ctx, s, users, "bob", time.Hour)
	console := handOff(t, ctx, s, site)
	handed, err := s.Get(ctx, console)
	require.NoError(t, err)

	require.NoError(t, s.Revoke(ctx, ada, handed.ID))
	_, err = s.Get(ctx, site)
	assert.ErrorIs(t, err, authn.ErrSessionNotFound)
	_, err = s.Get(ctx, other)
	assert.NoError(t, err)
}
