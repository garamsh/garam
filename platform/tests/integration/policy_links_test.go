package integration_test

import (
	"context"
	"log/slog"
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

const outboxUser2 = "grn:acme/iam/user/kim"

// memberLink is the link the seeded membership is.
const memberLink = "MEMBER " + outboxUser + " " + outboxOrg

// seedDelivered turns the feed on, seeds the organization and delivers
// everything, so the service holds the seeded member's link and the
// outbox is empty.
func seedDelivered(t *testing.T) (*pgxpool.Pool, *fakePolicy, deliverer) {
	t.Helper()
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	startFeed(t, p)
	seedOutboxOrg(t, p)
	_, err := d.pass(t)
	require.NoError(t, err)
	require.Equal(t, []string{memberLink}, fake.links(), "the case needs the seeded link delivered")
	require.Empty(t, outboxMarks(t, p), "the case needs an empty outbox")
	return p, fake, d
}

func TestPolicyLinks_EachMembershipEventIsTheLinkWrite(t *testing.T) {
	tests := []struct {
		name string
		sql  []string
		want []string
		// policies is how many policies the service holds after: a role
		// that changed leaves one, not the old and the new.
		policies int
	}{
		{"a member joins",
			[]string{`INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ('` + outboxUser2 + `', 'kim', now(), now())`,
				`INSERT INTO org_members (org_grn, user_grn, role, created_at) VALUES ('` + outboxOrg + `', '` + outboxUser2 + `', 'admin', now())`},
			[]string{"ADMIN " + outboxUser2 + " " + outboxOrg, memberLink}, 2},
		{"a role changes",
			[]string{`UPDATE org_members SET role = 'admin'`},
			[]string{"ADMIN " + outboxUser + " " + outboxOrg}, 1},
		{"a role changes to the owner's",
			[]string{`UPDATE org_members SET role = 'owner'`},
			[]string{"OWNER " + outboxUser + " " + outboxOrg}, 1},
		{"a membership ends",
			[]string{`DELETE FROM org_members`},
			nil, 0},
		{"a user is deleted and takes its membership",
			[]string{`DELETE FROM users WHERE grn = '` + outboxUser + `'`},
			nil, 0},
		{"an organization is deleted and takes its memberships",
			[]string{`DELETE FROM orgs WHERE grn = '` + outboxOrg + `'`},
			nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, fake, d := seedDelivered(t)

			for _, q := range tt.sql {
				execSQL(t, p, q)
			}
			_, err := d.pass(t)
			require.NoError(t, err)

			assert.Equal(t, tt.want, fake.links())
			fake.mu.Lock()
			assert.Len(t, fake.policies, tt.policies)
			fake.mu.Unlock()
			assert.Empty(t, outboxMarks(t, p), "the link marks are delivered and gone")
		})
	}
}

func TestPolicyLinks_AMarkDeliveredAgainIsNoFault(t *testing.T) {
	p, fake, d := seedDelivered(t)

	// The same link written again: CreatePolicy answers ALREADY_EXISTS
	// and the pass goes on to activate it.
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::RoleLink', $1)`, outboxOrg+" "+outboxUser)
	_, err := d.pass(t)
	require.NoError(t, err)
	assert.Equal(t, []string{memberLink}, fake.links())

	// The same removal twice: DeletePolicy answers NOT_FOUND the second
	// time. The control is the first, which removed the link.
	execSQL(t, p, `DELETE FROM org_members`)
	_, err = d.pass(t)
	require.NoError(t, err)
	require.Empty(t, fake.links(), "the control: the removal took effect")
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::RoleLink', $1)`, outboxOrg+" "+outboxUser)
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Empty(t, fake.links())
	assert.Empty(t, outboxMarks(t, p))
}

