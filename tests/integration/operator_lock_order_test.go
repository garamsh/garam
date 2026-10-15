package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

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
	insertTokenRow(t, p, tokenRow{hash: hash, column: column, id: id, subject: subject, issuedAt: "-4 hours", expiresAt: expiresAt})
}

// tokenRow is a token row a test writes directly: its issuance, its
// subject, the audience it names if any, and when it was issued and
// expires, as SQL intervals from now.
type tokenRow struct {
	hash, column        string
	id                  int64
	subject             grn.GRN
	audience            *grn.GRN
	issuedAt, expiresAt string
}

// insertTokenRow records row.
func insertTokenRow(t *testing.T, p *pgxpool.Pool, row tokenRow) {
	t.Helper()
	var audience *string
	if row.audience != nil {
		a := row.audience.String()
		audience = &a
	}
	_, err := p.Exec(context.Background(), `INSERT INTO operator_tokens (token_hash, `+row.column+`, operator_grn, audience_grn, issued_at, expires_at)
		VALUES ($1, $2, $3, $4, now() + $5::interval, now() + $6::interval)`,
		row.hash, row.id, row.subject.String(), audience, row.issuedAt, row.expiresAt)
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
	pool(t) // migrates and truncates the database; the store runs on p
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
		token, err := svc.Exchange(ctx, testSource, credential, nil)
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
	pool(t) // migrates and truncates the database; the store runs on p
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

// awaitSettled returns once n of the calls under test have each
// returned or are waiting on a lock. A call the order under test keeps
// from waiting returns rather than queueing, so the same steps drive
// the interleaving with that order and without it.
func awaitSettled(t *testing.T, p *pgxpool.Pool, n int, returned ...func() bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		var settled int
		if err := p.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&settled); err != nil {
			return false
		}
		for _, r := range returned {
			if r() {
				settled++
			}
		}
		return settled >= n
	}, 10*time.Second, 10*time.Millisecond)
}

// returned reports whether ch holds a call's answer, without taking it.
func returned[T any](ch chan T) func() bool {
	return func() bool { return len(ch) > 0 }
}

// blockedBy answers the sessions waiting on a lock holder holds.
func blockedBy(t *testing.T, p *pgxpool.Pool, holder int) []int {
	t.Helper()
	rows, err := p.Query(context.Background(),
		`SELECT pid FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, holder)
	require.NoError(t, err)
	pids, err := pgx.CollectRows(rows, pgx.RowTo[int])
	require.NoError(t, err)
	return pids
}

// exchange is what an exchange under test answered.
type exchange struct {
	token *operator.Token
	err   error
}

// TestOperatorLockOrder_AnAudiencesDeletionAndAnExchangeSweepingTokensNamingItBothComplete
// is #1424's audience cascade against an exchange. Deleting the
// audience took the client's tokens naming it in heap order, holding
// only the audience's registration; the client's exchange sweeps its
// expired tokens in expiry order; two expired tokens naming the
// audience sit in opposite orders in the two. Parked between them as
// #1418's tests park, each needed the token the other held. The
// deletion now takes them in token_hash order, and the exchange waits
// on a token only in its trim, holding none.
func TestOperatorLockOrder_AnAudiencesDeletionAndAnExchangeSweepingTokensNamingItBothComplete(t *testing.T) {
	ctx := context.Background()
	pool(t) // migrates and truncates the database; the store runs on p
	p := indexScanPool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := exchangeService(store)
	subject, credential := registerCredentialed(t, store, "controller")
	audience, _ := registerCredentialed(t, store, "hosted")
	issuance := credentialID(t, p, subject)
	// Heap order, which a scan for the audience's tokens takes, is late,
	// park-deletion, early. Expiry order is early, park-exchange, late.
	for _, row := range []tokenRow{
		{hash: "late", audience: &audience, expiresAt: "-1 hour"},
		{hash: "park-deletion", audience: &audience, expiresAt: "1 hour"},
		{hash: "early", audience: &audience, expiresAt: "-3 hours"},
		{hash: "park-exchange", expiresAt: "-2 hours"},
	} {
		row.column, row.id, row.subject, row.issuedAt = "credential_id", issuance, subject, "-4 hours"
		insertTokenRow(t, p, row)
	}
	requireHeapOrder(t, p, "late", "park-deletion", "early")
	requirePlanUses(t, p, "operator_tokens_operator_grn_expires_at_idx",
		`DELETE FROM operator_tokens WHERE operator_grn = $1 AND expires_at <= now()`, subject.String())

	parked := parkOn(t, p, "park-deletion", "park-exchange")
	deleted := make(chan error, 1)
	go func() { deleted <- store.Delete(ctx, audience) }()
	awaitSettled(t, p, 1, returned(deleted))
	exchanged := make(chan exchange, 1)
	go func() {
		token, err := svc.Exchange(ctx, testSource, credential, nil)
		exchanged <- exchange{token, err}
	}()
	awaitSettled(t, p, 2, returned(deleted), returned(exchanged))
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, deleted), "the audience's deletion completes")
	done := receive(t, exchanged)
	require.NoError(t, done.err, "the exchange completes rather than deadlocking")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE audience_grn = $1`, audience.String()))
	_, err := store.ResolveToken(ctx, done.token.Hash())
	assert.NoError(t, err, "the control: the exchange's new token resolves")
}

