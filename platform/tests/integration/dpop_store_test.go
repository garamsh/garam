package integration_test

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/dpop"
	dpoppg "github.com/garamsh/garam/platform/internal/dpop/postgres"
)

// count answers how many rows of table match where.
func count(t *testing.T, p *pgxpool.Pool, table, where string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, p.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE "+where, args...).Scan(&n))
	return n
}

// TestDPoPStore_AJTIIsSpentOncePerKey holds ADR-0191 decision 4 at the
// store every replica reads: a jti is spent once for its key, the same
// jti under another key is the control, and once a row expires it is
// swept by the next spend, whichever key that is for.
func TestDPoPStore_AJTIIsSpentOncePerKey(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := dpoppg.New(p)
	run := rand.Text()
	jkt, other := "jkt-"+run, "other-"+run

	fresh, err := s.SpendJTI(ctx, jkt, "j1", dpop.SpentFor)
	require.NoError(t, err)
	assert.True(t, fresh, "the first spend")
	fresh, err = s.SpendJTI(ctx, jkt, "j1", dpop.SpentFor)
	require.NoError(t, err)
	assert.False(t, fresh, "the replay")
	fresh, err = s.SpendJTI(ctx, other, "j1", dpop.SpentFor)
	require.NoError(t, err)
	assert.True(t, fresh, "the control: the same jti under another key")

	_, err = p.Exec(ctx, `UPDATE dpop_jtis SET expires_at = now() - interval '1 second' WHERE jkt = $1`, jkt)
	require.NoError(t, err)
	fresh, err = s.SpendJTI(ctx, other, "j2", dpop.SpentFor)
	require.NoError(t, err)
	assert.True(t, fresh)
	assert.Zero(t, count(t, p, "dpop_jtis", "jkt = $1", jkt), "another key's spend swept the expired row")
	assert.Equal(t, 2, count(t, p, "dpop_jtis", "jkt = $1", other), "and left the live ones")
}

// TestDPoPStore_OnePeriodHasOneNonce holds ADR-0191 decision 5: any
// number of asks in a period, from any store over the database, answer
// one nonce and hold one row, whichever candidate each brought; the
// nonce is live in its period and the next, and not after, and the
// next period's first ask sweeps it.
func TestDPoPStore_OnePeriodHasOneNonce(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	_, err := p.Exec(ctx, `DELETE FROM dpop_nonces`)
	require.NoError(t, err)
	issuer, reader := dpoppg.New(p), dpoppg.New(p)

	nonce, err := issuer.CurrentNonce(ctx, "first-"+rand.Text(), dpop.NoncePeriod)
	require.NoError(t, err)
	for range 20 {
		again, err := reader.CurrentNonce(ctx, "later-"+rand.Text(), dpop.NoncePeriod)
		require.NoError(t, err)
		require.Equal(t, nonce, again, "the period's nonce, not the later candidate")
	}
	assert.Equal(t, 1, count(t, p, "dpop_nonces", "true"), "a flood of asks holds one row")
	live, err := reader.NonceLive(ctx, nonce, dpop.NoncePeriod)
	require.NoError(t, err)
	assert.True(t, live, "another replica accepts it")
	live, err = reader.NonceLive(ctx, "never-"+rand.Text(), dpop.NoncePeriod)
	require.NoError(t, err)
	assert.False(t, live, "one never issued")

	_, err = p.Exec(ctx, `UPDATE dpop_nonces SET period = period - 1`)
	require.NoError(t, err)
	live, err = reader.NonceLive(ctx, nonce, dpop.NoncePeriod)
	require.NoError(t, err)
	assert.True(t, live, "the previous period's")
	_, err = p.Exec(ctx, `UPDATE dpop_nonces SET period = period - 1`)
	require.NoError(t, err)
	live, err = reader.NonceLive(ctx, nonce, dpop.NoncePeriod)
	require.NoError(t, err)
	assert.False(t, live, "two periods old")

	next, err := issuer.CurrentNonce(ctx, "next-"+rand.Text(), dpop.NoncePeriod)
	require.NoError(t, err)
	assert.NotEqual(t, nonce, next)
	assert.Equal(t, 1, count(t, p, "dpop_nonces", "true"), "the old period was swept")
}
