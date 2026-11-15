package integration_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
)

// The pair of organizations the consent cases use: outboxOrg (acme) is
// the pair's org_a and otherOrg (globex) its org_b, since the pair is
// stored ordered.
const (
	grantA = `INSERT INTO message_consents (org_a, org_b, a_consented_at) VALUES ('` + outboxOrg + `', '` + otherOrg + `', now())`
	grantB = `INSERT INTO message_consents (org_a, org_b, b_consented_at) VALUES ('` + outboxOrg + `', '` + otherOrg + `', now())`
	grants = `INSERT INTO message_consents (org_a, org_b, a_consented_at, b_consented_at) VALUES ('` + outboxOrg + `', '` + otherOrg + `', now(), now())`

	// The grant of the store's GrantConsent, acme granting toward
	// globex or globex toward acme: the first creates the row, a repeat
	// only re-stamps the half.
	upsertA = `INSERT INTO message_consents (org_a, org_b, a_consented_at, b_consented_at)
		VALUES ('` + outboxOrg + `', '` + otherOrg + `', now(), NULL)
		ON CONFLICT (org_a, org_b) DO UPDATE SET
		a_consented_at = coalesce(excluded.a_consented_at, message_consents.a_consented_at),
		b_consented_at = coalesce(excluded.b_consented_at, message_consents.b_consented_at)`
	upsertB = `INSERT INTO message_consents (org_a, org_b, a_consented_at, b_consented_at)
		VALUES ('` + outboxOrg + `', '` + otherOrg + `', NULL, now())
		ON CONFLICT (org_a, org_b) DO UPDATE SET
		a_consented_at = coalesce(excluded.a_consented_at, message_consents.a_consented_at),
		b_consented_at = coalesce(excluded.b_consented_at, message_consents.b_consented_at)`

	// A withdrawal by acme is the store's two statements: the update
	// clears its half while globex's stands, and the delete drops the
	// row where it was the last (WithdrawConsent, DropEmptyConsent).
	// Each does nothing where the other's condition holds.
	withdrawA = `UPDATE message_consents SET a_consented_at = NULL WHERE b_consented_at IS NOT NULL`
	dropEmpty = `DELETE FROM message_consents WHERE b_consented_at IS NULL`

	orgMarkA = "Iam::Org " + outboxOrg
	orgMarkB = "Iam::Org " + otherOrg
)

// seedConsentPair turns the feed on, seeds both organizations and runs
// setup, then empties the outbox, so a case reads only what its own
// statements marked.
func seedConsentPair(t *testing.T, setup ...string) *pgxpool.Pool {
	t.Helper()
	p := pool(t)
	startFeed(t, p)
	seedOutboxOrg(t, p)
	seedOtherOrg(t, p)
	for _, q := range setup {
		execSQL(t, p, q)
	}
	execSQL(t, p, `DELETE FROM policy_outbox`)
	return p
}

func TestPolicyOutbox_AConsentChangeMarksTheOrganizationWhoseHalfChanged(t *testing.T) {
	tests := []struct {
		name  string
		setup []string
		sql   []string
		want  []string
	}{
		{"the first organization grants", nil, []string{upsertA}, []string{orgMarkA}},
		{"the second organization grants", nil, []string{upsertB}, []string{orgMarkB}},
		{"the other organization grants toward the first", []string{upsertA},
			[]string{upsertB}, []string{orgMarkB}},
		{"an organization grants again", []string{upsertA}, []string{upsertA}, nil},
		{"an organization grants again beside the other's half", []string{grants},
			[]string{upsertA}, nil},
		{"a row is written with both halves", nil, []string{grants}, []string{orgMarkA, orgMarkB}},
		{"an organization withdraws and the other's half stands", []string{grants},
			[]string{withdrawA}, []string{orgMarkA}},
		{"an organization withdraws its last half and the row is dropped", []string{grantA},
			[]string{withdrawA, dropEmpty}, []string{orgMarkA}},
		{"a withdrawal of a half beside the other's drops no row", []string{grants},
			[]string{dropEmpty}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := seedConsentPair(t, tt.setup...)

			for _, q := range tt.sql {
				execSQL(t, p, q)
			}

			want := append([]string{}, tt.want...)
			sort.Strings(want)
			assert.Equal(t, want, markKeys(outboxMarks(t, p)))
		})
	}
}