// TestOperatorLockOrder_TheDeletionsOfAClientAndOfItsAudienceBothComplete
// is the audience cascade against the client's own deletion. The
// client's cascade took its tokens in expiry order and the audience's
// the ones naming it in heap order, each holding only its own
// registration, so parked between two tokens of the key in opposite
// orders each needed the one the other held. Both deletions now take
// them in token_hash order.
func TestOperatorLockOrder_TheDeletionsOfAClientAndOfItsAudienceBothComplete(t *testing.T) {
	ctx := context.Background()
	pool(t) // migrates and truncates the database; the store runs on p
	p := indexScanPool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	subject, _ := registerCredentialed(t, store, "controller")
	audience, _ := registerCredentialed(t, store, "hosted")
	_, key := issueKey(t, store, subject, operator.KeyRotationFirst)
	// Key tokens, which the client's cascade by operator reaches before
	// its cascade by key. Heap order is late, park-audience, early;
	// expiry order is early, park-client, late, park-audience.
	for _, row := range []tokenRow{
		{hash: "late", audience: &audience, expiresAt: "-1 hour"},
		{hash: "park-audience", audience: &audience, expiresAt: "1 hour"},
		{hash: "early", audience: &audience, expiresAt: "-3 hours"},
		{hash: "park-client", expiresAt: "-2 hours"},
	} {
		row.column, row.id, row.subject, row.issuedAt = "key_id", key.ID, subject, "-4 hours"
		insertTokenRow(t, p, row)
	}
	requireHeapOrder(t, p, "late", "park-audience", "early")
	requirePlanUses(t, p, "operator_tokens_operator_grn_expires_at_idx",
		`DELETE FROM operator_tokens WHERE operator_grn = $1`, subject.String())

	parked := parkOn(t, p, "park-audience", "park-client")
	audienceDeleted := make(chan error, 1)
	go func() { audienceDeleted <- store.Delete(ctx, audience) }()
	awaitSettled(t, p, 1, returned(audienceDeleted))
	clientDeleted := make(chan error, 1)
	go func() { clientDeleted <- store.Delete(ctx, subject) }()
	awaitSettled(t, p, 2, returned(audienceDeleted), returned(clientDeleted))
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, audienceDeleted), "the audience's deletion completes")
	require.NoError(t, receive(t, clientDeleted), "the client's deletion completes rather than deadlocking")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operators`), "both registrations are gone")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens`), "and every token with them")
}

