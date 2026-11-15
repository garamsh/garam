package integration_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
)

// delegableOperations are the operations the policy service lets an
// Operator be principal of, in the wire form its manifest names them
// (policy/crates/manifest/tests/manifests.rs holds the same ten). The
// platform stores each of them under that name, so nothing maps one to
// the other, and this list is what holds the two together.
var delegableOperations = []string{
	"iam:agent.activate", "iam:agent.configure", "iam:agent.create", "iam:agent.execution-read",
	"iam:agent.recover", "iam:agent.replace", "iam:agent-template.publish", "iam:agent-template.read",
	"iam:execution-profile.read", "iam:operator.read",
}

// operatorMark is the outbox key of the hosted operator.
func (f *delegationFixture) operatorMark() string { return "Iam::Operator " + f.hosted.String() }

// operationsOf is the operations the last upsert of the operator
// carried, and the parents it carried with them.
func operationsOf(t *testing.T, fake *fakePolicy, operatorGRN grn.GRN) (operations, parents []string) {
	t.Helper()
	var found bool
	for _, req := range fake.received() {
		c, ok := changesByEntity(req)["Iam::Operator "+operatorGRN.String()]
		if !ok {
			continue
		}
		found = true
		operations = c.GetUpsert().GetOperations()
		parents = parentKeys(c)
	}
	require.True(t, found, "the service was sent %s", operatorGRN)
	return operations, parents
}

// seedDelegationFeed registers the delegation fixture's operators with
// the feed on and delivers everything, so the outbox is empty and the
// service holds each operator without a delegation.
func seedDelegationFeed(t *testing.T) (*delegationFixture, *fakePolicy, deliverer) {
	t.Helper()
	f := newDelegationFixture(t)
	fake, key := newFake(t)
	d := newDeliverer(t, f.pool, fake, key)
	startFeed(t, f.pool)
	_, err := d.pass(t)
	require.NoError(t, err)
	require.Empty(t, outboxMarks(t, f.pool), "the case needs an empty outbox")
	return f, fake, d
}

func (f *delegationFixture) end(t *testing.T, id operator.GrantID) {
	t.Helper()
	require.NoError(t, f.service.EndGrant(context.Background(), f.actor, f.org, f.hosted, id))
}

// replace writes a grant that replaces first, covering ops.
func (f *delegationFixture) replace(t *testing.T, first *operator.Grant, requestID string, ops ...operator.Operation) *operator.Grant {
	t.Helper()
	g, created, err := f.write(requestID, grantRef(first.ID), f.terms(t, []grn.GRN{f.controller}, ops...))
	require.NoError(t, err)
	require.True(t, created)
	return g
}

func TestPolicyOutbox_ADelegationWrittenReplacedOrEndedMarksItsOperatorAlone(t *testing.T) {
	f, _, _ := seedDelegationFeed(t)
	drain := func() { execSQL(t, f.pool, `DELETE FROM policy_outbox`) }

	first := f.grant(t)
	assert.Equal(t, []string{f.operatorMark()}, markKeys(outboxMarks(t, f.pool)), "a write")

	drain()
	second := f.replace(t, first, "grant-2", operator.OperationAgentActivate)
	assert.Equal(t, []string{f.operatorMark()}, markKeys(outboxMarks(t, f.pool)), "a replacement")

	drain()
	f.end(t, second.ID)
	assert.Equal(t, []string{f.operatorMark()}, markKeys(outboxMarks(t, f.pool)), "an end")

	drain()
	execSQL(t, f.pool, `UPDATE operator_delegations SET expires_at = now() - interval '1 second'`)
	assert.Empty(t, outboxMarks(t, f.pool), "the control: a lapse is a clock and writes nothing")
}

func TestPolicyOutbox_AFeedThatIsOffMarksNoDelegation(t *testing.T) {
	f := newDelegationFixture(t)
	require.False(t, feedEnabled(t, f.pool))

	f.grant(t)

	assert.Empty(t, outboxMarks(t, f.pool))
}

