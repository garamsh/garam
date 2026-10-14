package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// indexScanPool is a pool over the test database whose sessions plan
// every token delete as an index scan, the plan a table of production
// size gets. A table of a test's size is scanned sequentially, in one
// order for every statement, and two statements walking one order never
// lock two rows in opposite orders, so the interleavings below need the
// index orders to exist at all. Each test also asserts the plans it
// rests on.
func indexScanPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(os.Getenv("GARAM_TEST_DATABASE_URL"))
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["enable_seqscan"] = "off"
	cfg.ConnConfig.RuntimeParams["enable_bitmapscan"] = "off"
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	return p
}

// requirePlanUses fails unless p plans query, a token delete, as a
// scan of index.
func requirePlanUses(t *testing.T, p *pgxpool.Pool, index, query string, args ...any) {
	t.Helper()
	rows, err := p.Query(context.Background(), "EXPLAIN "+query, args...)
	require.NoError(t, err)
	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, strings.Join(plan, "\n"), "Index Scan using "+index, "the plan the interleaving rests on")
}

// requireHeapOrder fails unless the tokens sit in the heap in the order
// given, which is the order an index scan over one equal key visits
// them.
func requireHeapOrder(t *testing.T, p *pgxpool.Pool, hashes ...string) {
	t.Helper()
	for i := 1; i < len(hashes); i++ {
		var before bool
		require.NoError(t, p.QueryRow(context.Background(),
			`SELECT (SELECT ctid FROM operator_tokens WHERE token_hash = $1) <
			        (SELECT ctid FROM operator_tokens WHERE token_hash = $2)`,
			hashes[i-1], hashes[i]).Scan(&before))
		require.True(t, before, "%s sits before %s in the heap", hashes[i-1], hashes[i])
	}
}

// insertToken records a token row for subject under the credential or
// the key named, expiring at expiresAt, an SQL interval from now.
func insertToken(t *testing.T, p *pgxpool.Pool, hash, column string, id int64, subject grn.GRN, expiresAt string) {
	t.Helper()
	_, err := p.Exec(context.Background(), `INSERT INTO operator_tokens (token_hash, `+column+`, operator_grn, issued_at, expires_at)
		VALUES ($1, $2, $3, now() - interval '4 hours', now() + $4::interval)`, hash, id, subject.String(), expiresAt)
	require.NoError(t, err)
}

// credentialID answers the id of subject's credential.
func credentialID(t *testing.T, p *pgxpool.Pool, subject grn.GRN) int64 {
	t.Helper()
	var id int64
	require.NoError(t, p.QueryRow(context.Background(),
		`SELECT id FROM operator_credentials WHERE operator_grn = $1`, subject.String()).Scan(&id))
	return id
}