// TestOperatorLockOrder_TwoExchangesOfOneOperatorTrimmingAndSweepingTheSameTokensBothComplete
// is #1423's named residual. A credential exchange began first; its
// trim took the credential's surplus tokens in credential order. A key
// exchange of the same operator began after two of them expired; its
// sweep took them in expiry order, the opposite one. Two exchanges hold
// the registration together, so nothing ordered them: parked between
// the two tokens, each needed the one the other held. An exchange now
// trims in token_hash order holding no token, and its sweep skips a
// token another holds.
func TestOperatorLockOrder_TwoExchangesOfOneOperatorTrimmingAndSweepingTheSameTokensBothComplete(t *testing.T) {
	ctx := context.Background()
	pool(t) // migrates and truncates the database; the store runs on p
	p := indexScanPool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := keyService(store)
	subject, credential := registerCredentialed(t, store, "controller")
	private, key := issueKey(t, store, subject, operator.KeyRotationFirst)
	issuance := credentialID(t, p, subject)
	// Five live tokens newer than the rest: the credential exchange's
	// trim keeps four beside its own and takes keep-0 and the old ones.
	for i := range operator.LiveTokensPerCredential {
		insertTokenRow(t, p, tokenRow{hash: "keep-" + string(rune('0'+i)), column: "credential_id", id: issuance,
			subject: subject, issuedAt: "-" + string(rune('9'-i)) + " minutes", expiresAt: "1 hour"})
	}
	for _, hash := range []string{"late", "park-trim", "early"} {
		insertTokenRow(t, p, tokenRow{hash: hash, column: "credential_id", id: issuance, subject: subject,
			issuedAt: "-4 hours", expiresAt: "1 hour"})
	}
	insertTokenRow(t, p, tokenRow{hash: "park-sweep", column: "key_id", id: key.ID, subject: subject,
		issuedAt: "-4 hours", expiresAt: "1 hour"})
	requirePlanUses(t, p, "operator_tokens_credential_id_idx",
		`DELETE FROM operator_tokens WHERE credential_id = $1`, issuance)
	requirePlanUses(t, p, "operator_tokens_operator_grn_expires_at_idx",
		`DELETE FROM operator_tokens WHERE operator_grn = $1 AND expires_at <= now()`, subject.String())

	// The credential exchange begins, and waits on its issuance's row
	// before it reads or writes a token.
	issuanceHolder, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = issuanceHolder.Rollback(ctx) }()
	_, err = issuanceHolder.Exec(ctx, `SELECT id FROM operator_credentials WHERE id = $1 FOR UPDATE`, issuance)
	require.NoError(t, err)
	credentialExchanged := make(chan exchange, 1)
	go func() {
		token, err := svc.Exchange(ctx, testSource, credential, nil)
		credentialExchanged <- exchange{token, err}
	}()
	var credentialPID int
	require.Eventually(t, func() bool {
		waiting := blockedBy(t, p, backendPID(t, issuanceHolder))
		if len(waiting) == 1 {
			credentialPID = waiting[0]
		}
		return len(waiting) == 1
	}, 10*time.Second, 10*time.Millisecond)
	// Expire three tokens after it began, so its now() finds them live
	// and the key exchange's finds them expired: early, park-sweep, late
	// in expiry order. Rewriting them in this order puts late,
	// park-trim, early in the heap, the order its trim walks.
	var began time.Time
	require.NoError(t, p.QueryRow(ctx, `SELECT xact_start FROM pg_stat_activity WHERE pid = $1`, credentialPID).Scan(&began))
	for _, set := range []struct {
		hash string
		at   time.Time
	}{
		{"late", began.Add(3 * time.Millisecond)},
		{"park-trim", began.Add(time.Hour)},
		{"early", began.Add(time.Millisecond)},
		{"park-sweep", began.Add(2 * time.Millisecond)},
	} {
		_, err := p.Exec(ctx, `UPDATE operator_tokens SET expires_at = $2 WHERE token_hash = $1`, set.hash, set.at)
		require.NoError(t, err)
	}
	requireHeapOrder(t, p, "keep-0", "late", "park-trim", "early")
	require.Eventually(t, func() bool {
		var passed bool
		return p.QueryRow(ctx, `SELECT now() > $1`, began.Add(3*time.Millisecond)).Scan(&passed) == nil && passed
	}, 10*time.Second, 10*time.Millisecond, "the key exchange begins after the three expired")

	parked := parkOn(t, p, "park-trim", "park-sweep")
	keyExchanged := make(chan exchange, 1)
	go func() {
		token, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), nil)
		keyExchanged <- exchange{token, err}
	}()
	awaitSettled(t, p, 2, returned(keyExchanged))
	require.NoError(t, issuanceHolder.Rollback(ctx))
	require.Eventually(t, func() bool {
		for _, pid := range blockedBy(t, p, backendPID(t, parked)) {
			if pid == credentialPID {
				return true
			}
		}
		return len(credentialExchanged) > 0
	}, 10*time.Second, 10*time.Millisecond, "the credential exchange reaches the parked token or returns")
	require.NoError(t, parked.Rollback(ctx))

	byCredential, byKey := receive(t, credentialExchanged), receive(t, keyExchanged)
	require.NoError(t, byCredential.err, "the credential exchange completes rather than deadlocking")
	require.NoError(t, byKey.err, "the key exchange completes rather than deadlocking")
	assert.Equal(t, operator.LiveTokensPerCredential,
		countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE credential_id = $1`, issuance),
		"the credential keeps its bound")
	assert.Equal(t, 0, countRows(t, p,
		`SELECT count(*) FROM operator_tokens WHERE operator_grn = $1 AND expires_at <= now()`, subject.String()),
		"no expired token is left")
	for _, done := range []exchange{byCredential, byKey} {
		_, err := store.ResolveToken(ctx, done.token.Hash())
		assert.NoError(t, err, "the control: each exchange's new token resolves")
	}
}

// TestOperatorLockOrder_TwoMintsSweepingEachOthersExpiredEnrollmentsBothComplete
// is #1424's enrollment sweep. A mint writes its own operator's token
// and sweeps every other operator's expired one, holding none of their
// registrations. Two re-mints of operators whose tokens had expired,
// held at a third expired token both sweeps reach first, each went on
// to the token the other had just written. Each sweep now skips a
// token another transaction holds.
func TestOperatorLockOrder_TwoMintsSweepingEachOthersExpiredEnrollmentsBothComplete(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	parkedOn, _ := registerCredentialed(t, store, "carol")
	first, _ := registerCredentialed(t, store, "alice")
	second, _ := registerCredentialed(t, store, "bob")
	// Expired in this order, the tokens sit in the heap in it too, and
	// carol's is the earliest to expire: both orders a sweep may take
	// meet it first.
	for i, g := range []grn.GRN{parkedOn, first, second} {
		_, err := p.Exec(ctx, `UPDATE operator_enrollment_tokens SET expires_at = now() - $2::interval WHERE operator_grn = $1`,
			g.String(), string(rune('3'-i))+" hours")
		require.NoError(t, err)
	}
	var heapOrdered bool
	require.NoError(t, p.QueryRow(ctx, `SELECT array_agg(operator_grn ORDER BY ctid) = $1 FROM operator_enrollment_tokens`,
		[]string{parkedOn.String(), first.String(), second.String()}).Scan(&heapOrdered))
	require.True(t, heapOrdered, "carol's, alice's and bob's tokens sit in the heap in that order")
	by := scopedGRN("acme", "user", "ada")

	parked, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = parked.Rollback(ctx) }()
	_, err = parked.Exec(ctx, `SELECT 1 FROM operator_enrollment_tokens WHERE operator_grn = $1 FOR UPDATE`, parkedOn.String())
	require.NoError(t, err)
	remint := func(g grn.GRN) chan error {
		e, err := operator.NewEnrollment(g, by)
		require.NoError(t, err)
		done := make(chan error, 1)
		go func() { done <- store.Remint(ctx, e) }()
		return done
	}
	firstMinted := remint(first)
	awaitSettled(t, p, 1, returned(firstMinted))
	secondMinted := remint(second)
	awaitSettled(t, p, 2, returned(firstMinted), returned(secondMinted))
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, firstMinted), "the first re-mint completes")
	require.NoError(t, receive(t, secondMinted), "the second re-mint completes rather than deadlocking")
	assert.Equal(t, 2, countRows(t, p,
		`SELECT count(*) FROM operator_enrollment_tokens WHERE operator_grn = ANY($1) AND expires_at > now()`,
		[]string{first.String(), second.String()}), "each re-minted operator holds a live token")
}

// TestOperatorLockOrder_AnOrganizationDeletionAndAnExchangeNamingAnAudienceBothComplete
// is #1424's organization deletion. Its cascade deleted the
// organization's operators in the order its scan met them, the
// audience's first here, while the exchange held one registration and
// then waited on the other: each held the registration the other
// needed next. The deletion now holds every operator's registration in
// GRN byte order before its cascade, after its agents' execution rows
// (000047), and the exchange holds its two in that order, so it waits
// for the deletion and finds what it named gone.
// With the client first in GRN order the case needs the deletion's
// order, with the audience first the exchange's.
func TestOperatorLockOrder_AnOrganizationDeletionAndAnExchangeNamingAnAudienceBothComplete(t *testing.T) {
	for name, c := range map[string]struct {
		client, audience string
		refused          error
	}{
		"the client first in GRN order":   {"alpha", "bravo", operator.ErrCredentialInvalid},
		"the audience first in GRN order": {"bravo", "alpha", operator.ErrAudienceInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := pool(t)
			orgs := org.NewPostgresStore(p)
			require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
			store := operator.NewPostgresStore(p)
			svc := exchangeService(store)
			// Registered first, the audience sits first in the heap, the
			// order the cascade deletes the operators in.
			audience, _ := registerCredentialed(t, store, c.audience)
			client, credential := registerCredentialed(t, store, c.client)
			var audienceFirst bool
			require.NoError(t, p.QueryRow(ctx, `SELECT (SELECT ctid FROM operators WHERE grn = $1) < (SELECT ctid FROM operators WHERE grn = $2)`,
				audience.String(), client.String()).Scan(&audienceFirst))
			require.True(t, audienceFirst, "the audience sits first in the heap")

			parked, err := p.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = parked.Rollback(ctx) }()
			_, err = parked.Exec(ctx, `SELECT grn FROM operators WHERE grn = $1 FOR UPDATE`, audience.String())
			require.NoError(t, err)
			deleted := make(chan error, 1)
			go func() { deleted <- orgs.DeleteOrg(ctx, orgRootGRN("acme")) }()
			awaitSettled(t, p, 1, returned(deleted))
			exchanged := make(chan exchange, 1)
			go func() {
				token, err := svc.Exchange(ctx, testSource, credential, &audience)
				exchanged <- exchange{token, err}
			}()
			awaitSettled(t, p, 2, returned(deleted), returned(exchanged))
			require.NoError(t, parked.Rollback(ctx))

			require.NoError(t, receive(t, deleted), "the organization's deletion completes rather than deadlocking")
			assert.ErrorIs(t, receive(t, exchanged).err, c.refused,
				"the exchange waited for the deletion and found the first registration it holds gone")
			assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operators`))
		})
	}
}

