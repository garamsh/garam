package integration_test

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

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

// TestDPoPStore_ANonceIsLiveUntilItExpires holds ADR-0191 decision 5:
// a nonce one store issued is live for any store over the database,
// one never issued is not, nor one past its expiry, and the next issue
// sweeps the expired one.
func TestDPoPStore_ANonceIsLiveUntilItExpires(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	issuer, reader := dpoppg.New(p), dpoppg.New(p)
	run := rand.Text()
	nonce := "nonce-" + run

	require.NoError(t, issuer.IssueNonce(ctx, nonce, dpop.NonceTTL))
	live, err := reader.NonceLive(ctx, nonce)
	require.NoError(t, err)
	assert.True(t, live, "another replica accepts it")
	live, err = reader.NonceLive(ctx, "never-"+run)
	require.NoError(t, err)
	assert.False(t, live, "one never issued")

	_, err = p.Exec(ctx, `UPDATE dpop_nonces SET expires_at = now() - interval '1 second' WHERE nonce = $1`, nonce)
	require.NoError(t, err)
	live, err = reader.NonceLive(ctx, nonce)
	require.NoError(t, err)
	assert.False(t, live, "one past its expiry")
	require.NoError(t, issuer.IssueNonce(ctx, "next-"+run, time.Minute))
	assert.Zero(t, count(t, p, "dpop_nonces", "nonce = $1", nonce), "the next issue swept it")
}
