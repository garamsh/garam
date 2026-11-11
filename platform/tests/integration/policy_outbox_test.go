package integration_test

import (
	"context"
	"io"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/cli"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
)

const (
	outboxOrg      = "grn:root/iam/org/acme"
	outboxUser     = "grn:acme/iam/user/lee"
	outboxAgent    = "grn:acme/iam/agent/bot"
	outboxOperator = "grn:acme/iam/operator/op1"
	outboxTeam     = "grn:acme/iam/group/eng"
	outboxTeam2    = "grn:acme/iam/group/ops"
	outboxProject  = "grn:acme/iam/project/web"
)

// seedOutboxOrg writes one organization holding a user, an agent, an
// operator, two teams and a project, with the user and the agent in
// the team and the project and the user a member of the organization.
func seedOutboxOrg(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('` + outboxOrg + `', 'acme', now(), now())`,
		`INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ('` + outboxUser + `', 'lee', now(), now())`,
		`INSERT INTO org_members (org_grn, user_grn, role, created_at) VALUES ('` + outboxOrg + `', '` + outboxUser + `', 'member', now())`,
		`INSERT INTO groups (grn, org_grn, name, created_at, updated_at) VALUES ('` + outboxTeam + `', '` + outboxOrg + `', 'eng', now(), now())`,
		`INSERT INTO groups (grn, org_grn, name, created_at, updated_at) VALUES ('` + outboxTeam2 + `', '` + outboxOrg + `', 'ops', now(), now())`,
		`INSERT INTO projects (grn, org_grn, name, created_at, updated_at) VALUES ('` + outboxProject + `', '` + outboxOrg + `', 'web', now(), now())`,
		`INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at) VALUES ('` + outboxAgent + `', '` + outboxOrg + `', 'ready', 'unknown', now(), now())`,
		`INSERT INTO operators (grn, org_grn, registered_by, created_at, name) VALUES ('` + outboxOperator + `', '` + outboxOrg + `', '` + outboxUser + `', now(), 'op1')`,
		`INSERT INTO group_members (group_grn, member_grn) VALUES ('` + outboxTeam + `', '` + outboxUser + `')`,
		`INSERT INTO group_members (group_grn, member_grn) VALUES ('` + outboxTeam + `', '` + outboxAgent + `')`,
		`INSERT INTO project_members (project_grn, member_grn) VALUES ('` + outboxProject + `', '` + outboxUser + `')`,
		`INSERT INTO project_members (project_grn, member_grn) VALUES ('` + outboxProject + `', '` + outboxAgent + `')`,
	} {
		execSQL(t, p, q)
	}
}

// outboxEntities is every entity the seeded organization holds.
var outboxEntities = []string{
	"Agent " + outboxAgent, "Operator " + outboxOperator, "Org " + outboxOrg,
	"OrgMembers " + outboxOrg, "Project " + outboxProject, "Team " + outboxTeam,
	"Team " + outboxTeam2, "User " + outboxUser,
}

func execSQL(t *testing.T, p *pgxpool.Pool, q string, args ...any) {
	t.Helper()
	_, err := p.Exec(context.Background(), q, args...)
	require.NoError(t, err, q)
}

// outboxMarks reads the outbox as "Type id" keys with their
// generations.
func outboxMarks(t *testing.T, p *pgxpool.Pool) map[string]int64 {
	t.Helper()
	rows, err := p.Query(context.Background(), `SELECT entity_type || ' ' || entity_id, generation FROM policy_outbox`)
	require.NoError(t, err)
	defer rows.Close()
	marks := map[string]int64{}
	for rows.Next() {
		var key string
		var gen int64
		require.NoError(t, rows.Scan(&key, &gen))
		marks[key] = gen
	}
	require.NoError(t, rows.Err())
	return marks
}

func markKeys(marks map[string]int64) []string {
	keys := make([]string, 0, len(marks))
	for k := range marks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// startFeed turns the feed on as `garam policy-feed start` does and
// turns it off with the test.
func startFeed(t *testing.T, p *pgxpool.Pool) int64 {
	t.Helper()
	sw := policyfeedpg.New(p)
	marked, err := sw.Start(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = sw.Stop(context.Background()) })
	return marked
}

func feedEnabled(t *testing.T, p *pgxpool.Pool) bool {
	t.Helper()
	var enabled bool
	require.NoError(t, p.QueryRow(context.Background(), `SELECT enabled FROM policy_feed`).Scan(&enabled))
	return enabled
}

// seedWithFeedOn seeds the organization with the feed on and empties
// the outbox, so a case reads only what its own statement marked.
func seedWithFeedOn(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	startFeed(t, p)
	seedOutboxOrg(t, p)
	execSQL(t, p, `DELETE FROM policy_outbox`)
	require.True(t, feedEnabled(t, p), "the case needs the feed on")
	require.Empty(t, outboxMarks(t, p), "the case needs an empty outbox")
}

func TestPolicyOutbox_EachChangeMarksTheEntitiesItReaches(t *testing.T) {
	const org2 = "grn:root/iam/org/globex"
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{"an organization is added", `INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('` + org2 + `', 'globex', now(), now())`,
			[]string{"Org " + org2, "OrgMembers " + org2}},
		{"a team is added", `INSERT INTO groups (grn, org_grn, name, created_at, updated_at) VALUES ('grn:acme/iam/group/new', '` + outboxOrg + `', 'new', now(), now())`,
			[]string{"Team grn:acme/iam/group/new"}},
		{"a project is added", `INSERT INTO projects (grn, org_grn, name, created_at, updated_at) VALUES ('grn:acme/iam/project/new', '` + outboxOrg + `', 'new', now(), now())`,
			[]string{"Project grn:acme/iam/project/new"}},
		{"a user is added", `INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ('grn:acme/iam/user/new', 'new', now(), now())`,
			[]string{"User grn:acme/iam/user/new"}},
		{"an agent is added", `INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at) VALUES ('grn:acme/iam/agent/new', '` + outboxOrg + `', 'ready', 'unknown', now(), now())`,
			[]string{"Agent grn:acme/iam/agent/new"}},
		{"an operator is added", `INSERT INTO operators (grn, org_grn, registered_by, created_at, name) VALUES ('grn:acme/iam/operator/new', '` + outboxOrg + `', 'x', now(), 'new')`,
			[]string{"Operator grn:acme/iam/operator/new"}},
		{"a user joins a team", `INSERT INTO group_members (group_grn, member_grn) VALUES ('` + outboxTeam2 + `', '` + outboxUser + `')`,
			[]string{"User " + outboxUser}},
		{"an agent joins a team", `INSERT INTO group_members (group_grn, member_grn) VALUES ('` + outboxTeam2 + `', '` + outboxAgent + `')`,
			[]string{"Agent " + outboxAgent}},
		{"a member of another type joins a team", `INSERT INTO group_members (group_grn, member_grn) VALUES ('` + outboxTeam2 + `', '` + outboxOperator + `')`,
			nil},
		{"an agent leaves a project", `DELETE FROM project_members WHERE member_grn = '` + outboxAgent + `'`,
			[]string{"Agent " + outboxAgent}},
		{"a user leaves an organization", `DELETE FROM org_members WHERE user_grn = '` + outboxUser + `'`,
			[]string{"User " + outboxUser}},
		{"a team is removed", `DELETE FROM groups WHERE grn = '` + outboxTeam + `'`,
			[]string{"Team " + outboxTeam, "User " + outboxUser, "Agent " + outboxAgent}},
		{"a project is removed", `DELETE FROM projects WHERE grn = '` + outboxProject + `'`,
			[]string{"Project " + outboxProject, "User " + outboxUser, "Agent " + outboxAgent}},
		{"a user is removed", `DELETE FROM users WHERE grn = '` + outboxUser + `'`,
			[]string{"User " + outboxUser}},
		{"an agent is removed", `DELETE FROM agents WHERE grn = '` + outboxAgent + `'`,
			[]string{"Agent " + outboxAgent}},
		{"an operator is removed", `DELETE FROM operators WHERE grn = '` + outboxOperator + `'`,
			[]string{"Operator " + outboxOperator}},
		{"an organization is removed and its foreign keys take the rest", `DELETE FROM orgs WHERE grn = '` + outboxOrg + `'`,
			outboxEntities},
		{"a member's role changes", `UPDATE org_members SET role = 'admin'`, nil},
		{"a user is renamed", `UPDATE users SET nickname = 'renamed', roles = ARRAY['admin'], updated_at = now()`, nil},
		{"an organization is renamed", `UPDATE orgs SET name = 'renamed', updated_at = now()`, nil},
		{"a team is renamed and given a role", `UPDATE groups SET name = 'renamed', roles = ARRAY['admin'], updated_at = now()`, nil},
		{"a project is renamed", `UPDATE projects SET name = 'renamed', updated_at = now()`, nil},
		{"an agent reports a state", `UPDATE agents SET runtime_state = 'connected', name = 'renamed', updated_at = now()`, nil},
		{"an operator is renamed", `UPDATE operators SET name = 'renamed', description = 'd'`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := pool(t)
			seedWithFeedOn(t, p)

			execSQL(t, p, tt.sql)

			got := markKeys(outboxMarks(t, p))
			want := append([]string{}, tt.want...)
			sort.Strings(want)
			assert.Equal(t, want, got)
		})
	}
}

func TestPolicyOutbox_AUserJoiningAnOrganizationMarksTheUser(t *testing.T) {
	p := pool(t)
	seedWithFeedOn(t, p)
	execSQL(t, p, `DELETE FROM org_members`)
	execSQL(t, p, `DELETE FROM policy_outbox`)

	execSQL(t, p, `INSERT INTO org_members (org_grn, user_grn, role, created_at)
		VALUES ($1, $2, 'member', now())`, outboxOrg, outboxUser)

	assert.Equal(t, []string{"User " + outboxUser}, markKeys(outboxMarks(t, p)))
}

func TestPolicyOutbox_AFeedThatIsOffWritesNothing(t *testing.T) {
	p := pool(t)
	require.False(t, feedEnabled(t, p), "the case needs the feed off")

	seedOutboxOrg(t, p)
	execSQL(t, p, `DELETE FROM orgs`)
	execSQL(t, p, `DELETE FROM users`)

	assert.Empty(t, outboxMarks(t, p))

	// The control: the same writes with the feed on mark entities, so
	// the empty outbox above is the gate and not the statements.
	startFeed(t, p)
	seedOutboxOrg(t, p)
	assert.Equal(t, outboxEntities, markKeys(outboxMarks(t, p)))
}

func TestPolicyOutbox_AWriteRaisesTheGeneration(t *testing.T) {
	p := pool(t)
	seedWithFeedOn(t, p)

	execSQL(t, p, `DELETE FROM group_members WHERE member_grn = '`+outboxUser+`'`)
	first := outboxMarks(t, p)
	require.Equal(t, int64(1), first["User "+outboxUser], "the first mark starts at one")

	execSQL(t, p, `INSERT INTO group_members (group_grn, member_grn) VALUES ('`+outboxTeam2+`', '`+outboxUser+`')`)
	execSQL(t, p, `DELETE FROM project_members WHERE member_grn = '`+outboxUser+`'`)
	execSQL(t, p, `DELETE FROM project_members WHERE member_grn = '`+outboxAgent+`'`)

	second := outboxMarks(t, p)
	assert.Equal(t, int64(3), second["User "+outboxUser], "each write raised the mark's generation")
	assert.Equal(t, int64(1), second["Agent "+outboxAgent], "a mark written once stays at one")
}

func TestPolicyFeedStart_MarksEveryLiveEntityAsTheTriggersWould(t *testing.T) {
	p := pool(t)
	require.False(t, feedEnabled(t, p))
	seedOutboxOrg(t, p)
	require.Empty(t, outboxMarks(t, p), "the seed ran with the feed off")

	marked := startFeed(t, p)

	assert.True(t, feedEnabled(t, p))
	marks := outboxMarks(t, p)
	assert.Equal(t, outboxEntities, markKeys(marks))
	assert.Equal(t, int64(len(outboxEntities)), marked)

	// The same state built with the feed on is the set the triggers
	// mark, so the backfill and the triggers cannot drift apart.
	execSQL(t, p, `DELETE FROM orgs`)
	execSQL(t, p, `DELETE FROM users`)
	execSQL(t, p, `DELETE FROM policy_outbox`)
	seedOutboxOrg(t, p)
	assert.Equal(t, markKeys(marks), markKeys(outboxMarks(t, p)))
}

func TestPolicyFeedStart_WaitsOutAWriterThatReadTheFeedAsOff(t *testing.T) {
	p := pool(t)
	ctx := context.Background()
	// Registered first so it runs last: Stop waits on the writer's
	// lock, which the rollback below releases.
	t.Cleanup(func() { _, _ = policyfeedpg.New(p).Stop(ctx) })
	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	_, err = tx.Exec(ctx, `INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('`+outboxOrg+`', 'acme', now(), now())`)
	require.NoError(t, err)

	started := make(chan error, 1)
	go func() {
		_, err := policyfeedpg.New(p).Start(ctx)
		started <- err
	}()

	// Start is waiting on the writer's lock, which is what the
	// pause below depends on.
	require.Eventually(t, func() bool {
		var n int
		err := p.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE 'LOCK TABLE%'`).Scan(&n)
		return err == nil && n == 1
	}, 10*time.Second, 20*time.Millisecond)
	select {
	case err := <-started:
		require.Failf(t, "start returned while a writer held the table", "%v", err)
	default:
	}

	require.NoError(t, tx.Commit(ctx))
	require.NoError(t, <-started)

	assert.Equal(t, []string{"Org " + outboxOrg, "OrgMembers " + outboxOrg}, markKeys(outboxMarks(t, p)),
		"the organization the writer committed is marked")
}

