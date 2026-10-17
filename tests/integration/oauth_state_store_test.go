package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authn"
)

// TestOAuthStateStore_RoundTrip drives a login state through the shape
// the callback rests on: live once, spent by the callback that
// completes, and gone when its flow has run out. The first consume is
// the control for the second — the refusals below are the state having
// been spent or lapsed, not a store that refuses everything.
func TestOAuthStateStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authn.NewPostgresOAuthStateStore(p)

	state, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, state, time.Minute, "/console/invitations/tok-1"))
	returnPath, err := s.Consume(ctx, state)
	require.NoError(t, err)
	assert.Equal(t, "/console/invitations/tok-1", returnPath, "the path is spent with its state")

	_, err = s.Consume(ctx, state)
	assert.ErrorIs(t, err, authn.ErrOAuthStateNotLive, "a replay reads neither the state nor its path")
	_, err = s.Consume(ctx, "never-issued")
	assert.ErrorIs(t, err, authn.ErrOAuthStateNotLive)

	// A state issued with no path answers none.
	bare, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, bare, time.Minute, ""))
	returnPath, err = s.Consume(ctx, bare)
	require.NoError(t, err)
	assert.Empty(t, returnPath)

	// The schema holds the rule too: a row naming a path off the
	// console is refused where nothing but the surface writes it.
	offConsole, err := authn.NewToken()
	require.NoError(t, err)
	assert.Error(t, s.Issue(ctx, offConsole, time.Minute, "//evil.example/console"))

	// A state past its lifetime is not live, whether or not the row
	// naming it is still there.
	lapsed, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, lapsed, -time.Minute, "/console"))
	_, err = s.Consume(ctx, lapsed)
	assert.ErrorIs(t, err, authn.ErrOAuthStateNotLive, "a path expires with its state")

	// Issuing prunes what has lapsed, which is what bounds the table:
	// the row above survives its own issue and not the next one.
	fresh, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, fresh, time.Minute, ""))
	var rows int
	require.NoError(t, p.QueryRow(ctx,
		"SELECT count(*) FROM oauth_states WHERE state_hash = $1", authn.HashToken(lapsed),
	).Scan(&rows))
	assert.Equal(t, 0, rows, "the lapsed state was pruned")
}

// TestOAuthStateStore_IssueBoundsTheLiveStates holds that issuing
// refuses past MaxLiveOAuthStates live states, which is what bounds
// the write rate an unauthenticated caller can drive on
// /auth/login/{provider}: each state lives at most one flow lifetime,
// so the table — and the writes that fill it — stop at the bound
// however fast the requests arrive.
func TestOAuthStateStore_IssueBoundsTheLiveStates(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authn.NewPostgresOAuthStateStore(p)

	// One row short of the bound, seeded in bulk so the test pays no
	// round trip per row.
	_, err := p.Exec(ctx, `INSERT INTO oauth_states (state_hash, expires_at)
		SELECT 'seed-' || g, now() + interval '10 minutes'
		FROM generate_series(1, $1) g`, authn.MaxLiveOAuthStates-1)
	require.NoError(t, err)

	// The write that reaches the bound is admitted, and the one past
	// it is refused: the table holds exactly the bound, which is the
	// property the rate limit is read out of.
	seeded, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, seeded, time.Minute, ""))
	fresh, err := authn.NewToken()
	require.NoError(t, err)
	assert.ErrorIs(t, s.Issue(ctx, fresh, time.Minute, ""), authn.ErrOAuthStateRateLimited)
	assert.Equal(t, authn.MaxLiveOAuthStates, countRows(t, p,
		"SELECT count(*) FROM oauth_states"))

	// The control: spending a state admits one more, so the refusal
	// above is the bound on live states and not a store that has
	// stopped writing.
	_, err = s.Consume(ctx, seeded)
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, fresh, time.Minute, ""))

	// A refused issue still prunes: an expired row lying on a full
	// table is swept by the issue that refuses, so shedding load
	// cannot wedge the table.
	_, err = p.Exec(ctx, `INSERT INTO oauth_states (state_hash, expires_at)
		VALUES ('lapsed', now() - interval '1 minute')`)
	require.NoError(t, err)
	another, err := authn.NewToken()
	require.NoError(t, err)
	assert.ErrorIs(t, s.Issue(ctx, another, time.Minute, ""), authn.ErrOAuthStateRateLimited)
	assert.Equal(t, 0, countRows(t, p,
		"SELECT count(*) FROM oauth_states WHERE state_hash = 'lapsed'"),
		"the refused issue still swept the lapsed row")
}
