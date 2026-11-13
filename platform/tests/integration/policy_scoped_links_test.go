package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// The identifiers ADR-0163 item 2 states as literals, for the seeded
// group eng and the seeded project web. The platform and the policy
// service must derive the same ones.
const (
	teamAdminPolicy     = "grn:acme/iam/policy/link-scoped-admin-057a36867e8b5d54bdfba284ee2f3a02"
	projectMemberPolicy = "grn:acme/iam/policy/link-scoped-member-0e9bfafcc0092e41678c97122d4c76be"
)

// scopedLink is a scoped role link as "ROLE owner owner": the owner is
// both principal and resource.
func scopedLink(role, owner string) string { return role + " " + owner + " " + owner }

// policyNames is the names of the policies the fake holds, active or
// not.
func policyNames(f *fakePolicy) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for name := range f.policies {
		out = append(out, name)
	}
	return out
}

func TestPolicyScopedLinks_EachRoleEventIsTheLinkWriteOrRemoval(t *testing.T) {
	tests := []struct {
		name string
		sql  []string
		want []string
	}{
		{"a team is given a role",
			[]string{`UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '` + outboxTeam + `'`},
			[]string{memberLink, scopedLink("ADMIN", outboxTeam)}},
		{"a project is given a role",
			[]string{`UPDATE projects SET roles = ARRAY['member']`},
			[]string{memberLink, scopedLink("MEMBER", outboxProject)}},
		{"a team holds two roles",
			[]string{`UPDATE groups SET roles = ARRAY['member','owner'] WHERE grn = '` + outboxTeam + `'`},
			[]string{memberLink, scopedLink("MEMBER", outboxTeam), scopedLink("OWNER", outboxTeam)}},
		{"a role named twice is one link",
			[]string{`UPDATE groups SET roles = ARRAY['admin','admin'] WHERE grn = '` + outboxTeam + `'`},
			[]string{memberLink, scopedLink("ADMIN", outboxTeam)}},
		{"a role the platform does not know is no link",
			[]string{`UPDATE groups SET roles = ARRAY['wizard'] WHERE grn = '` + outboxTeam + `'`},
			[]string{memberLink}},
		{"a team's role changes",
			[]string{`UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '` + outboxTeam + `'`, `PASS`,
				`UPDATE groups SET roles = ARRAY['member'] WHERE grn = '` + outboxTeam + `'`},
			[]string{memberLink, scopedLink("MEMBER", outboxTeam)}},
		{"a team's role is removed",
			[]string{`UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '` + outboxTeam + `'`, `PASS`,
				`UPDATE groups SET roles = '{}' WHERE grn = '` + outboxTeam + `'`},
			[]string{memberLink}},
		{"a team with a role is deleted",
			[]string{`UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '` + outboxTeam + `'`, `PASS`,
				`DELETE FROM groups WHERE grn = '` + outboxTeam + `'`},
			[]string{memberLink}},
		{"a project with a role is deleted",
			[]string{`UPDATE projects SET roles = ARRAY['member']`, `PASS`,
				`DELETE FROM projects`},
			[]string{memberLink}},
		{"a team created with a role",
			[]string{`INSERT INTO groups (grn, org_grn, name, roles, created_at, updated_at)
				VALUES ('grn:acme/iam/group/new', '` + outboxOrg + `', 'new', ARRAY['owner'], now(), now())`},
			[]string{memberLink, scopedLink("OWNER", "grn:acme/iam/group/new")}},
		{"an organization with roles on its team and project is deleted",
			[]string{`UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '` + outboxTeam + `'`,
				`UPDATE projects SET roles = ARRAY['member']`, `PASS`,
				`DELETE FROM orgs WHERE grn = '` + outboxOrg + `'`},
			nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, fake, d := seedDelivered(t)

			for _, q := range tt.sql {
				if q == "PASS" {
					_, err := d.pass(t)
					require.NoError(t, err)
					continue
				}
				execSQL(t, p, q)
			}
			_, err := d.pass(t)
			require.NoError(t, err)

			assert.ElementsMatch(t, tt.want, fake.links())
			assert.Len(t, policyNames(fake), len(tt.want), "no link outlives its role")
			assert.Empty(t, outboxMarks(t, p), "the marks are delivered and gone")
		})
	}
}