// TestPolicyOutbox_DeletingAnOrganizationMarksTheOnesThatHeldAHalfTowardIt
// holds ADR-0147 item 2: the foreign key takes the pair's row with the
// organization, and the other organization's `consents` loses it.
func TestPolicyOutbox_DeletingAnOrganizationMarksTheOnesThatHeldAHalfTowardIt(t *testing.T) {
	p := seedConsentPair(t, grants)

	execSQL(t, p, `DELETE FROM orgs WHERE grn = $1`, outboxOrg)

	assert.Contains(t, markKeys(outboxMarks(t, p)), orgMarkB, "the organization that held a half toward it")
	assert.NotContains(t, markKeys(outboxMarks(t, p)), "Iam::OrgMembers "+otherOrg, "and nothing else of it")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM message_consents`))

	// The control: an organization that held no half toward the deleted
	// one is not marked.
	p = seedConsentPair(t, grantA)
	execSQL(t, p, `DELETE FROM orgs WHERE grn = $1`, outboxOrg)
	assert.NotContains(t, markKeys(outboxMarks(t, p)), orgMarkB)
}

func TestPolicyOutbox_AFeedThatIsOffMarksNoConsent(t *testing.T) {
	p := pool(t)
	require.False(t, feedEnabled(t, p))
	seedOutboxOrg(t, p)
	seedOtherOrg(t, p)

	execSQL(t, p, grants)

	assert.Empty(t, outboxMarks(t, p))
}

func TestPolicyEntityState_AnOrgNamesTheOrganizationsItHoldsAHalfToward(t *testing.T) {
	p := pool(t)
	seedOutboxOrg(t, p)
	seedOtherOrg(t, p)
	consents := func(org string) []string {
		var ids []string
		require.NoError(t, p.QueryRow(context.Background(),
			`SELECT consent_ids FROM policy_entity_state('Iam::Org', $1)`, org).Scan(&ids))
		return ids
	}
	require.Empty(t, consents(outboxOrg), "the control: no half stands")

	execSQL(t, p, grantA)
	assert.Equal(t, []string{otherOrg}, consents(outboxOrg), "the half's own side")
	assert.Empty(t, consents(otherOrg), "not the other side")

	execSQL(t, p, `UPDATE message_consents SET b_consented_at = now()`)
	assert.Equal(t, []string{outboxOrg}, consents(otherOrg), "each side holds its own")

	execSQL(t, p, withdrawA)
	assert.Empty(t, consents(outboxOrg), "a withdrawn half is gone")
	assert.Equal(t, []string{outboxOrg}, consents(otherOrg), "the other's stands")

	var teamIDs []string
	require.NoError(t, p.QueryRow(context.Background(),
		`SELECT consent_ids FROM policy_entity_state('Iam::Team', $1)`, outboxTeam).Scan(&teamIDs))
	assert.Empty(t, teamIDs, "only an Org carries consents")
}

// consentsOf is the consents the last upsert of org carried.
func consentsOf(t *testing.T, fake *fakePolicy, org string) []string {
	t.Helper()
	var out []string
	var found bool
	for _, req := range fake.received() {
		c, ok := changesByEntity(req)["Iam::Org "+org]
		if !ok {
			continue
		}
		found = true
		out = nil
		for _, o := range c.GetUpsert().GetConsents() {
			assert.Equal(t, "Iam::Org", o.GetType())
			out = append(out, o.GetId())
		}
	}
	require.True(t, found, "the service was sent %s", org)
	return out
}

func TestPolicyDelivery_AGrantIsDeliveredAsConsentsAndAWithdrawalAsTheirRemoval(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	startFeed(t, p)
	seedOutboxOrg(t, p)
	seedOtherOrg(t, p)
	_, err := d.pass(t)
	require.NoError(t, err)
	require.Empty(t, consentsOf(t, fake, outboxOrg), "the control: an organization with no half carries none")

	execSQL(t, p, grants)
	require.Equal(t, []string{orgMarkA, orgMarkB}, markKeys(outboxMarks(t, p)))
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Equal(t, []string{otherOrg}, consentsOf(t, fake, outboxOrg))
	assert.Equal(t, []string{outboxOrg}, consentsOf(t, fake, otherOrg))
	assert.Empty(t, outboxMarks(t, p))

	execSQL(t, p, withdrawA)
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Empty(t, consentsOf(t, fake, outboxOrg), "the withdrawal is the attribute without the organization")
	assert.Equal(t, []string{outboxOrg}, consentsOf(t, fake, otherOrg), "the other's half stands")
}

func TestPolicyFeedStart_DeliversConsentsThatExistedBefore(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	seedOutboxOrg(t, p)
	seedOtherOrg(t, p)
	execSQL(t, p, grants)
	require.False(t, feedEnabled(t, p))
	require.Empty(t, outboxMarks(t, p), "the consent was written with the feed off")

	startFeed(t, p)
	_, err := d.pass(t)
	require.NoError(t, err)

	assert.Equal(t, []string{otherOrg}, consentsOf(t, fake, outboxOrg))
	assert.Equal(t, []string{outboxOrg}, consentsOf(t, fake, otherOrg))
}

func TestPolicyDelivery_AnOrganizationDeletedLeavesTheOtherWithoutItsConsent(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	startFeed(t, p)
	seedOutboxOrg(t, p)
	seedOtherOrg(t, p)
	execSQL(t, p, grants)
	_, err := d.pass(t)
	require.NoError(t, err)
	require.Equal(t, []string{outboxOrg}, consentsOf(t, fake, otherOrg), "the control: it held the consent")

	execSQL(t, p, `DELETE FROM orgs WHERE grn = $1`, outboxOrg)
	_, err = d.pass(t)
	require.NoError(t, err)

	assert.Empty(t, consentsOf(t, fake, otherOrg))
}

func TestPolicyGate_AwaitEntityAnswersOnlyOnceTheConsentIsInForce(t *testing.T) {
	p, fake, d := seedDelivered(t)
	seedOtherOrg(t, p)
	_, err := d.pass(t)
	require.NoError(t, err)
	gate := runLoop(t, p, d, 5*time.Second)
	time.Sleep(200 * time.Millisecond)
	entity := policyfeed.Entity{Type: "Iam::Org", ID: outboxOrg}

	execSQL(t, p, grantA)
	require.Equal(t, []string{orgMarkA}, markKeys(outboxMarks(t, p)), "the control: not delivered before the wait")

	require.NoError(t, gate.AwaitEntity(context.Background(), entity))

	assert.Equal(t, []string{otherOrg}, consentsOf(t, fake, outboxOrg), "the answer comes with the consent in force")
}

func TestPolicyGate_AwaitEntityAnswersUnavailableWhileTheConsentIsNotApplied(t *testing.T) {
	p, fake, d := seedDelivered(t)
	seedOtherOrg(t, p)
	_, err := d.pass(t)
	require.NoError(t, err)
	gate := runLoop(t, p, d, 400*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	fake.mu.Lock()
	fake.fail = status.Error(codes.Unavailable, "policy service down")
	fake.mu.Unlock()
	entity := policyfeed.Entity{Type: "Iam::Org", ID: outboxOrg}

	execSQL(t, p, grantA)
	err = gate.AwaitEntity(context.Background(), entity)

	require.ErrorIs(t, err, policyfeed.ErrNotApplied)
	assert.Equal(t, errorx.KindUnavailable, errorx.KindOf(err))
	assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM message_consents`), "the half is stored")
	assert.Contains(t, outboxMarks(t, p), orgMarkA, "its mark waits")
	assert.NoError(t, gate.AwaitEntity(context.Background(), policyfeed.Entity{Type: "Iam::Org", ID: otherOrg}),
		"the control: an entity with no mark is not waited for")

	fake.mu.Lock()
	fake.fail = nil
	fake.mu.Unlock()
	require.Eventually(t, func() bool { return len(outboxMarks(t, p)) == 0 }, 5*time.Second, 20*time.Millisecond)
	assert.NoError(t, gate.AwaitEntity(context.Background(), entity), "and the wait is over")
}