func TestPolicyLinks_AFailedWriteKeepsTheMembershipAndTheMarkAndAppliesLater(t *testing.T) {
	p, fake, d := seedDelivered(t)
	fake.linkFail(status.Error(codes.Unavailable, "policy service down"))

	execSQL(t, p, `DELETE FROM org_members`)
	_, err := d.pass(t)

	// The platform's table is the truth: the membership is gone there
	// and its mark waits. The service still holds the link it had, which
	// is the one place the two disagree, and only until the next pass.
	require.Error(t, err)
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM org_members`))
	assert.Equal(t, map[string]int64{outboxLinkMark: 1, "Iam::User " + outboxUser: 1}, outboxMarks(t, p))
	assert.Equal(t, []string{memberLink}, fake.links(), "the service has not applied the removal")

	fake.linkFail(nil)
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Empty(t, fake.links(), "the next pass applies it")
	assert.Empty(t, outboxMarks(t, p))
}

func TestPolicyLinks_ARoleChangeNeverLeavesAMemberWithNoLink(t *testing.T) {
	p, fake, d := seedDelivered(t)
	// Only the removal of the old role's link fails, so the pass
	// reaches it after the new link is written. Written first, the
	// member holds both roles at that moment; removed first, the
	// failure would leave the old one alone and the new one unwritten.
	fake.linkFailOn("DeletePolicy", status.Error(codes.Unavailable, "policy service down"))

	execSQL(t, p, `UPDATE org_members SET role = 'admin'`)
	_, err := d.pass(t)

	require.Error(t, err)
	assert.Equal(t, []string{"ADMIN " + outboxUser + " " + outboxOrg, memberLink}, fake.links(),
		"the new link is in force before the old one is removed")

	fake.linkFail(nil)
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.Equal(t, []string{"ADMIN " + outboxUser + " " + outboxOrg}, fake.links())
}

// service runs the delivery loop against the fake, as serve api does,
// with a gate over it. The pass pace is an hour, so only a wake runs a
// pass the test did not drive.
func runLoop(t *testing.T, p *pgxpool.Pool, d deliverer, bound time.Duration) policyfeed.Gate {
	t.Helper()
	store := policyfeedpg.NewStore(p)
	svc := policyfeed.NewService(store, d.sender, slog.New(slog.DiscardHandler),
		policyfeed.Config{Poll: time.Hour, MaxBackoff: 100 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = svc.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return policyfeed.NewGate(store, svc, bound)
}

func TestPolicyGate_AwaitAnswersOnlyOnceTheLinkIsInForce(t *testing.T) {
	p, fake, d := seedDelivered(t)
	gate := runLoop(t, p, d, 5*time.Second)
	// Let the loop's first pass finish, so it is in its hour's wait.
	time.Sleep(200 * time.Millisecond)

	execSQL(t, p, `INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ('`+outboxUser2+`', 'kim', now(), now())`)
	execSQL(t, p, `INSERT INTO org_members (org_grn, user_grn, role, created_at) VALUES ($1, $2, 'admin', now())`,
		outboxOrg, outboxUser2)
	require.NotContains(t, fake.links(), "ADMIN "+outboxUser2+" "+outboxOrg, "the control: not in force before the wait")

	require.NoError(t, gate.Await(context.Background(), outboxOrg, outboxUser2))

	assert.Contains(t, fake.links(), "ADMIN "+outboxUser2+" "+outboxOrg, "the request answers with the link in force")
}

func TestPolicyGate_AwaitAnswersUnavailableWhileTheStoredChangeIsNotApplied(t *testing.T) {
	p, fake, d := seedDelivered(t)
	gate := runLoop(t, p, d, 400*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	fake.linkFail(status.Error(codes.Unavailable, "policy service down"))

	execSQL(t, p, `DELETE FROM org_members`)
	err := gate.Await(context.Background(), outboxOrg, outboxUser)

	require.Error(t, err)
	assert.Equal(t, errorx.KindUnavailable, errorx.KindOf(err))
	reason, ok := errorx.ReasonOf(err)
	require.True(t, ok)
	assert.Equal(t, policyfeed.ReasonChangeNotApplied, reason.Code)
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM org_members`), "the change is stored")
	assert.Contains(t, outboxMarks(t, p), outboxLinkMark, "its mark waits")

	fake.linkFail(nil)
	require.Eventually(t, func() bool { return len(fake.links()) == 0 }, 5*time.Second, 20*time.Millisecond,
		"the loop applies it once the service answers")
	assert.NoError(t, gate.Await(context.Background(), outboxOrg, outboxUser), "and the wait is over")
}