// parkOn begins a transaction holding the named tokens' rows, which a
// delete reaching one of them waits on, and answers it.
func parkOn(t *testing.T, p *pgxpool.Pool, hashes ...string) pgx.Tx {
	t.Helper()
	tx, err := p.Begin(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	_, err = tx.Exec(context.Background(),
		`SELECT 1 FROM operator_tokens WHERE token_hash = ANY($1) FOR UPDATE`, hashes)
	require.NoError(t, err)
	return tx
}

// TestOperatorLockOrder_AKeyWithdrawalAndAnExchangeSweepingItsTokensBothComplete
// is #1418's first ordering made certain rather than raced for. The
// withdrawal deletes the key's tokens in key order; a credential
// exchange sweeps the operator's expired tokens in expiry order; two
// expired tokens of the key sit in opposite orders in the two. A third
// transaction parks each delete between them, on a row only that delete
// reaches, so on its release each needed the token the other held and
// Postgres aborted one with 40P01. Holding the registration first, the
// withdrawal makes the exchange wait for it before it deletes anything.
func TestOperatorLockOrder_AKeyWithdrawalAndAnExchangeSweepingItsTokensBothComplete(t *testing.T) {
	ctx := context.Background()
	pool(t)
	p := indexScanPool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := exchangeService(store)
	subject, credential := registerCredentialed(t, store, "controller")
	_, key := issueKey(t, store, subject, operator.KeyRotationFirst)
	// Key order is the heap order: late, park-withdrawal, early. Expiry
	// order is early, park-exchange, late.
	insertToken(t, p, "late", "key_id", key.ID, subject, "-1 hour")
	insertToken(t, p, "park-withdrawal", "key_id", key.ID, subject, "1 hour")
	insertToken(t, p, "early", "key_id", key.ID, subject, "-3 hours")
	insertToken(t, p, "park-exchange", "credential_id", credentialID(t, p, subject), subject, "-2 hours")
	requireHeapOrder(t, p, "late", "park-withdrawal", "early")
	requirePlanUses(t, p, "operator_tokens_key_id_idx",
		`DELETE FROM operator_tokens WHERE key_id = ANY($1::bigint[])`, []int64{key.ID})
	requirePlanUses(t, p, "operator_tokens_operator_grn_expires_at_idx",
		`DELETE FROM operator_tokens WHERE operator_grn = $1 AND expires_at <= now()`, subject.String())

	parked := parkOn(t, p, "park-withdrawal", "park-exchange")
	withdrawn := make(chan error, 1)
	go func() { withdrawn <- store.RevokeKey(ctx, subject, key.KID) }()
	awaitLockWaiters(t, p, 1)
	type exchange struct {
		token *operator.Token
		err   error
	}
	exchanged := make(chan exchange, 1)
	go func() {
		token, err := svc.Exchange(ctx, credential, nil)
		exchanged <- exchange{token, err}
	}()
	awaitLockWaiters(t, p, 2)
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, withdrawn), "the withdrawal completes")
	done := receive(t, exchanged)
	require.NoError(t, done.err, "the exchange completes rather than deadlocking")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE key_id = $1`, key.ID))
	_, err := store.ResolveToken(ctx, done.token.Hash())
	assert.NoError(t, err, "the control: the credential's new token resolves")
}

// TestOperatorLockOrder_ACredentialReissueAndADeletionBothComplete is
// #1418's second ordering. A third transaction holding the credential
// parks the re-issue's delete, and the operator's deletion arrives
// then. Without the registration held first, the deletion took the
// registration and waited on the credential, the re-issue took the
// credential and its insert's foreign key waited on the registration,
// and Postgres aborted one with 40P01. Holding it, the deletion waits
// for the re-issue and then removes what it wrote.
func TestOperatorLockOrder_ACredentialReissueAndADeletionBothComplete(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	subject, _ := registerCredentialed(t, store, "controller")
	reissued, err := operator.NewIssuedCredential(subject, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)

	holder, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = holder.Exec(ctx, `SELECT id FROM operator_credentials WHERE operator_grn = $1 FOR UPDATE`, subject.String())
	require.NoError(t, err)

	reissue := make(chan error, 1)
	go func() { reissue <- store.ReissueCredential(ctx, reissued) }()
	awaitLockWaiters(t, p, 1)
	deleted := make(chan error, 1)
	go func() { deleted <- store.Delete(ctx, subject) }()
	awaitLockWaiters(t, p, 2)
	require.NoError(t, holder.Rollback(ctx))

	require.NoError(t, receive(t, reissue), "the re-issue completes")
	require.NoError(t, receive(t, deleted), "the deletion completes rather than deadlocking")
	_, err = store.Get(ctx, subject)
	assert.ErrorIs(t, err, operator.ErrOperatorNotFound, "the deletion came second and removed the registration")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_credentials WHERE operator_grn = $1`, subject.String()),
		"and the credential the re-issue wrote with it")
}

// TestOperatorLockOrder_ACredentialReissueAndAKeyExchangeSweepingItsTokensBothComplete
// is the re-issue's form of the first ordering, found reading #1418's
// writers. The re-issue's cascade deletes the credential's tokens in
// credential order; a key exchange, which never touches the credential,
// sweeps the operator's expired tokens in expiry order. Parked between
// two expired credential tokens as above, each needed the one the other
// held. Holding the registration first, the re-issue makes the exchange
// wait for it.
func TestOperatorLockOrder_ACredentialReissueAndAKeyExchangeSweepingItsTokensBothComplete(t *testing.T) {
	ctx := context.Background()
	pool(t)
	p := indexScanPool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := keyService(store)
	subject, _ := registerCredentialed(t, store, "controller")
	private, key := issueKey(t, store, subject, operator.KeyRotationFirst)
	credential := credentialID(t, p, subject)
	// Credential order is the heap order: late, park-reissue, early.
	// Expiry order is early, park-exchange, late.
	insertToken(t, p, "late", "credential_id", credential, subject, "-1 hour")
	insertToken(t, p, "park-reissue", "credential_id", credential, subject, "1 hour")
	insertToken(t, p, "early", "credential_id", credential, subject, "-3 hours")
	insertToken(t, p, "park-exchange", "key_id", key.ID, subject, "-2 hours")
	requireHeapOrder(t, p, "late", "park-reissue", "early")
	requirePlanUses(t, p, "operator_tokens_credential_id_idx",
		`DELETE FROM operator_tokens WHERE credential_id = $1`, credential)
	requirePlanUses(t, p, "operator_tokens_operator_grn_expires_at_idx",
		`DELETE FROM operator_tokens WHERE operator_grn = $1 AND expires_at <= now()`, subject.String())
	reissued, err := operator.NewIssuedCredential(subject, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)

	parked := parkOn(t, p, "park-reissue", "park-exchange")
	reissue := make(chan error, 1)
	go func() { reissue <- store.ReissueCredential(ctx, reissued) }()
	awaitLockWaiters(t, p, 1)
	type exchange struct {
		token *operator.Token
		err   error
	}
	exchanged := make(chan exchange, 1)
	go func() {
		token, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), nil)
		exchanged <- exchange{token, err}
	}()
	awaitLockWaiters(t, p, 2)
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, reissue), "the re-issue completes")
	done := receive(t, exchanged)
	require.NoError(t, done.err, "the key exchange completes rather than deadlocking")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE credential_id = $1`, credential),
		"the re-issue took the previous credential's tokens with it")
	_, err = store.ResolveToken(ctx, done.token.Hash())
	assert.NoError(t, err, "the control: the key's new token resolves")
}