// TestMigration000077_MarksTheHoldersOfAHalfOfAFeedThatIsOn applies
// 000077 over consents that exist: a feed that is on marks each
// organization holding a half and one that is off marks none, since its
// start marks every organization. The way down removes the trigger.
func TestMigration000077_MarksTheHoldersOfAHalfOfAFeedThatIsOn(t *testing.T) {
	for _, on := range []bool{true, false} {
		name := "a feed that is off"
		schema := "migrate_000077_off"
		if on {
			name = "a feed that is on"
			schema = "migrate_000077_on"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := pool(t)
			m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = m.Close() })
			require.NoError(t, m.Migrate(76))
			if on {
				_, err = p.Exec(ctx, "UPDATE "+schema+".policy_feed SET enabled = true")
				require.NoError(t, err)
			}
			for _, q := range []string{
				`INSERT INTO ` + schema + `.orgs (grn, name, created_at, updated_at) VALUES ('` + outboxOrg + `', 'acme', now(), now())`,
				`INSERT INTO ` + schema + `.orgs (grn, name, created_at, updated_at) VALUES ('` + otherOrg + `', 'globex', now(), now())`,
				`INSERT INTO ` + schema + `.message_consents (org_a, org_b, a_consented_at) VALUES ('` + outboxOrg + `', '` + otherOrg + `', now())`,
			} {
				_, err = p.Exec(ctx, q)
				require.NoError(t, err, q)
			}
			_, err = p.Exec(ctx, "DELETE FROM "+schema+".policy_outbox")
			require.NoError(t, err)
			require.NoError(t, m.Migrate(77))

			want := 0
			if on {
				want = 1
			}
			assert.Equal(t, want, countRows(t, p,
				"SELECT count(*) FROM "+schema+".policy_outbox WHERE entity_type = 'Iam::Org' AND entity_id = '"+outboxOrg+"'"),
				"the holder of the half is marked")
			assert.Equal(t, 0, countRows(t, p,
				"SELECT count(*) FROM "+schema+".policy_outbox WHERE entity_id = '"+otherOrg+"'"),
				"the organization holding none is not")

			require.NoError(t, m.Migrate(76), "000077 rolls back")
			assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM pg_trigger WHERE tgname = 'policy_outbox_message_consents' AND tgrelid = '"+schema+".message_consents'::regclass"))
		})
	}
}

