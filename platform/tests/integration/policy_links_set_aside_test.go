package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
)

const (
	otherOrg      = "grn:root/iam/org/globex"
	otherUser     = "grn:globex/iam/user/pat"
	otherTeam     = "grn:globex/iam/group/eng"
	otherLinkMark = "Iam::RoleLink " + otherOrg + " " + otherUser
	otherLink     = "ADMIN " + otherUser + " " + otherOrg
)

// seedOtherOrg writes a second organization with its own member and a
// team, so a pass holds the work of two organizations.
func seedOtherOrg(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('` + otherOrg + `', 'globex', now(), now())`,
		`INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ('` + otherUser + `', 'pat', now(), now())`,
		`INSERT INTO org_members (org_grn, user_grn, role, created_at) VALUES ('` + otherOrg + `', '` + otherUser + `', 'admin', now())`,
		`INSERT INTO groups (grn, org_grn, name, created_at, updated_at) VALUES ('` + otherTeam + `', '` + otherOrg + `', 'eng', now(), now())`,
	} {
		execSQL(t, p, q)
	}
}

// setAside is one set-aside mark as an operator reads it from the
// outbox.
type setAside struct {
	code, reason string
	at           time.Time
}

// setAsideMarks reads every set-aside mark by its "Type id" key.
func setAsideMarks(t *testing.T, p *pgxpool.Pool) map[string]setAside {
	t.Helper()
	rows, err := p.Query(context.Background(), `
		SELECT entity_type || ' ' || entity_id, set_aside_code, set_aside_reason, set_aside_at
		FROM policy_outbox WHERE set_aside_at IS NOT NULL`)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string]setAside{}
	for rows.Next() {
		var key string
		var s setAside
		require.NoError(t, rows.Scan(&key, &s.code, &s.reason, &s.at))
		out[key] = s
	}
	require.NoError(t, rows.Err())
	return out
}

// seedTwoOrgs turns the feed on and seeds both organizations, which
// leaves the outbox holding both organizations' entities and links.
func seedTwoOrgs(t *testing.T) (*pgxpool.Pool, *fakePolicy, deliverer) {
	t.Helper()
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	startFeed(t, p)
	seedOutboxOrg(t, p)
	seedOtherOrg(t, p)
	require.Contains(t, outboxMarks(t, p), outboxLinkMark)
	require.Contains(t, outboxMarks(t, p), otherLinkMark)
	return p, fake, d
}

// changed reports whether an ApplyEntityChanges call of the fake
// carried the entity.
func changed(fake *fakePolicy, entity string) bool {
	for _, req := range fake.received() {
		if _, ok := changesByEntity(req)[entity]; ok {
			return true
		}
	}
	return false
}

func TestPolicyLinks_ALinkRefusedForGoodIsSetAsideAndTheRestOfThePassIsDelivered(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.InvalidArgument} {
		t.Run(code.String(), func(t *testing.T) {
			p, fake, d := seedTwoOrgs(t)
			fake.linkRefuseOrg(outboxOrg, status.Error(code, "refused for good"))

			result, err := d.pass(t)

			require.NoError(t, err, "a link refused for good does not fail the pass")
			require.Len(t, result.SetAside, 1)
			assert.Equal(t, outboxUser, result.SetAside[0].Link.User)
			assert.Equal(t, code.String(), result.SetAside[0].Code)
			// The accepted control: the other organization's link and
			// entities are delivered in the same pass.
			assert.Equal(t, []string{otherLink}, fake.links())
			assert.True(t, changed(fake, "Iam::Team "+otherTeam))
			assert.True(t, changed(fake, "Iam::Team "+outboxTeam), "the refused link's organization's entities too")

			// The refused links' marks stay, with the refusal an
			// operator reads: the organization's refusal reaches its
			// member's link and its groups' and project's scoped links
			// alike. Everything else is gone.
			orgLinkMarks := []string{outboxLinkMark,
				"Iam::ScopedRoleLink " + outboxTeam, "Iam::ScopedRoleLink " + outboxTeam2,
				"Iam::ScopedRoleLink " + outboxProject}
			marks := setAsideMarks(t, p)
			require.Contains(t, marks, outboxLinkMark)
			assert.Equal(t, code.String(), marks[outboxLinkMark].code)
			assert.Contains(t, marks[outboxLinkMark].reason, "refused for good")
			assert.WithinDuration(t, time.Now(), marks[outboxLinkMark].at, time.Minute)
			assert.Equal(t, orgLinkMarks, markKeys(outboxMarks(t, p)))

			// Not retried: the next pass reads nothing.
			result, err = d.pass(t)
			require.NoError(t, err)
			assert.Zero(t, result.Marks)
			assert.Empty(t, result.SetAside)
			assert.Equal(t, orgLinkMarks, markKeys(outboxMarks(t, p)))
		})
	}
}

func TestPolicyLinks_AFaultThatMayClearFailsThePassAndIsRetried(t *testing.T) {
	// The control of the test above: every code outside the permanent
	// list fails the pass, deletes no mark, sets none aside, and the
	// pass that follows the service's recovery delivers all of it.
	for _, code := range []codes.Code{
		codes.Unavailable, codes.Internal, codes.DeadlineExceeded,
		codes.FailedPrecondition, codes.Unauthenticated, codes.NotFound,
	} {
		t.Run(code.String(), func(t *testing.T) {
			p, fake, d := seedTwoOrgs(t)
			before := markKeys(outboxMarks(t, p))
			fake.linkRefuseOrg(outboxOrg, status.Error(code, "may clear"))

			_, err := d.pass(t)

			require.Error(t, err)
			assert.Equal(t, before, markKeys(outboxMarks(t, p)), "no mark is deleted")
			assert.Empty(t, setAsideMarks(t, p), "none is set aside")

			fake.linkRefuseOrg(outboxOrg, nil)
			result, err := d.pass(t)
			require.NoError(t, err, "the retry")
			assert.Empty(t, result.SetAside)
			assert.Equal(t, []string{otherLink, "MEMBER " + outboxUser + " " + outboxOrg}, fake.links())
			assert.Empty(t, outboxMarks(t, p))
		})
	}
}

func TestPolicyLinks_ARefusedWriteLeavesTheMemberNoLinkAndNotTheOldRole(t *testing.T) {
	p, fake, d := seedDelivered(t)
	// The role's new link is written and then refused at its activation,
	// for good. The old role's link is removed so the refusal denies and
	// does not decide on what the service last held.
	fake.linkFailOn("ActivatePolicy", status.Error(codes.PermissionDenied, "no writer"))

	execSQL(t, p, `UPDATE org_members SET role = 'admin'`)
	result, err := d.pass(t)

	require.NoError(t, err)
	require.Len(t, result.SetAside, 1)
	assert.Empty(t, fake.links(), "the member holds no role the platform did not give")
	assert.Contains(t, setAsideMarks(t, p), outboxLinkMark)
}

func TestPolicyLinks_AWriteOfTheMembershipClearsTheSetAside(t *testing.T) {
	p, fake, d := seedDelivered(t)
	fake.linkRefuseOrg(outboxOrg, status.Error(codes.PermissionDenied, "refused for good"))
	execSQL(t, p, `UPDATE org_members SET role = 'admin'`)
	_, err := d.pass(t)
	require.NoError(t, err)
	require.Contains(t, setAsideMarks(t, p), outboxLinkMark, "the case needs a set-aside link")

	// The service is repaired and the membership is written again.
	fake.linkRefuseOrg(outboxOrg, nil)
	execSQL(t, p, `UPDATE org_members SET role = 'owner'`)

	assert.Empty(t, setAsideMarks(t, p), "the write marks it anew")
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Equal(t, []string{"OWNER " + outboxUser + " " + outboxOrg}, fake.links())
	assert.Empty(t, outboxMarks(t, p))

	// `garam policy-feed start` clears it too: the stop is the other
	// control, which empties the outbox.
	fake.linkRefuseOrg(outboxOrg, status.Error(codes.PermissionDenied, "refused for good"))
	execSQL(t, p, `UPDATE org_members SET role = 'member'`)
	_, err = d.pass(t)
	require.NoError(t, err)
	require.Contains(t, setAsideMarks(t, p), outboxLinkMark)
	fake.linkRefuseOrg(outboxOrg, nil)
	startFeed(t, p)
	assert.Empty(t, setAsideMarks(t, p), "start marks every membership anew")
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Equal(t, []string{memberLink}, fake.links())
}

func TestPolicyGate_AwaitAnswersASetAsideLinkAtOnceBesideTheOrdinaryAnswer(t *testing.T) {
	p, fake, d := seedTwoOrgs(t)
	_, err := d.pass(t)
	require.NoError(t, err)
	fake.linkRefuseOrg(outboxOrg, status.Error(codes.PermissionDenied, "refused for good"))
	bound := 10 * time.Second
	gate := runLoop(t, p, d, bound)
	time.Sleep(200 * time.Millisecond)

	execSQL(t, p, `UPDATE org_members SET role = 'owner' WHERE org_grn = $1`, outboxOrg)
	execSQL(t, p, `UPDATE org_members SET role = 'owner' WHERE org_grn = $1`, otherOrg)

	// The ordinary answer: the other organization's link is applied.
	require.NoError(t, gate.Await(context.Background(), otherOrg, otherUser))
	assert.Contains(t, fake.links(), "OWNER "+otherUser+" "+otherOrg)

	// The refused one answers as not applied, at once and not at the
	// bound, and says the link is set aside.
	start := time.Now()
	err = gate.Await(context.Background(), outboxOrg, outboxUser)
	require.ErrorIs(t, err, policyfeed.ErrLinkSetAside)
	assert.NotErrorIs(t, err, policyfeed.ErrNotApplied)
	assert.Equal(t, errorx.KindUnavailable, errorx.KindOf(err))
	reason, ok := errorx.ReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, policyfeed.ReasonChangeRefused, reason.Code)
	assert.Less(t, time.Since(start), bound/2, "it did not wait out the bound")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM org_members WHERE role = 'member'`),
		"the change is stored")
	// An organization wait covers the set-aside link of its member.
	assert.ErrorIs(t, gate.Await(context.Background(), outboxOrg, ""), policyfeed.ErrLinkSetAside)
}