// TestOperatorLockOrder_AnAudiencesDeletionAndAWriterOfTheClientsTokensBothComplete
// holds every other statement that deletes several of a client's
// tokens to the token_hash order the audience's deletion now takes
// them in: an exchange's trim, a credential re-issue and a key
// withdrawal each walk their issuance's tokens, and before #1424 met
// the audience's cascade in the one heap order both walked. Two tokens
// naming the audience sit in the heap in the opposite of their
// token_hash order, so a writer walking the heap and parked between
// them would hold the one the deletion needs next.
func TestOperatorLockOrder_AnAudiencesDeletionAndAWriterOfTheClientsTokensBothComplete(t *testing.T) {
	type subject struct {
		store      *operator.PostgresStore
		client     grn.GRN
		credential string
		key        operator.Key
	}
	for name, c := range map[string]struct {
		column string
		write  func(ctx context.Context, s subject) error
	}{
		"an exchange's trim": {"credential_id", func(ctx context.Context, s subject) error {
			_, err := exchangeService(s.store).Exchange(ctx, testSource, s.credential, nil)
			return err
		}},
		"a credential re-issue": {"credential_id", func(ctx context.Context, s subject) error {
			reissued, err := operator.NewIssuedCredential(s.client, scopedGRN("acme", "user", "ada"))
			if err != nil {
				return err
			}
			return s.store.ReissueCredential(ctx, reissued)
		}},
		"a key withdrawal": {"key_id", func(ctx context.Context, s subject) error {
			return s.store.RevokeKey(ctx, s.client, s.key.KID)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			pool(t) // migrates and truncates the database; the store runs on p
			p := indexScanPool(t)
			require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
			store := operator.NewPostgresStore(p)
			client, credential := registerCredentialed(t, store, "controller")
			audience, _ := registerCredentialed(t, store, "hosted")
			peer, _ := registerCredentialed(t, store, "peer")
			_, key := issueKey(t, store, client, operator.KeyRotationFirst)
			issuance := key.ID
			if c.column == "credential_id" {
				issuance = credentialID(t, p, client)
			}
			// Five newer live tokens an exchange keeps beside its own, but
			// for keep-0; then late, park-writer and early in the heap,
			// all old and live, which every writer here deletes.
			for i := range operator.LiveTokensPerCredential {
				insertTokenRow(t, p, tokenRow{hash: "keep-" + string(rune('0'+i)), column: c.column, id: issuance,
					subject: client, issuedAt: "-" + string(rune('9'-i)) + " minutes", expiresAt: "1 hour"})
			}
			for _, row := range []tokenRow{
				{hash: "late", audience: &audience},
				{hash: "park-writer"},
				{hash: "early", audience: &audience},
			} {
				row.column, row.id, row.subject, row.issuedAt, row.expiresAt = c.column, issuance, client, "-4 hours", "1 hour"
				insertTokenRow(t, p, row)
			}
			// The deletion's own parked token, another client's, sorts
			// between early and late.
			insertTokenRow(t, p, tokenRow{hash: "hold-deletion", column: "credential_id", id: credentialID(t, p, peer),
				subject: peer, audience: &audience, issuedAt: "-4 hours", expiresAt: "1 hour"})
			requireHeapOrder(t, p, "keep-0", "late", "park-writer", "early")
			requirePlanUses(t, p, "operator_tokens_"+c.column+"_idx",
				`DELETE FROM operator_tokens WHERE `+c.column+` = $1`, issuance)

			parked := parkOn(t, p, "hold-deletion", "park-writer")
			deleted := make(chan error, 1)
			go func() { deleted <- store.Delete(ctx, audience) }()
			awaitSettled(t, p, 1, returned(deleted))
			written := make(chan error, 1)
			go func() {
				written <- c.write(ctx, subject{store: store, client: client, credential: credential, key: key})
			}()
			awaitSettled(t, p, 2, returned(deleted), returned(written))
			require.NoError(t, parked.Rollback(ctx))

			require.NoError(t, receive(t, deleted), "the audience's deletion completes")
			require.NoError(t, receive(t, written), "the writer completes rather than deadlocking")
			assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE token_hash IN ('early', 'late', 'park-writer')`),
				"the writer and the deletion removed every token either reached")
		})
	}
}

// TestOperatorLockOrder_AnAudiencesDeletionAndAnExchangeHoldingNoTokenItWaitsWithBothComplete
// holds an exchange to the order its token writes take: the trim, the
// one that may wait, before the sweep, and a sweep that skips a token
// another transaction holds. Either way round, the exchange would hold
// a token the audience's deletion needs next while waiting on one the
// deletion holds: a sweep that waited would hold what the trim took,
// and a sweep that ran first would hold what it took while the trim
// waited.
func TestOperatorLockOrder_AnAudiencesDeletionAndAnExchangeHoldingNoTokenItWaitsWithBothComplete(t *testing.T) {
	type token struct {
		hash                string
		byKey               bool
		issuedAt, expiresAt string
	}
	for name, c := range map[string]struct {
		keeps  bool
		tokens []token
	}{
		// The trim takes c-credential and the sweep reaches a-key.
		"a sweep reaching a token the deletion holds": {false, []token{
			{"a-key", true, "-4 hours", "-1 hour"},
			{"c-credential", false, "-4 hours", "-1 hour"},
		}},
		// The sweep would take c-credential and the trim reach a-surplus.
		"a trim reaching a token the deletion holds": {true, []token{
			{"a-surplus", false, "-4 hours", "1 hour"},
			{"c-credential", false, "-4 hours", "-1 hour"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			pool(t) // migrates and truncates the database; the store runs on p
			p := indexScanPool(t)
			require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
			store := operator.NewPostgresStore(p)
			client, credential := registerCredentialed(t, store, "controller")
			audience, _ := registerCredentialed(t, store, "hosted")
			peer, _ := registerCredentialed(t, store, "peer")
			_, key := issueKey(t, store, client, operator.KeyRotationFirst)
			issuance := credentialID(t, p, client)
			if c.keeps {
				for i := range operator.LiveTokensPerCredential {
					insertTokenRow(t, p, tokenRow{hash: "keep-" + string(rune('0'+i)), column: "credential_id", id: issuance,
						subject: client, issuedAt: "-" + string(rune('9'-i)) + " minutes", expiresAt: "1 hour"})
				}
			}
			for _, tk := range c.tokens {
				row := tokenRow{hash: tk.hash, column: "credential_id", id: issuance, subject: client, audience: &audience,
					issuedAt: tk.issuedAt, expiresAt: tk.expiresAt}
				if tk.byKey {
					row.column, row.id = "key_id", key.ID
				}
				insertTokenRow(t, p, row)
			}
			// The deletion's parked token, another client's, sorts between
			// the two the exchange reaches.
			insertTokenRow(t, p, tokenRow{hash: "b-park", column: "credential_id", id: credentialID(t, p, peer),
				subject: peer, audience: &audience, issuedAt: "-4 hours", expiresAt: "1 hour"})

			parked := parkOn(t, p, "b-park")
			deleted := make(chan error, 1)
			go func() { deleted <- store.Delete(ctx, audience) }()
			awaitSettled(t, p, 1, returned(deleted))
			exchanged := make(chan exchange, 1)
			go func() {
				token, err := exchangeService(store).Exchange(ctx, testSource, credential, nil)
				exchanged <- exchange{token, err}
			}()
			awaitSettled(t, p, 2, returned(deleted), returned(exchanged))
			require.NoError(t, parked.Rollback(ctx))

			require.NoError(t, receive(t, deleted), "the audience's deletion completes")
			done := receive(t, exchanged)
			require.NoError(t, done.err, "the exchange completes rather than deadlocking")
			assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE audience_grn = $1`, audience.String()))
			_, err := store.ResolveToken(ctx, done.token.Hash())
			assert.NoError(t, err, "the control: the exchange's new token resolves")
		})
	}
}