// TestPolicyFeedStart_WaitsOutAWriterOfAConsentThatReadTheFeedAsOff
// holds that start takes message_consents with the other source
// tables: a grant in flight when the feed is switched on is committed
// before the backfill reads, so a trigger that read the feed as off
// leaves no consent the backfill cannot see.
func TestPolicyFeedStart_WaitsOutAWriterOfAConsentThatReadTheFeedAsOff(t *testing.T) {
	p := pool(t)
	ctx := context.Background()
	seedOutboxOrg(t, p)
	seedOtherOrg(t, p)
	// Registered first so it runs last: Stop waits on the writer's
	// lock, which the rollback below releases.
	t.Cleanup(func() { _, _ = policyfeedpg.New(p).Stop(ctx) })
	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	_, err = tx.Exec(ctx, grantA)
	require.NoError(t, err)

	started := make(chan error, 1)
	go func() {
		_, err := policyfeedpg.New(p).Start(ctx)
		started <- err
	}()

	require.Eventually(t, func() bool {
		var n int
		err := p.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE 'LOCK TABLE%'`).Scan(&n)
		return err == nil && n == 1
	}, 10*time.Second, 20*time.Millisecond, "start waits on the writer of a consent")
	select {
	case err := <-started:
		require.Failf(t, "start returned while a writer held the table", "%v", err)
	default:
	}

	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-started)
	assert.Contains(t, markKeys(outboxMarks(t, p)), orgMarkA, "the organization that granted is marked")
}