// TestPolicyScopedLinks_AGroupMemberIsAllowedByTheLinkTheServiceReceived
// reads what the fake received, the two things the service decides
// from: the link, named as ADR-0163 names it, whose principal and
// resource are the group and whose role is the one the group holds; and
// the member, which is a child of that group. The service's own
// evaluation is not run here.
func TestPolicyScopedLinks_AGroupMemberIsAllowedByTheLinkTheServiceReceived(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		owner     string
		ownerType string
		role      policyv1.Role
		policy    string
	}{
		{"a group", `UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '` + outboxTeam + `'`,
			outboxTeam, "Iam::Team", policyv1.Role_ROLE_ADMIN, teamAdminPolicy},
		{"a project", `UPDATE projects SET roles = ARRAY['member']`,
			outboxProject, "Iam::Project", policyv1.Role_ROLE_MEMBER, projectMemberPolicy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, fake, d := seedDelivered(t)
			// The owner and its member reach the service as entities
			// in a pass of their own, as they do when the role is
			// given later than the group.
			execSQL(t, p, `SELECT policy_outbox_mark('Iam::User', $1)`, outboxUser)
			execSQL(t, p, tt.sql)
			_, err := d.pass(t)
			require.NoError(t, err)

			fake.mu.Lock()
			link, ok := fake.policies[tt.policy]
			fake.mu.Unlock()
			require.True(t, ok, "the policy the ADR names is the one written; held: %v", policyNames(fake))
			assert.True(t, link.active, "the link is in force")
			req := link.req
			assert.Equal(t, "grn:root/iam/org/acme", req.GetOrg())
			rl := req.GetBody().GetRoleLink()
			assert.Equal(t, tt.role, rl.GetRole())
			assert.Equal(t, policyv1.RoleScope_ROLE_SCOPE_TEAM_OR_PROJECT, rl.GetScope())
			assert.Equal(t, tt.ownerType, rl.GetPrincipal().GetType())
			assert.Equal(t, tt.owner, rl.GetPrincipal().GetId())
			assert.Equal(t, tt.ownerType, rl.GetResource().GetType())
			assert.Equal(t, tt.owner, rl.GetResource().GetId())

			member := changed(fake, "Iam::User "+outboxUser)
			require.True(t, member, "the member was delivered")
			var parents []string
			for _, r := range fake.received() {
				c, ok := changesByEntity(r)["Iam::User "+outboxUser]
				if !ok {
					continue
				}
				parents = nil
				for _, pr := range c.GetUpsert().GetParents() {
					parents = append(parents, pr.GetType()+" "+pr.GetId())
				}
			}
			assert.Contains(t, parents, tt.ownerType+" "+tt.owner, "the member is in the group of the link")
		})
	}
}

func TestPolicyScopedLinks_ARoleChangeNeverLeavesAGroupWithNoLink(t *testing.T) {
	p, fake, d := seedDelivered(t)
	execSQL(t, p, `UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '`+outboxTeam+`'`)
	_, err := d.pass(t)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{memberLink, scopedLink("ADMIN", outboxTeam)}, fake.links())

	// Only a removal fails. The pass writes the held role first, so the
	// group holds both for as long as the removal fails and never none.
	fake.linkFailOn("DeletePolicy", status.Error(codes.Unavailable, "policy service down"))
	execSQL(t, p, `UPDATE groups SET roles = ARRAY['member'] WHERE grn = '`+outboxTeam+`'`)
	_, err = d.pass(t)

	require.Error(t, err)
	assert.ElementsMatch(t, []string{memberLink, scopedLink("ADMIN", outboxTeam), scopedLink("MEMBER", outboxTeam)},
		fake.links(), "the new link is in force before the old one is removed")
	assert.Equal(t, []string{"Iam::ScopedRoleLink " + outboxTeam}, markKeys(outboxMarks(t, p)),
		"its mark waits")

	fake.linkFail(nil)
	_, err = d.pass(t)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{memberLink, scopedLink("MEMBER", outboxTeam)}, fake.links())
}