func TestPolicyOutbox_DeletingAnOperatorWithADelegationMarksIt(t *testing.T) {
	f, _, _ := seedDelegationFeed(t)
	f.grant(t)
	execSQL(t, f.pool, `DELETE FROM policy_outbox`)

	execSQL(t, f.pool, `DELETE FROM operators WHERE grn = $1`, f.hosted.String())

	assert.Contains(t, markKeys(outboxMarks(t, f.pool)), f.operatorMark())
	assert.Equal(t, 0, countRows(t, f.pool, `SELECT count(*) FROM operator_delegations`),
		"the delegation went with it")
}

func TestPolicyEntityState_AnOperatorHoldsTheOperationsOfItsCurrentDelegationAlone(t *testing.T) {
	f := newDelegationFixture(t)
	read := func(operatorGRN grn.GRN) (ops, parents []string) {
		var types, ids []string
		require.NoError(t, f.pool.QueryRow(context.Background(),
			`SELECT operations, parent_types, parent_ids FROM policy_entity_state('Iam::Operator', $1)`,
			operatorGRN.String()).Scan(&ops, &types, &ids))
		for i := range types {
			parents = append(parents, types[i]+" "+ids[i])
		}
		return ops, parents
	}
	org := f.org.String()
	ops, parents := read(f.hosted)
	require.Empty(t, ops, "the control: no delegation holds none")
	assert.Equal(t, []string{"Iam::Org " + org, "Iam::OrgMembers " + org}, parents,
		"an operator sits in its org and among its members")

	first := f.grant(t)
	ops, _ = read(f.hosted)
	assert.Equal(t, []string{"iam:agent.configure", "iam:agent.create"}, ops)
	other, _ := read(f.other)
	assert.Empty(t, other, "another operator's is not its")

	second := f.replace(t, first, "grant-2", operator.OperationAgentActivate)
	ops, _ = read(f.hosted)
	assert.Equal(t, []string{"iam:agent.activate"}, ops, "a replaced grant is what then stands, not both")

	execSQL(t, f.pool, `UPDATE operator_delegations SET expires_at = now() - interval '1 second'
		WHERE id = $1`, string(second.ID))
	ops, _ = read(f.hosted)
	assert.Equal(t, []string{"iam:agent.activate"}, ops, "a grant that lapsed and was not ended is as stored")

	f.end(t, second.ID)
	ops, _ = read(f.hosted)
	assert.Empty(t, ops, "an ended grant holds none")
}

func TestPolicyDelivery_ADelegationIsDeliveredAsOperationsAndItsEndAsNone(t *testing.T) {
	f, fake, d := seedDelegationFeed(t)
	ops, parents := operationsOf(t, fake, f.hosted)
	require.Empty(t, ops, "the control: an operator with no delegation carries none")
	org := f.org.String()
	assert.Equal(t, []string{"Iam::Org " + org, "Iam::OrgMembers " + org}, parents,
		"the parent tenant isolation reads: the operator is among its org's members")

	first := f.grant(t)
	_, err := d.pass(t)
	require.NoError(t, err)
	ops, _ = operationsOf(t, fake, f.hosted)
	assert.Equal(t, []string{"iam:agent.configure", "iam:agent.create"}, ops, "a write")
	assert.Empty(t, outboxMarks(t, f.pool))

	second := f.replace(t, first, "grant-2", operator.OperationAgentActivate)
	_, err = d.pass(t)
	require.NoError(t, err)
	ops, _ = operationsOf(t, fake, f.hosted)
	assert.Equal(t, []string{"iam:agent.activate"}, ops, "a replacement is the new set alone")

	execSQL(t, f.pool, `UPDATE operator_delegations SET expires_at = now() - interval '1 second'`)
	execSQL(t, f.pool, `SELECT policy_outbox_mark('Iam::Operator', $1)`, f.hosted.String())
	_, err = d.pass(t)
	require.NoError(t, err)
	ops, _ = operationsOf(t, fake, f.hosted)
	assert.Equal(t, []string{"iam:agent.activate"}, ops,
		"a grant that lapsed and was not ended is fed as stored: the platform refuses it, not the service")

	f.end(t, second.ID)
	_, err = d.pass(t)
	require.NoError(t, err)
	ops, parents = operationsOf(t, fake, f.hosted)
	assert.Empty(t, ops, "an end delivers none")
	assert.Equal(t, []string{"Iam::Org " + org, "Iam::OrgMembers " + org}, parents, "and the operator stays")
}