func TestPolicyFeedStop_TurnsTheFeedOffAndEmptiesTheOutbox(t *testing.T) {
	p := pool(t)
	startFeed(t, p)
	seedOutboxOrg(t, p)
	require.NotEmpty(t, outboxMarks(t, p), "the case needs marks to remove")

	removed, err := policyfeedpg.New(p).Stop(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(len(outboxEntities)), removed)
	assert.False(t, feedEnabled(t, p))
	assert.Empty(t, outboxMarks(t, p))
	execSQL(t, p, `INSERT INTO users (grn, nickname, created_at, updated_at) VALUES ('grn:acme/iam/user/late', 'late', now(), now())`)
	assert.Empty(t, outboxMarks(t, p), "a write after stop marks nothing")
}

// TestPolicyFeedCommands_SwitchTheFeedThroughTheRootCommand runs the
// two commands as a deployment does.
func TestPolicyFeedCommands_SwitchTheFeedThroughTheRootCommand(t *testing.T) {
	p := pool(t)
	t.Setenv("GARAM_DATABASE_URL", os.Getenv("GARAM_TEST_DATABASE_URL"))
	run := func(verb string) {
		root := cli.NewRootCommand()
		root.SetArgs([]string{"policy-feed", verb})
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		require.NoError(t, root.Execute())
	}
	seedOutboxOrg(t, p)

	run("start")
	assert.True(t, feedEnabled(t, p))
	assert.Equal(t, outboxEntities, markKeys(outboxMarks(t, p)))

	run("stop")
	assert.False(t, feedEnabled(t, p))
	assert.Empty(t, outboxMarks(t, p))
}