func TestPolicyFeed_StandingIsReadFromTheMetrics(t *testing.T) {
	p, fake, d := seedTwoOrgs(t)
	store := policyfeedpg.NewStore(p)
	last := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	reg := prometheus.NewRegistry()
	require.NoError(t, policyfeed.NewCollector(reg, store, func() time.Time { return last }))

	// Pending: the seeded marks wait for a pass.
	pending := len(outboxMarks(t, p))
	got := gatherStanding(t, reg)
	assert.EqualValues(t, pending, got[`marks{state="pending"}`])
	assert.EqualValues(t, 0, got[`marks{state="set_aside"}`])
	assert.EqualValues(t, last.Unix(), got["last_success"])

	// Set aside: the refused link leaves the pending count and joins the
	// other.
	fake.linkRefuseOrg(outboxOrg, status.Error(codes.PermissionDenied, "refused for good"))
	_, err := d.pass(t)
	require.NoError(t, err)
	got = gatherStanding(t, reg)
	assert.EqualValues(t, 0, got[`marks{state="pending"}`])
	assert.EqualValues(t, 4, got[`marks{state="set_aside"}`], "the member's link and the three scoped links")
}

func TestPolicyFeed_StandingLeavesOutALastSuccessThatNeverCame(t *testing.T) {
	p := pool(t)
	reg := prometheus.NewRegistry()
	require.NoError(t, policyfeed.NewCollector(reg, policyfeedpg.NewStore(p), nil))

	got := gatherStanding(t, reg)

	assert.Contains(t, got, `marks{state="pending"}`)
	assert.NotContains(t, got, "last_success")
}

