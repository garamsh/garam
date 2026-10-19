package integration_test

import (
	"context"
	"crypto/rsa"
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
	orgpg "github.com/garamsh/garam/internal/org/postgres"
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
	return connect(t, os.Getenv("GARAM_TEST_DATABASE_URL"), func(cfg *pgxpool.Config) {
		cfg.ConnConfig.RuntimeParams["enable_seqscan"] = "off"
		cfg.ConnConfig.RuntimeParams["enable_bitmapscan"] = "off"
	})
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

// insertToken records a token row for subject under the key named,
// expiring at expiresAt, an SQL interval from now.
func insertToken(t *testing.T, p *pgxpool.Pool, hash string, keyID int64, subject grn.GRN, expiresAt string) {
	t.Helper()
	insertTokenRow(t, p, tokenRow{hash: hash, keyID: keyID, subject: subject, issuedAt: "-4 hours", expiresAt: expiresAt})
}

// tokenRow is a token row a test writes directly: the key it was
// exchanged under, its subject, the audience it names if any, and when
// it was issued and expires, as SQL intervals from now.
type tokenRow struct {
	hash                string
	keyID               int64
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
	_, err := p.Exec(context.Background(), `INSERT INTO operator_tokens (token_hash, key_id, operator_grn, audience_grn, issued_at, expires_at)
		VALUES ($1, $2, $3, $4, now() + $5::interval, now() + $6::interval)`,
		row.hash, row.keyID, row.subject.String(), audience, row.issuedAt, row.expiresAt)
	require.NoError(t, err)
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
// withdrawal deletes the key's tokens in key order; an exchange under
// the operator's other key sweeps its expired tokens in expiry order;
// two
// expired tokens of the key sit in opposite orders in the two. A third
// transaction parks each delete between them, on a row only that delete
// reaches, so on its release each needed the token the other held and
// Postgres aborted one with 40P01. Holding the registration first, the
// withdrawal makes the exchange wait for it before it deletes anything.
func TestOperatorLockOrder_AKeyWithdrawalAndAnExchangeSweepingItsTokensBothComplete(t *testing.T) {
	ctx := context.Background()
	pool(t) // migrates and truncates the database; the store runs on p
	p := indexScanPool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := keyService(store)
	subject := register(t, store, "controller")
	_, key := issueKey(t, store, subject, operator.KeyRotationFirst)
	successorKey, successor := issueKey(t, store, subject, operator.KeyRotationPlanned)
	// Key order is the heap order: late, park-withdrawal, early. Expiry
	// order is early, park-exchange, late.
	insertToken(t, p, "late", key.ID, subject, "-1 hour")
	insertToken(t, p, "park-withdrawal", key.ID, subject, "1 hour")
	insertToken(t, p, "early", key.ID, subject, "-3 hours")
	insertToken(t, p, "park-exchange", successor.ID, subject, "-2 hours")
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
		token, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, successorKey, subject), nil)
		exchanged <- exchange{token, err}
	}()
	awaitLockWaiters(t, p, 2)
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, withdrawn), "the withdrawal completes")
	done := receive(t, exchanged)
	require.NoError(t, done.err, "the exchange completes rather than deadlocking")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE key_id = $1`, key.ID))
	_, err := store.ResolveToken(ctx, done.token.Hash())
	assert.NoError(t, err, "the control: the other key's new token resolves")
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
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := keyService(store)
	subject := register(t, store, "controller")
	audience := register(t, store, "hosted")
	private, key := issueKey(t, store, subject, operator.KeyRotationFirst)
	// Heap order, which a scan for the audience's tokens takes, is late,
	// park-deletion, early. Expiry order is early, park-exchange, late.
	for _, row := range []tokenRow{
		{hash: "late", audience: &audience, expiresAt: "-1 hour"},
		{hash: "park-deletion", audience: &audience, expiresAt: "1 hour"},
		{hash: "early", audience: &audience, expiresAt: "-3 hours"},
		{hash: "park-exchange", expiresAt: "-2 hours"},
	} {
		row.keyID, row.subject, row.issuedAt = key.ID, subject, "-4 hours"
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
		token, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), nil)
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
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	subject := register(t, store, "controller")
	audience := register(t, store, "hosted")
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
		row.keyID, row.subject, row.issuedAt = key.ID, subject, "-4 hours"
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
// is #1423's named residual. An exchange under the operator's first key
// began first; its trim took that key's surplus tokens in key order. An
// exchange under its second key began after two of them expired; its
// sweep took them in expiry order, the opposite one. Two exchanges hold
// the registration together, so nothing ordered them: parked between
// the two tokens, each needed the one the other held. An exchange now
// trims in token_hash order holding no token, and its sweep skips a
// token another holds.
func TestOperatorLockOrder_TwoExchangesOfOneOperatorTrimmingAndSweepingTheSameTokensBothComplete(t *testing.T) {
	ctx := context.Background()
	pool(t) // migrates and truncates the database; the store runs on p
	p := indexScanPool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := keyService(store)
	subject := register(t, store, "controller")
	trimmingPrivate, trimming := issueKey(t, store, subject, operator.KeyRotationFirst)
	sweepingPrivate, sweeping := issueKey(t, store, subject, operator.KeyRotationPlanned)
	// Five live tokens newer than the rest: the first key's exchange
	// trims to four beside its own and takes keep-0 and the old ones.
	for i := range operator.LiveTokensPerKey {
		insertTokenRow(t, p, tokenRow{hash: "keep-" + string(rune('0'+i)), keyID: trimming.ID,
			subject: subject, issuedAt: "-" + string(rune('9'-i)) + " minutes", expiresAt: "1 hour"})
	}
	for _, hash := range []string{"late", "park-trim", "early"} {
		insertTokenRow(t, p, tokenRow{hash: hash, keyID: trimming.ID, subject: subject,
			issuedAt: "-4 hours", expiresAt: "1 hour"})
	}
	insertTokenRow(t, p, tokenRow{hash: "park-sweep", keyID: sweeping.ID, subject: subject,
		issuedAt: "-4 hours", expiresAt: "1 hour"})
	requirePlanUses(t, p, "operator_tokens_key_id_idx",
		`DELETE FROM operator_tokens WHERE key_id = $1`, trimming.ID)
	requirePlanUses(t, p, "operator_tokens_operator_grn_expires_at_idx",
		`DELETE FROM operator_tokens WHERE operator_grn = $1 AND expires_at <= now()`, subject.String())

	// The first key's exchange begins, and waits on its key's row
	// before it reads or writes a token.
	issuanceHolder, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = issuanceHolder.Rollback(ctx) }()
	_, err = issuanceHolder.Exec(ctx, `SELECT id FROM operator_keys WHERE id = $1 FOR UPDATE`, trimming.ID)
	require.NoError(t, err)
	trimExchanged := make(chan exchange, 1)
	go func() {
		token, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, trimmingPrivate, subject), nil)
		trimExchanged <- exchange{token, err}
	}()
	var trimPID int
	require.Eventually(t, func() bool {
		waiting := blockedBy(t, p, backendPID(t, issuanceHolder))
		if len(waiting) == 1 {
			trimPID = waiting[0]
		}
		return len(waiting) == 1
	}, 10*time.Second, 10*time.Millisecond)
	// Expire three tokens after it began, so its now() finds them live and
	// the second key's exchange finds them expired: early, park-sweep,
	// late in expiry order. Rewriting them in this order puts late,
	// park-trim, early in the heap, the order its trim walks.
	var began time.Time
	require.NoError(t, p.QueryRow(ctx, `SELECT xact_start FROM pg_stat_activity WHERE pid = $1`, trimPID).Scan(&began))
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
	}, 10*time.Second, 10*time.Millisecond, "the second key's exchange begins after the three expired")

	parked := parkOn(t, p, "park-trim", "park-sweep")
	sweepExchanged := make(chan exchange, 1)
	go func() {
		token, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, sweepingPrivate, subject), nil)
		sweepExchanged <- exchange{token, err}
	}()
	awaitSettled(t, p, 2, returned(sweepExchanged))
	require.NoError(t, issuanceHolder.Rollback(ctx))
	require.Eventually(t, func() bool {
		for _, pid := range blockedBy(t, p, backendPID(t, parked)) {
			if pid == trimPID {
				return true
			}
		}
		return len(trimExchanged) > 0
	}, 10*time.Second, 10*time.Millisecond, "the first key's exchange reaches the parked token or returns")
	require.NoError(t, parked.Rollback(ctx))

	byTrim, bySweep := receive(t, trimExchanged), receive(t, sweepExchanged)
	require.NoError(t, byTrim.err, "the first key's exchange completes rather than deadlocking")
	require.NoError(t, bySweep.err, "the second key's exchange completes rather than deadlocking")
	assert.Equal(t, operator.LiveTokensPerKey,
		countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE key_id = $1`, trimming.ID),
		"the first key keeps its bound")
	assert.Equal(t, 0, countRows(t, p,
		`SELECT count(*) FROM operator_tokens WHERE operator_grn = $1 AND expires_at <= now()`, subject.String()),
		"no expired token is left")
	for _, done := range []exchange{byTrim, bySweep} {
		_, err := store.ResolveToken(ctx, done.token.Hash())
		assert.NoError(t, err, "the control: each exchange's new token resolves")
	}
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
		"the client first in GRN order":   {"alpha", "bravo", operator.ErrClientInvalid},
		"the audience first in GRN order": {"bravo", "alpha", operator.ErrAudienceInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := pool(t)
			orgs := orgpg.New(p)
			require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
			store := operator.NewPostgresStore(p)
			svc := keyService(store)
			// Registered first, the audience sits first in the heap, the
			// order the cascade deletes the operators in.
			audience := register(t, store, c.audience)
			client, private := keyed(t, store, c.client)
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
				token, err := svc.ExchangeAssertion(ctx, testSource, client, freshAssertion(t, private, client), &audience)
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
// them in: an exchange's trim and a key withdrawal each walk their
// key's tokens, and before #1424 met
// the audience's cascade in the one heap order both walked. Two tokens
// naming the audience sit in the heap in the opposite of their
// token_hash order, so a writer walking the heap and parked between
// them would hold the one the deletion needs next.
func TestOperatorLockOrder_AnAudiencesDeletionAndAWriterOfTheClientsTokensBothComplete(t *testing.T) {
	type subject struct {
		store   *operator.PostgresStore
		client  grn.GRN
		private *rsa.PrivateKey
		key     operator.Key
	}
	for name, write := range map[string]func(ctx context.Context, t *testing.T, s subject) error{
		"an exchange's trim": func(ctx context.Context, t *testing.T, s subject) error {
			_, err := keyService(s.store).ExchangeAssertion(ctx, testSource, s.client, freshAssertion(t, s.private, s.client), nil)
			return err
		},
		"a key withdrawal": func(ctx context.Context, _ *testing.T, s subject) error {
			return s.store.RevokeKey(ctx, s.client, s.key.KID)
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			pool(t) // migrates and truncates the database; the store runs on p
			p := indexScanPool(t)
			require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
			store := operator.NewPostgresStore(p)
			client := register(t, store, "controller")
			audience := register(t, store, "hosted")
			peer := register(t, store, "peer")
			private, key := issueKey(t, store, client, operator.KeyRotationFirst)
			_, peerKey := issueKey(t, store, peer, operator.KeyRotationFirst)
			// Five newer live tokens an exchange keeps beside its own, but
			// for keep-0; then late, park-writer and early in the heap,
			// all old and live, which every writer here deletes.
			for i := range operator.LiveTokensPerKey {
				insertTokenRow(t, p, tokenRow{hash: "keep-" + string(rune('0'+i)), keyID: key.ID,
					subject: client, issuedAt: "-" + string(rune('9'-i)) + " minutes", expiresAt: "1 hour"})
			}
			for _, row := range []tokenRow{
				{hash: "late", audience: &audience},
				{hash: "park-writer"},
				{hash: "early", audience: &audience},
			} {
				row.keyID, row.subject, row.issuedAt, row.expiresAt = key.ID, client, "-4 hours", "1 hour"
				insertTokenRow(t, p, row)
			}
			// The deletion's own parked token, another client's, sorts
			// between early and late.
			insertTokenRow(t, p, tokenRow{hash: "hold-deletion", keyID: peerKey.ID,
				subject: peer, audience: &audience, issuedAt: "-4 hours", expiresAt: "1 hour"})
			requireHeapOrder(t, p, "keep-0", "late", "park-writer", "early")
			requirePlanUses(t, p, "operator_tokens_key_id_idx",
				`DELETE FROM operator_tokens WHERE key_id = $1`, key.ID)

			parked := parkOn(t, p, "hold-deletion", "park-writer")
			deleted := make(chan error, 1)
			go func() { deleted <- store.Delete(ctx, audience) }()
			awaitSettled(t, p, 1, returned(deleted))
			written := make(chan error, 1)
			go func() {
				written <- write(ctx, t, subject{store: store, client: client, private: private, key: key})
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
		hash string
		// byOther puts the token under the client's other key, which
		// the exchange's trim never reaches and its sweep does.
		byOther             bool
		issuedAt, expiresAt string
	}
	for name, c := range map[string]struct {
		keeps  bool
		tokens []token
	}{
		// The trim takes c-own and the sweep reaches a-other.
		"a sweep reaching a token the deletion holds": {false, []token{
			{"a-other", true, "-4 hours", "-1 hour"},
			{"c-own", false, "-4 hours", "-1 hour"},
		}},
		// The sweep would take c-own and the trim reach a-surplus.
		"a trim reaching a token the deletion holds": {true, []token{
			{"a-surplus", false, "-4 hours", "1 hour"},
			{"c-own", false, "-4 hours", "-1 hour"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			pool(t) // migrates and truncates the database; the store runs on p
			p := indexScanPool(t)
			require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
			store := operator.NewPostgresStore(p)
			client := register(t, store, "controller")
			audience := register(t, store, "hosted")
			peer := register(t, store, "peer")
			private, own := issueKey(t, store, client, operator.KeyRotationFirst)
			_, other := issueKey(t, store, client, operator.KeyRotationPlanned)
			_, peerKey := issueKey(t, store, peer, operator.KeyRotationFirst)
			if c.keeps {
				for i := range operator.LiveTokensPerKey {
					insertTokenRow(t, p, tokenRow{hash: "keep-" + string(rune('0'+i)), keyID: own.ID,
						subject: client, issuedAt: "-" + string(rune('9'-i)) + " minutes", expiresAt: "1 hour"})
				}
			}
			for _, tk := range c.tokens {
				row := tokenRow{hash: tk.hash, keyID: own.ID, subject: client, audience: &audience,
					issuedAt: tk.issuedAt, expiresAt: tk.expiresAt}
				if tk.byOther {
					row.keyID = other.ID
				}
				insertTokenRow(t, p, row)
			}
			// The deletion's parked token, another client's, sorts between
			// the two the exchange reaches.
			insertTokenRow(t, p, tokenRow{hash: "b-park", keyID: peerKey.ID,
				subject: peer, audience: &audience, issuedAt: "-4 hours", expiresAt: "1 hour"})

			parked := parkOn(t, p, "b-park")
			deleted := make(chan error, 1)
			go func() { deleted <- store.Delete(ctx, audience) }()
			awaitSettled(t, p, 1, returned(deleted))
			exchanged := make(chan exchange, 1)
			go func() {
				token, err := keyService(store).ExchangeAssertion(ctx, testSource, client, freshAssertion(t, private, client), nil)
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