type entityState struct {
	org     *string
	members *string
	parents []string
}

func readEntityState(t *testing.T, p *pgxpool.Pool, typ, id string) *entityState {
	t.Helper()
	rows, err := p.Query(context.Background(),
		`SELECT org, members, parent_types, parent_ids FROM policy_entity_state($1, $2)`, typ, id)
	require.NoError(t, err)
	defer rows.Close()
	if !rows.Next() {
		require.NoError(t, rows.Err())
		return nil
	}
	var s entityState
	var types, ids []string
	require.NoError(t, rows.Scan(&s.org, &s.members, &types, &ids))
	require.Len(t, ids, len(types))
	for i := range types {
		s.parents = append(s.parents, types[i]+" "+ids[i])
	}
	require.False(t, rows.Next(), "an entity has one state")
	return &s
}

func TestPolicyEntityState_ReturnsWhatTheOwnersTablesSay(t *testing.T) {
	p := pool(t)
	seedOutboxOrg(t, p)

	tests := []struct {
		name string
		typ  string
		id   string
		want entityState
	}{
		{"an org names itself and its members", "Org", outboxOrg, entityState{org: ptr(outboxOrg), members: ptr(outboxOrg)}},
		{"an OrgMembers names nothing", "OrgMembers", outboxOrg, entityState{}},
		{"a team sits in its org", "Team", outboxTeam, entityState{org: ptr(outboxOrg), parents: []string{"Org " + outboxOrg}}},
		{"a project sits in its org", "Project", outboxProject, entityState{org: ptr(outboxOrg), parents: []string{"Org " + outboxOrg}}},
		{"an operator sits in its org", "Operator", outboxOperator, entityState{org: ptr(outboxOrg), parents: []string{"Org " + outboxOrg}}},
		{"a user sits in each membership it holds", "User", outboxUser, entityState{parents: []string{
			"OrgMembers " + outboxOrg, "Project " + outboxProject, "Team " + outboxTeam}}},
		{"an agent is a member and a contained resource", "Agent", outboxAgent, entityState{org: ptr(outboxOrg), parents: []string{
			"Org " + outboxOrg, "OrgMembers " + outboxOrg, "Project " + outboxProject, "Team " + outboxTeam}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readEntityState(t, p, tt.typ, tt.id)
			require.NotNil(t, got)
			assert.Equal(t, tt.want, *got)
		})
	}

	t.Run("an entity the tables no longer hold has no state", func(t *testing.T) {
		require.NotNil(t, readEntityState(t, p, "User", outboxUser), "the control: the user is held")
		execSQL(t, p, `DELETE FROM users WHERE grn = $1`, outboxUser)
		// A deleted user leaves its group_members and project_members
		// rows behind, which must not read as a user.
		require.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM group_members WHERE member_grn = $1`, outboxUser))
		assert.Nil(t, readEntityState(t, p, "User", outboxUser))
		assert.Nil(t, readEntityState(t, p, "Team", "grn:acme/iam/group/absent"))
	})

	t.Run("a change to a membership reaches the next read", func(t *testing.T) {
		execSQL(t, p, `DELETE FROM project_members WHERE member_grn = $1`, outboxAgent)
		got := readEntityState(t, p, "Agent", outboxAgent)
		require.NotNil(t, got)
		assert.Equal(t, []string{"Org " + outboxOrg, "OrgMembers " + outboxOrg, "Team " + outboxTeam}, got.parents)
	})
}