func TestPolicyDelivery_EveryDelegableOperationIsDeliveredUnderTheNameTheManifestGivesIt(t *testing.T) {
	f, fake, d := seedDelegationFeed(t)
	all := make([]operator.Operation, len(delegableOperations))
	for i, op := range delegableOperations {
		parsed, err := operator.ParseOperation(op)
		require.NoError(t, err, "the platform accepts %s", op)
		all[i] = parsed
	}
	_, err := operator.ParseOperation("iam:agent-create")
	require.Error(t, err, "the control: a name the manifest does not list is refused")

	_, created, err := f.write("grant-all", nil, f.terms(t, []grn.GRN{f.controller}, all...))
	require.NoError(t, err)
	require.True(t, created)
	_, err = d.pass(t)
	require.NoError(t, err)

	ops, _ := operationsOf(t, fake, f.hosted)
	want := append([]string{}, delegableOperations...)
	sort.Strings(want)
	assert.Equal(t, want, ops)
}

func TestPolicyFeedStart_DeliversDelegationsThatExistedBefore(t *testing.T) {
	f := newDelegationFixture(t)
	fake, key := newFake(t)
	d := newDeliverer(t, f.pool, fake, key)
	f.grant(t)
	require.False(t, feedEnabled(t, f.pool))
	require.Empty(t, outboxMarks(t, f.pool), "the delegation was written with the feed off")

	startFeed(t, f.pool)
	_, err := d.pass(t)
	require.NoError(t, err)

	ops, _ := operationsOf(t, fake, f.hosted)
	assert.Equal(t, []string{"iam:agent.configure", "iam:agent.create"}, ops)
	other, _ := operationsOf(t, fake, f.other)
	assert.Empty(t, other, "the control: an operator with none is delivered with none")
}

func TestPolicyGate_AwaitEntityAnswersOnlyOnceTheDelegationIsInForce(t *testing.T) {
	f, fake, d := seedDelegationFeed(t)
	gate := runLoop(t, f.pool, d, 5*time.Second)
	time.Sleep(200 * time.Millisecond)
	entity := policyfeed.Entity{Type: "Iam::Operator", ID: f.hosted.String()}

	f.grant(t)
	require.Equal(t, []string{f.operatorMark()}, markKeys(outboxMarks(t, f.pool)),
		"the control: not delivered before the wait")

	require.NoError(t, gate.AwaitEntity(context.Background(), entity))

	ops, _ := operationsOf(t, fake, f.hosted)
	assert.Equal(t, []string{"iam:agent.configure", "iam:agent.create"}, ops,
		"the answer comes with the operations in force")
}