func TestPolicyScopedLinks_AMarkDeliveredAgainIsNoFault(t *testing.T) {
	p, fake, d := seedDelivered(t)
	execSQL(t, p, `UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '`+outboxTeam+`'`)
	_, err := d.pass(t)
	require.NoError(t, err)

	// The link written again: CreatePolicy answers ALREADY_EXISTS and
	// the pass goes on to activate it. The roles not held are removed
	// again, which the service answers with NOT_FOUND.
	execSQL(t, p, `SELECT policy_outbox_mark('Iam::ScopedRoleLink', $1)`, outboxTeam)
	_, err = d.pass(t)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{memberLink, scopedLink("ADMIN", outboxTeam)}, fake.links())
	assert.Empty(t, outboxMarks(t, p))
}

func TestPolicyScopedLinks_ALinkRefusedForGoodIsSetAsideAndTheOthersAreDelivered(t *testing.T) {
	for _, code := range []codes.Code{codes.PermissionDenied, codes.InvalidArgument} {
		t.Run(code.String(), func(t *testing.T) {
			p, fake, d := seedDelivered(t)
			execSQL(t, p, `UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '`+outboxTeam+`'`)
			execSQL(t, p, `UPDATE projects SET roles = ARRAY['member']`)
			fake.linkRefuseOwner(outboxTeam, status.Error(code, "reserved identifier"))

			result, err := d.pass(t)

			require.NoError(t, err, "a link refused for good does not fail the pass")
			require.Len(t, result.ScopedSetAside, 1, "the project's link is not refused")
			assert.Equal(t, code.String(), result.ScopedSetAside[0].Code)
			assert.Contains(t, result.ScopedSetAside[0].Reason, "reserved identifier")
			marks := setAsideMarks(t, p)
			require.Contains(t, marks, "Iam::ScopedRoleLink "+outboxTeam)
			assert.NotContains(t, marks, "Iam::ScopedRoleLink "+outboxProject)
			assert.Equal(t, code.String(), marks["Iam::ScopedRoleLink "+outboxTeam].code)
			assert.WithinDuration(t, time.Now(), marks["Iam::ScopedRoleLink "+outboxTeam].at, time.Minute)
			assert.ElementsMatch(t, []string{memberLink, scopedLink("MEMBER", outboxProject)}, fake.links(),
				"the other owner's link is delivered beside the refused one")
			assert.NotContains(t, outboxMarks(t, p), "Iam::ScopedRoleLink "+outboxProject)

			// Not retried: the next pass reads nothing.
			result, err = d.pass(t)
			require.NoError(t, err)
			assert.Zero(t, result.Marks)

			// A write of the roles clears it, and the link is written.
			fake.linkRefuseOwner(outboxTeam, nil)
			execSQL(t, p, `UPDATE groups SET roles = ARRAY['owner'] WHERE grn = '`+outboxTeam+`'`)
			_, err = d.pass(t)
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{memberLink, scopedLink("OWNER", outboxTeam),
				scopedLink("MEMBER", outboxProject)}, fake.links())
			assert.NotContains(t, setAsideMarks(t, p), "Iam::ScopedRoleLink "+outboxTeam)
		})
	}
}

// setAsideTeam gives the seeded group a link that is written and then
// refused for good, which sets the group's mark aside.
func setAsideTeam(t *testing.T) (*pgxpool.Pool, *fakePolicy, deliverer) {
	t.Helper()
	p, fake, d := seedDelivered(t)
	execSQL(t, p, `UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '`+outboxTeam+`'`)
	_, err := d.pass(t)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{memberLink, scopedLink("ADMIN", outboxTeam)}, fake.links(),
		"the case needs the link written")

	fake.linkRefuseOwner(outboxTeam, status.Error(codes.PermissionDenied, "reserved identifier"))
	execSQL(t, p, `UPDATE groups SET roles = ARRAY['member'] WHERE grn = '`+outboxTeam+`'`)
	result, err := d.pass(t)
	require.NoError(t, err)
	require.Len(t, result.ScopedSetAside, 1)
	return p, fake, d
}