// gatherStanding reads the feed's metrics off the registry as
// "marks{state=...}" and "last_success" keys.
func gatherStanding(t *testing.T, reg *prometheus.Registry) map[string]float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	out := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			switch f.GetName() {
			case policyfeed.MarksMetric:
				out[`marks{state="`+m.GetLabel()[0].GetValue()+`"}`] = m.GetGauge().GetValue()
			case policyfeed.LastSuccessMetric:
				out["last_success"] = m.GetGauge().GetValue()
			}
		}
	}
	return out
}

// TestMigration000076_RecordsASetAsideLinkAndTheWayDownMakesItPending
// applies 000076 over marks that exist. They gain no refusal; a
// set-aside link, and only a link, can be recorded; a write of the mark
// clears it. The way down keeps every mark.
func TestMigration000076_RecordsASetAsideLinkAndTheWayDownMakesItPending(t *testing.T) {
	const schema = "migrate_000076"
	ctx := context.Background()
	p := pool(t)
	m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(75))
	for _, q := range []string{
		`INSERT INTO ` + schema + `.policy_outbox (entity_type, entity_id) VALUES ('Iam::RoleLink', 'a b')`,
		`INSERT INTO ` + schema + `.policy_outbox (entity_type, entity_id) VALUES ('Iam::Org', 'a')`,
	} {
		_, err = p.Exec(ctx, q)
		require.NoError(t, err, q)
	}
	require.NoError(t, m.Migrate(76))

	assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM "+schema+".policy_outbox WHERE set_aside_at IS NOT NULL"),
		"no stored mark is set aside by the migration")
	_, err = p.Exec(ctx, `UPDATE `+schema+`.policy_outbox SET set_aside_code = 'PermissionDenied',
		set_aside_reason = 'r', set_aside_at = now() WHERE entity_type = 'Iam::RoleLink'`)
	require.NoError(t, err, "a link can be set aside")
	_, err = p.Exec(ctx, `UPDATE `+schema+`.policy_outbox SET set_aside_code = 'PermissionDenied',
		set_aside_reason = 'r', set_aside_at = now() WHERE entity_type = 'Iam::Org'`)
	require.Error(t, err, "an entity cannot")
	_, err = p.Exec(ctx, `UPDATE `+schema+`.policy_outbox SET set_aside_code = NULL
		WHERE entity_type = 'Iam::RoleLink'`)
	require.Error(t, err, "a record is all of its three columns or none")

	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SET LOCAL search_path = `+schema)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `SELECT policy_outbox_mark('Iam::RoleLink', 'a b')`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM "+schema+".policy_outbox WHERE set_aside_at IS NOT NULL"),
		"a write of the mark clears the record")

	_, err = p.Exec(ctx, `UPDATE `+schema+`.policy_outbox SET set_aside_code = 'PermissionDenied',
		set_aside_reason = 'r', set_aside_at = now() WHERE entity_type = 'Iam::RoleLink'`)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(75), "000076 rolls back")
	assert.Equal(t, 2, countRows(t, p, "SELECT count(*) FROM "+schema+".policy_outbox"),
		"the way down keeps every mark")
}