func TestPolicyGate_AwaitEntityAnswersUnavailableWhileTheDelegationIsNotApplied(t *testing.T) {
	f, fake, d := seedDelegationFeed(t)
	gate := runLoop(t, f.pool, d, 400*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	fake.mu.Lock()
	fake.fail = status.Error(codes.Unavailable, "policy service down")
	fake.mu.Unlock()
	entity := policyfeed.Entity{Type: "Iam::Operator", ID: f.hosted.String()}

	f.grant(t)
	err := gate.AwaitEntity(context.Background(), entity)

	require.ErrorIs(t, err, policyfeed.ErrNotApplied)
	assert.Equal(t, errorx.KindUnavailable, errorx.KindOf(err))
	assert.Equal(t, 1, countRows(t, f.pool, `SELECT count(*) FROM operator_delegations`), "the grant is stored")
	assert.Contains(t, outboxMarks(t, f.pool), f.operatorMark(), "its mark waits")
	assert.NoError(t, gate.AwaitEntity(context.Background(),
		policyfeed.Entity{Type: "Iam::Operator", ID: f.other.String()}),
		"the control: an operator with no mark is not waited for")

	fake.mu.Lock()
	fake.fail = nil
	fake.mu.Unlock()
	require.Eventually(t, func() bool { return len(outboxMarks(t, f.pool)) == 0 }, 5*time.Second, 20*time.Millisecond)
	assert.NoError(t, gate.AwaitEntity(context.Background(), entity), "and the wait is over")
}

// TestMigration000078_MarksEveryOperatorOfAFeedThatIsOn applies 000078
// over operators that exist: a feed that is on marks each of them, the
// one holding a delegation and the one holding none, since each gains
// its OrgMembers parent, and one that is off marks none, since its
// start marks every operator. The way down removes the trigger.
func TestMigration000078_MarksEveryOperatorOfAFeedThatIsOn(t *testing.T) {
	for _, on := range []bool{true, false} {
		name := "a feed that is off"
		schema := "migrate_000078_off"
		if on {
			name = "a feed that is on"
			schema = "migrate_000078_on"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := pool(t)
			m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = m.Close() })
			require.NoError(t, m.Migrate(77))
			if on {
				_, err = p.Exec(ctx, "UPDATE "+schema+".policy_feed SET enabled = true")
				require.NoError(t, err)
			}
			for _, q := range []string{
				`INSERT INTO ` + schema + `.orgs (grn, name, created_at, updated_at) VALUES ('` + outboxOrg + `', 'acme', now(), now())`,
				`INSERT INTO ` + schema + `.operators (grn, org_grn, registered_by, created_at, name) VALUES ('` + outboxOperator + `', '` + outboxOrg + `', '` + outboxUser + `', now(), 'op1')`,
				`INSERT INTO ` + schema + `.operators (grn, org_grn, registered_by, created_at, name) VALUES ('grn:acme/iam/operator/op2', '` + outboxOrg + `', '` + outboxUser + `', now(), 'op2')`,
				`INSERT INTO ` + schema + `.operator_delegations (id, org_grn, operator_grn, request_id, operations, expires_at, granted_by, granted_at)
					VALUES ('g-1', '` + outboxOrg + `', '` + outboxOperator + `', 'r-1', ARRAY['iam:agent.create'], now() + interval '1 day', '` + outboxUser + `', now())`,
			} {
				_, err = p.Exec(ctx, q)
				require.NoError(t, err, q)
			}
			_, err = p.Exec(ctx, "DELETE FROM "+schema+".policy_outbox")
			require.NoError(t, err)
			require.NoError(t, m.Migrate(78))

			want := 0
			if on {
				want = 1
			}
			for _, op := range []string{outboxOperator, "grn:acme/iam/operator/op2"} {
				assert.Equal(t, want, countRows(t, p,
					"SELECT count(*) FROM "+schema+".policy_outbox WHERE entity_type = 'Iam::Operator' AND entity_id = '"+op+"'"), op)
			}
			require.NoError(t, m.Migrate(77), "000078 rolls back")
			assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM pg_trigger WHERE tgname = 'policy_outbox_operator_delegations' AND tgrelid = '"+schema+".operator_delegations'::regclass"))
		})
	}
}

// TestPolicyFeedStart_WaitsOutAWriterOfADelegationThatReadTheFeedAsOff
// holds that start takes operator_delegations with the other source
// tables: a grant in flight when the feed is switched on is committed
// before the backfill reads, so a trigger that read the feed as off
// leaves no delegation the backfill cannot see.
func TestPolicyFeedStart_WaitsOutAWriterOfADelegationThatReadTheFeedAsOff(t *testing.T) {
	f := newDelegationFixture(t)
	p := f.pool
	ctx := context.Background()
	// Registered first so it runs last: Stop waits on the writer's
	// lock, which the rollback below releases.
	t.Cleanup(func() { _, _ = policyfeedpg.New(p).Stop(ctx) })
	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	_, err = tx.Exec(ctx, `INSERT INTO operator_delegations
		(id, org_grn, operator_grn, request_id, operations, expires_at, granted_by, granted_at)
		VALUES ('g-1', $1, $2, 'r-1', ARRAY['iam:agent.create'], now() + interval '1 day', $3, now())`,
		f.org.String(), f.hosted.String(), f.actor.String())
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
	}, 10*time.Second, 20*time.Millisecond, "start waits on the writer of a delegation")
	select {
	case err := <-started:
		require.Failf(t, "start returned while a writer held the table", "%v", err)
	default:
	}

	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-started)
	assert.Contains(t, markKeys(outboxMarks(t, p)), f.operatorMark(), "the operator that was granted is marked")
}