func TestPolicyGate_AnOrganizationWaitCoversEveryMemberOfIt(t *testing.T) {
	p, fake, d := seedDelivered(t)
	gate := runLoop(t, p, d, 400*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	fake.linkFail(status.Error(codes.Unavailable, "policy service down"))

	execSQL(t, p, `DELETE FROM orgs`)

	// The user's own wait and the organization's both see the mark; a
	// wait on another organization sees none, which is the control.
	assert.Error(t, gate.Await(context.Background(), outboxOrg, ""))
	assert.NoError(t, gate.Await(context.Background(), "grn:root/iam/org/other", ""))
}

func TestPolicyFeedStart_BackfillsMembersCreatedBeforeItAndIsIdempotent(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	require.False(t, feedEnabled(t, p))
	seedOutboxOrg(t, p)
	execSQL(t, p, `INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ($1, 'kim', now(), now())`, outboxUser2)
	execSQL(t, p, `INSERT INTO org_members (org_grn, user_grn, role, created_at) VALUES ($1, $2, 'owner', now())`,
		outboxOrg, outboxUser2)
	require.Empty(t, outboxMarks(t, p), "the members predate the feed")

	want := []string{memberLink, "OWNER " + outboxUser2 + " " + outboxOrg}
	for round := range 2 {
		startFeed(t, p)
		_, err := d.pass(t)
		require.NoError(t, err, "round %d", round)

		assert.Equal(t, want, fake.links(), "round %d", round)
		fake.mu.Lock()
		assert.Len(t, fake.policies, 2, "round %d: a second start writes no second policy", round)
		fake.mu.Unlock()
		assert.Empty(t, outboxMarks(t, p), "round %d", round)
	}
}

// TestMigration000075_MarksTheMembershipsOfAFeedThatIsOn applies 000075
// over memberships that exist. With the feed on each gains its link
// mark; with it off none does, and `policy-feed start` marks them later
// (the case above). The way down removes the marks and the trigger.
func TestMigration000075_MarksTheMembershipsOfAFeedThatIsOn(t *testing.T) {
	for _, on := range []bool{true, false} {
		name := "a feed that is off"
		if on {
			name = "a feed that is on"
		}
		t.Run(name, func(t *testing.T) {
			schema := "migrate_000075_off"
			if on {
				schema = "migrate_000075_on"
			}
			ctx := context.Background()
			p := pool(t)
			m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = m.Close() })
			require.NoError(t, m.Migrate(74))
			if on {
				_, err = p.Exec(ctx, "UPDATE "+schema+".policy_feed SET enabled = true")
				require.NoError(t, err)
			}
			for _, q := range []string{
				`INSERT INTO ` + schema + `.orgs (grn, name, created_at, updated_at) VALUES ('` + outboxOrg + `', 'acme', now(), now())`,
				`INSERT INTO ` + schema + `.users (grn, nickname, created_at, updated_at) VALUES ('` + outboxUser + `', 'lee', now(), now())`,
				`INSERT INTO ` + schema + `.org_members (org_grn, user_grn, role, created_at) VALUES ('` + outboxOrg + `', '` + outboxUser + `', 'member', now())`,
			} {
				_, err = p.Exec(ctx, q)
				require.NoError(t, err, q)
			}
			require.NoError(t, m.Migrate(75))

			wantLinks := 0
			if on {
				wantLinks = 1
			}
			assert.Equal(t, wantLinks, countRows(t, p, "SELECT count(*) FROM "+schema+".policy_outbox WHERE entity_type = 'Iam::RoleLink'"))

			require.NoError(t, m.Migrate(74), "000075 rolls back")
			assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM "+schema+".policy_outbox WHERE entity_type = 'Iam::RoleLink'"))
		})
	}
}