// TestPolicyScopedLinks_ALinkRefusedAfterItWasWrittenRemovesAllThree starts
// from a link that is in force: the refusal of the next one removes what
// the owner held, so no role outlives the refusal (ADR-0162 item 4).
func TestPolicyScopedLinks_ALinkRefusedAfterItWasWrittenRemovesAllThree(t *testing.T) {
	p, fake, _ := setAsideTeam(t)

	assert.Equal(t, []string{memberLink}, fake.links(), "the written link is removed")
	assert.Len(t, policyNames(fake), 1, "and no policy of the owner is left, active or not")
	assert.Contains(t, setAsideMarks(t, p), "Iam::ScopedRoleLink "+outboxTeam)
}

// TestPolicyScopedGate_AwaitScopedOnASetAsideMarkAnswersAtOnce reads the
// set-aside branch of the store over Postgres: the wait on a refused
// owner is answered with its own error well inside the bound, and the
// wait on an owner whose link is pending runs to the bound.
func TestPolicyScopedGate_AwaitScopedOnASetAsideMarkAnswersAtOnce(t *testing.T) {
	p, fake, d := setAsideTeam(t)
	const bound = 2 * time.Second
	gate := runLoop(t, p, d, bound)
	time.Sleep(200 * time.Millisecond)
	fake.linkFail(status.Error(codes.Unavailable, "policy service down"))
	execSQL(t, p, `UPDATE projects SET roles = ARRAY['member']`)

	start := time.Now()
	err := gate.AwaitScoped(context.Background(), outboxTeam)
	elapsed := time.Since(start)

	assert.ErrorIs(t, err, policyfeed.ErrLinkSetAside)
	assert.Less(t, elapsed, bound/2, "a refused link is not waited for")

	err = gate.AwaitScoped(context.Background(), outboxProject)
	assert.ErrorIs(t, err, policyfeed.ErrNotApplied, "the control: a pending link waits for the bound")
}

func TestPolicyScopedGate_AwaitScopedAnswersOnlyOnceTheLinkIsInForce(t *testing.T) {
	p, fake, d := seedDelivered(t)
	gate := runLoop(t, p, d, 5*time.Second)
	time.Sleep(200 * time.Millisecond)

	// The service is down while the role is stored, so the loop cannot
	// have delivered it whatever it does: the control does not race it.
	fake.linkFail(status.Error(codes.Unavailable, "policy service down"))
	execSQL(t, p, `UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '`+outboxTeam+`'`)
	require.NotContains(t, fake.links(), scopedLink("ADMIN", outboxTeam), "the control: not in force before the wait")
	fake.linkFail(nil)

	require.NoError(t, gate.AwaitScoped(context.Background(), outboxTeam))

	assert.Contains(t, fake.links(), scopedLink("ADMIN", outboxTeam))
}