// TestOperatorLockOrder_AnOrganizationDeletionAndARemintSweepingItsEnrollmentsBothComplete
// is #1424's enrollment sweep against an organization's deletion. A
// re-mint of an operator with no enrollment row sweeps another
// operator's expired one and only then, at the insert's foreign key,
// waits on its own registration. The deletion, holding that
// registration and parked on a later one, went on to the swept row:
// each held what the other needed next. The re-mint now holds its
// registration before the sweep, so it waits for the deletion holding
// nothing and finds its operator gone.
func TestOperatorLockOrder_AnOrganizationDeletionAndARemintSweepingItsEnrollmentsBothComplete(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	// Registered in GRN order, which is the heap order the cascade takes
	// on dev and the order the deletion holds them in with 000046.
	reminted, _ := registerCredentialed(t, store, "alpha")
	parkedOn, _ := registerCredentialed(t, store, "bravo")
	swept, _ := registerCredentialed(t, store, "charlie")
	var heapOrdered bool
	require.NoError(t, p.QueryRow(ctx, `SELECT array_agg(grn ORDER BY ctid) = $1 FROM operators`,
		[]string{reminted.String(), parkedOn.String(), swept.String()}).Scan(&heapOrdered))
	require.True(t, heapOrdered, "alpha, bravo and charlie sit in the heap in that order")
	// alpha holds no enrollment row, so its re-mint inserts one; charlie's
	// has expired, so the re-mint's sweep takes it.
	_, err := p.Exec(ctx, `DELETE FROM operator_enrollment_tokens WHERE operator_grn = $1`, reminted.String())
	require.NoError(t, err)
	_, err = p.Exec(ctx, `UPDATE operator_enrollment_tokens SET expires_at = now() - interval '1 hour' WHERE operator_grn = $1`,
		swept.String())
	require.NoError(t, err)
	require.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM operator_enrollment_tokens WHERE expires_at <= now()`),
		"charlie's is the one expired token")

	// A holder of bravo's registration, as an exchange of bravo is.
	parked, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = parked.Rollback(ctx) }()
	_, err = parked.Exec(ctx, `SELECT grn FROM operators WHERE grn = $1 FOR KEY SHARE`, parkedOn.String())
	require.NoError(t, err)
	deleted := make(chan error, 1)
	go func() { deleted <- orgs.DeleteOrg(ctx, orgRootGRN("acme")) }()
	awaitSettled(t, p, 1, returned(deleted))
	e, err := operator.NewEnrollment(reminted, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	minted := make(chan error, 1)
	go func() { minted <- store.Remint(ctx, e) }()
	awaitSettled(t, p, 2, returned(deleted), returned(minted))
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, deleted), "the organization's deletion completes rather than deadlocking")
	assert.ErrorIs(t, receive(t, minted), operator.ErrOperatorNotFound,
		"the re-mint waited for the deletion and found its operator gone")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_enrollment_tokens`))
}