func TestPolicyScopedGate_AwaitScopedAnswersUnavailableWhileTheLinkIsNotApplied(t *testing.T) {
	p, fake, d := seedDelivered(t)
	gate := runLoop(t, p, d, 400*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	fake.linkFail(status.Error(codes.Unavailable, "policy service down"))

	execSQL(t, p, `UPDATE groups SET roles = ARRAY['admin'] WHERE grn = '`+outboxTeam+`'`)
	err := gate.AwaitScoped(context.Background(), outboxTeam)

	require.Error(t, err)
	assert.ErrorIs(t, err, policyfeed.ErrNotApplied)
	assert.Contains(t, outboxMarks(t, p), "Iam::ScopedRoleLink "+outboxTeam, "its mark waits")
	assert.NoError(t, gate.AwaitScoped(context.Background(), outboxProject),
		"the control: another owner's wait sees no mark")
}

func TestPolicyFeedStart_WritesTheLinksOfRolesThatExist(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	require.False(t, feedEnabled(t, p))
	seedOutboxOrg(t, p)
	execSQL(t, p, `UPDATE groups SET roles = ARRAY['admin'] WHERE grn = $1`, outboxTeam)
	execSQL(t, p, `UPDATE projects SET roles = ARRAY['member']`)
	require.Empty(t, outboxMarks(t, p), "the roles predate the feed")

	want := []string{memberLink, scopedLink("ADMIN", outboxTeam), scopedLink("MEMBER", outboxProject)}
	for round := range 2 {
		startFeed(t, p)
		_, err := d.pass(t)
		require.NoError(t, err, "round %d", round)

		assert.ElementsMatch(t, want, fake.links(), "round %d", round)
		assert.Len(t, policyNames(fake), 3, "round %d: a second start writes no second policy", round)
		assert.Empty(t, outboxMarks(t, p), "round %d", round)
	}
}

func TestPolicyScopedState_HoldsEachKnownRoleOnceAndNothingForAnAbsentOwner(t *testing.T) {
	p := pool(t)
	seedOutboxOrg(t, p)
	execSQL(t, p, `UPDATE groups SET roles = ARRAY['member','wizard','admin','admin'] WHERE grn = $1`, outboxTeam)

	var roles []string
	require.NoError(t, p.QueryRow(context.Background(),
		`SELECT roles FROM policy_scoped_link_state($1)`, outboxTeam).Scan(&roles))
	assert.Equal(t, []string{"admin", "member"}, roles)

	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM policy_scoped_link_state($1)`, "grn:acme/iam/group/absent"))
}

// TestMigration000079_MarksTheGroupsAndProjectsOfAFeedThatIsOnAndDropsTheUsersRoles
// applies 000079 over groups and projects that hold roles. With the feed
// on each gains its mark; with it off none does, and `policy-feed start`
// marks them later. The roles column of users is dropped, and the way
// down restores it empty and removes the marks.
func TestMigration000079_MarksTheGroupsAndProjectsOfAFeedThatIsOnAndDropsTheUsersRoles(t *testing.T) {
	for _, on := range []bool{true, false} {
		name := "a feed that is off"
		schema := "migrate_000079_off"
		if on {
			name = "a feed that is on"
			schema = "migrate_000079_on"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := pool(t)
			m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = m.Close() })
			require.NoError(t, m.Migrate(78))
			if on {
				_, err = p.Exec(ctx, "UPDATE "+schema+".policy_feed SET enabled = true")
				require.NoError(t, err)
			}
			for _, q := range []string{
				`INSERT INTO ` + schema + `.orgs (grn, name, created_at, updated_at) VALUES ('` + outboxOrg + `', 'acme', now(), now())`,
				`INSERT INTO ` + schema + `.groups (grn, org_grn, name, roles, created_at, updated_at) VALUES ('` + outboxTeam + `', '` + outboxOrg + `', 'eng', ARRAY['admin'], now(), now())`,
				`INSERT INTO ` + schema + `.projects (grn, org_grn, name, created_at, updated_at) VALUES ('` + outboxProject + `', '` + outboxOrg + `', 'web', now(), now())`,
				`INSERT INTO ` + schema + `.users (grn, nickname, roles, created_at, updated_at) VALUES ('` + outboxUser + `', 'lee', ARRAY['admin'], now(), now())`,
			} {
				_, err = p.Exec(ctx, q)
				require.NoError(t, err, q)
			}
			_, err = p.Exec(ctx, "DELETE FROM "+schema+".policy_outbox")
			require.NoError(t, err)
			require.NoError(t, m.Migrate(79))

			want := 0
			if on {
				want = 2
			}
			const scoped = ".policy_outbox WHERE entity_type = 'Iam::ScopedRoleLink'"
			assert.Equal(t, want, countRows(t, p, "SELECT count(*) FROM "+schema+scoped))
			assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'users' AND column_name = 'roles'`, schema),
				"the roles column of users is dropped")

			require.NoError(t, m.Migrate(78), "000079 rolls back")
			assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM "+schema+scoped))
			assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'users' AND column_name = 'roles'`, schema),
				"the roles column of users is back")
			assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM "+schema+".users WHERE roles <> '{}'"),
				"and empty: what it held is not kept")
		})
	}
}
