package integration_test

import (
	"context"
	"crypto/md5"
	"fmt"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
)

// supervisorSeed names what one registration of a supervisor is made
// of, in the organization acme or globex.
type supervisorSeed struct {
	org        string // the organization's slug
	agent      string // the agent's slug
	supervisor string // the supervisor's slug
}

func (s supervisorSeed) orgGRN() string   { return "grn:root/iam/org/" + s.org }
func (s supervisorSeed) agentGRN() string { return "grn:" + s.org + "/iam/agent/" + s.agent }
func (s supervisorSeed) supervisorGRN() string {
	return "grn:" + s.org + "/iam/supervisor/" + s.supervisor
}

// agentSQL writes the organization (once) and the agent with the row
// its executions hang from, in the schema prefix names ("" for the
// shared one).
func (s supervisorSeed) agentSQL(prefix string) []string {
	return []string{
		`INSERT INTO ` + prefix + `orgs (grn, name, created_at, updated_at)
			VALUES ('` + s.orgGRN() + `', '` + s.org + `', now(), now()) ON CONFLICT DO NOTHING`,
		`INSERT INTO ` + prefix + `agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
			VALUES ('` + s.agentGRN() + `', '` + s.orgGRN() + `', 'ready', 'unknown', now(), now())`,
		`INSERT INTO ` + prefix + `agent_executions (agent_grn, updated_at) VALUES ('` + s.agentGRN() + `', now())`,
	}
}

// registrationSQL writes the approval and the registration of the
// supervisor for the agent, as enrolling a standalone host does, with
// no assignment.
func (s supervisorSeed) registrationSQL(prefix string) []string {
	sum := md5.Sum([]byte(s.supervisorGRN()))
	id := fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
	return []string{
		`INSERT INTO ` + prefix + `agent_enrollments (id, org_grn, agent_grn, supervisor_grn, host_name, request_id,
				approved_by, secret_sha256, expires_at, created_at)
			VALUES ('` + id + `', '` + s.orgGRN() + `', '` + s.agentGRN() + `', '` + s.supervisorGRN() + `', 'laptop',
				'req-` + s.supervisor + `', 'grn:` + s.org + `/iam/user/lee', sha256('` + s.supervisor + `'::bytea),
				now() + interval '1 hour', now())`,
		`INSERT INTO ` + prefix + `agent_supervisors (grn, agent_grn, enrollment_id, host_name, registered_at)
			VALUES ('` + s.supervisorGRN() + `', '` + s.agentGRN() + `', '` + id + `', 'laptop', now())`,
	}
}

// assignSQL gives the agent to the assignee.
func (s supervisorSeed) assignSQL(prefix, assignee string) string {
	return `INSERT INTO ` + prefix + `agent_assignments (agent_grn, assignee_grn, epoch, created_at, updated_at)
		VALUES ('` + s.agentGRN() + `', '` + assignee + `', 1, now(), now())`
}

func (s supervisorSeed) exec(t *testing.T, p *pgxpool.Pool, queries ...string) {
	t.Helper()
	for _, q := range queries {
		execSQL(t, p, q)
	}
}

// supervisorMark and agentMark are the outbox keys of the entities.
func supervisorMark(grn string) string { return "Iam::Supervisor " + grn }
func agentMark(grn string) string      { return "Iam::Agent " + grn }

// readAgentOf is the agent policy_entity_state says a supervisor
// holds, and whether the state has a row at all.
func readSupervisorState(t *testing.T, p *pgxpool.Pool, id string) (state *entityState, agent *string) {
	t.Helper()
	var org, members *string
	var types, ids []string
	err := p.QueryRow(context.Background(),
		`SELECT org, members, parent_types, parent_ids, agent FROM policy_entity_state('Iam::Supervisor', $1)`, id).
		Scan(&org, &members, &types, &ids, &agent)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	require.NoError(t, err)
	state = &entityState{org: org, members: members}
	for i := range types {
		state.parents = append(state.parents, types[i]+" "+ids[i])
	}
	return state, agent
}

// TestPolicyOutbox_ARegistrationAndItsAssignmentMarkTheirSupervisor
// holds each write that starts, moves or ends a registration, or moves
// the agent it holds, against the whole set of marks it leaves. Before
// every write the outbox holds a mark of an unrelated entity, which a
// write must leave alone, and a registration of another organization
// is written beside, whose supervisor no write here may mark.
func TestPolicyOutbox_ARegistrationAndItsAssignmentMarkTheirSupervisor(t *testing.T) {
	p := pool(t)
	startFeed(t, p)
	a := supervisorSeed{org: "acme", agent: "a1", supervisor: "s1"}
	b := supervisorSeed{org: "acme", agent: "a2", supervisor: "s2"}
	other := supervisorSeed{org: "globex", agent: "g1", supervisor: "gs1"}
	operatorGRN := "grn:acme/iam/operator/op1"
	unrelated := "Iam::User grn:acme/iam/user/lee"
	a.exec(t, p, a.agentSQL("")...)
	b.exec(t, p, b.agentSQL("")...)
	other.exec(t, p, other.agentSQL("")...)
	other.exec(t, p, other.registrationSQL("")...)
	other.exec(t, p, other.assignSQL("", other.supervisorGRN()))

	steps := []struct {
		name string
		do   func()
		want []string
	}{
		{"a registration starts", func() { a.exec(t, p, a.registrationSQL("")...) },
			[]string{supervisorMark(a.supervisorGRN())}},
		{"the agent is assigned to it", func() { a.exec(t, p, a.assignSQL("", a.supervisorGRN())) },
			[]string{supervisorMark(a.supervisorGRN())}},
		{"a write of another column of the registration",
			func() {
				execSQL(t, p, `UPDATE agent_supervisors SET host_name = 'renamed' WHERE grn = $1`, a.supervisorGRN())
			},
			nil},
		{"the agent is moved to an operator, which loses it its agent",
			func() {
				execSQL(t, p, `UPDATE agent_assignments SET assignee_grn = $1, epoch = 2 WHERE agent_grn = $2`, operatorGRN, a.agentGRN())
			},
			[]string{supervisorMark(a.supervisorGRN())}},
		{"the agent is moved onto another supervisor",
			func() {
				b.exec(t, p, b.registrationSQL("")...)
				execSQL(t, p, `DELETE FROM policy_outbox WHERE entity_type = 'Iam::Supervisor'`)
				execSQL(t, p, `UPDATE agent_assignments SET assignee_grn = $1, epoch = 3 WHERE agent_grn = $2`, b.supervisorGRN(), a.agentGRN())
			},
			[]string{supervisorMark(b.supervisorGRN())}},
		{"the agent is moved between two supervisors",
			func() {
				execSQL(t, p, `UPDATE agent_assignments SET assignee_grn = $1, epoch = 4 WHERE agent_grn = $2`, a.supervisorGRN(), a.agentGRN())
			},
			[]string{supervisorMark(a.supervisorGRN()), supervisorMark(b.supervisorGRN())}},
		{"the registration ends",
			func() {
				execSQL(t, p, `UPDATE agent_supervisors SET ended_at = now() WHERE grn = $1`, a.supervisorGRN())
			},
			[]string{supervisorMark(a.supervisorGRN())}},
		{"the agent is deleted, which takes the registration and the assignment",
			func() { execSQL(t, p, `DELETE FROM agents WHERE grn = $1`, a.agentGRN()) },
			[]string{agentMark(a.agentGRN()), supervisorMark(a.supervisorGRN())}},
		{"an agent that holds no assignment is deleted with its registration",
			func() { execSQL(t, p, `DELETE FROM agents WHERE grn = $1`, b.agentGRN()) },
			[]string{agentMark(b.agentGRN()), supervisorMark(b.supervisorGRN())}},
	}
	for _, step := range steps {
		execSQL(t, p, `DELETE FROM policy_outbox`)
		execSQL(t, p, `SELECT policy_outbox_mark('Iam::User', 'grn:acme/iam/user/lee')`)

		step.do()

		want := append([]string{unrelated}, step.want...)
		assert.ElementsMatch(t, want, markKeys(outboxMarks(t, p)), step.name)
	}
}

// TestPolicyOutbox_AFeedThatIsOffMarksNoSupervisor is the control for
// the marks above: the same writes with the feed off leave the outbox
// empty, so the marks are the triggers' and not another's.
func TestPolicyOutbox_AFeedThatIsOffMarksNoSupervisor(t *testing.T) {
	p := pool(t)
	require.False(t, feedEnabled(t, p))
	a := supervisorSeed{org: "acme", agent: "a1", supervisor: "s1"}
	a.exec(t, p, a.agentSQL("")...)
	a.exec(t, p, a.registrationSQL("")...)
	a.exec(t, p, a.assignSQL("", a.supervisorGRN()))
	execSQL(t, p, `UPDATE agent_supervisors SET ended_at = now()`)

	assert.Empty(t, outboxMarks(t, p))
}

// TestPolicyEntityState_ASupervisorHoldsTheAgentItsAssignmentNames
// holds the state of a supervisor against the cases where a wrong
// reading differs from the right one: an agent whose assignment names
// another supervisor or an operator, a registration that ended, and a
// second organization whose supervisor must sit under its own
// organization's parents.
func TestPolicyEntityState_ASupervisorHoldsTheAgentItsAssignmentNames(t *testing.T) {
	p := pool(t)
	a := supervisorSeed{org: "acme", agent: "a1", supervisor: "s1"}
	b := supervisorSeed{org: "acme", agent: "a2", supervisor: "s2"}
	g := supervisorSeed{org: "globex", agent: "g1", supervisor: "gs1"}
	for _, s := range []supervisorSeed{a, b, g} {
		s.exec(t, p, s.agentSQL("")...)
		s.exec(t, p, s.registrationSQL("")...)
	}
	parentsOf := func(s supervisorSeed) []string {
		return []string{"Iam::Org " + s.orgGRN(), "Iam::OrgMembers " + s.orgGRN()}
	}

	state, agent := readSupervisorState(t, p, a.supervisorGRN())
	require.NotNil(t, state)
	assert.Equal(t, entityState{org: ptr(a.orgGRN()), parents: parentsOf(a)}, *state,
		"a supervisor sits in its org and among its members, as an agent does")
	assert.Nil(t, agent, "a registration alone holds no agent: the assignment says what holds it")

	a.exec(t, p, a.assignSQL("", b.supervisorGRN()))
	_, agent = readSupervisorState(t, p, a.supervisorGRN())
	assert.Nil(t, agent, "an agent assigned to another supervisor is not held by this one")
	_, agent = readSupervisorState(t, p, b.supervisorGRN())
	assert.Nil(t, agent, "and the other holds the agent its own registration names, which is another")

	execSQL(t, p, `UPDATE agent_assignments SET assignee_grn = $1 WHERE agent_grn = $2`, a.supervisorGRN(), a.agentGRN())
	_, agent = readSupervisorState(t, p, a.supervisorGRN())
	require.NotNil(t, agent)
	assert.Equal(t, a.agentGRN(), *agent, "assigned to it, the agent it registered")

	g.exec(t, p, g.assignSQL("", g.supervisorGRN()))
	state, agent = readSupervisorState(t, p, g.supervisorGRN())
	require.NotNil(t, state)
	assert.Equal(t, entityState{org: ptr(g.orgGRN()), parents: parentsOf(g)}, *state,
		"a second organization's supervisor sits under its own parents")
	require.NotNil(t, agent)
	assert.Equal(t, g.agentGRN(), *agent)
	state, _ = readSupervisorState(t, p, a.supervisorGRN())
	assert.Equal(t, entityState{org: ptr(a.orgGRN()), parents: parentsOf(a)}, *state, "and the first is unmoved")

	execSQL(t, p, `UPDATE agent_assignments SET assignee_grn = 'grn:acme/iam/operator/op1' WHERE agent_grn = $1`, a.agentGRN())
	_, agent = readSupervisorState(t, p, a.supervisorGRN())
	assert.Nil(t, agent, "an agent held by an operator is not held by its former supervisor")

	execSQL(t, p, `UPDATE agent_assignments SET assignee_grn = $1 WHERE agent_grn = $2`, a.supervisorGRN(), a.agentGRN())
	execSQL(t, p, `UPDATE agent_supervisors SET ended_at = now() WHERE grn = $1`, a.supervisorGRN())
	state, agent = readSupervisorState(t, p, a.supervisorGRN())
	assert.Nil(t, state, "an ended registration has no state")
	assert.Nil(t, agent)

	state, _ = readSupervisorState(t, p, "grn:acme/iam/supervisor/absent")
	assert.Nil(t, state, "nor does one the table never held")

	var held *string
	require.NoError(t, p.QueryRow(context.Background(),
		`SELECT agent FROM policy_entity_state('Iam::Agent', $1)`, g.agentGRN()).Scan(&held))
	assert.Nil(t, held, "an agent holds none: the column is a supervisor's alone")
}

// TestPolicyDelivery_ASupervisorIsDeliveredWithItsAgentAndItsEndAsADelete
// reads what the service is sent, whole: the entity's org, its
// parents and its agent, for a supervisor that holds an agent, one that
// holds none and one of another organization, in one pass beside a
// mark of an unrelated entity; then the end of a registration.
func TestPolicyDelivery_ASupervisorIsDeliveredWithItsAgentAndItsEndAsADelete(t *testing.T) {
	p := pool(t)
	fake, key := newFake(t)
	d := newDeliverer(t, p, fake, key)
	startFeed(t, p)
	a := supervisorSeed{org: "acme", agent: "a1", supervisor: "s1"}
	idle := supervisorSeed{org: "acme", agent: "a2", supervisor: "s2"}
	g := supervisorSeed{org: "globex", agent: "g1", supervisor: "gs1"}
	for _, s := range []supervisorSeed{a, idle, g} {
		s.exec(t, p, s.agentSQL("")...)
		s.exec(t, p, s.registrationSQL("")...)
	}
	a.exec(t, p, a.assignSQL("", a.supervisorGRN()))
	g.exec(t, p, g.assignSQL("", g.supervisorGRN()))
	idle.exec(t, p, idle.assignSQL("", "grn:acme/iam/operator/op1"))
	_, err := d.pass(t)
	require.NoError(t, err)
	require.Empty(t, outboxMarks(t, p))

	type delivered struct {
		org, members, agent string
		parents             []string
	}
	read := func(sv supervisorSeed) (*delivered, bool) {
		var out *delivered
		var deleted bool
		for _, req := range fake.received() {
			c, ok := changesByEntity(req)[supervisorMark(sv.supervisorGRN())]
			if !ok {
				continue
			}
			deleted = c.GetDelete() != nil
			out = &delivered{
				org:     c.GetUpsert().GetOrg().GetType() + " " + c.GetUpsert().GetOrg().GetId(),
				members: c.GetUpsert().GetMembers().GetId(), parents: parentKeys(c),
			}
			if ag := c.GetUpsert().GetAgent(); ag != nil {
				out.agent = ag.GetType() + " " + ag.GetId()
			}
		}
		return out, deleted
	}

	got, _ := read(a)
	require.NotNil(t, got, "the service was sent the supervisor")
	assert.Equal(t, delivered{org: "Iam::Org " + a.orgGRN(),
		parents: []string{"Iam::Org " + a.orgGRN(), "Iam::OrgMembers " + a.orgGRN()},
		agent:   "Iam::Agent " + a.agentGRN()}, *got)
	got, _ = read(idle)
	require.NotNil(t, got)
	assert.Equal(t, delivered{org: "Iam::Org " + idle.orgGRN(),
		parents: []string{"Iam::Org " + idle.orgGRN(), "Iam::OrgMembers " + idle.orgGRN()}}, *got,
		"a supervisor whose agent an operator holds is sent with no agent")
	got, _ = read(g)
	require.NotNil(t, got)
	assert.Equal(t, delivered{org: "Iam::Org " + g.orgGRN(),
		parents: []string{"Iam::Org " + g.orgGRN(), "Iam::OrgMembers " + g.orgGRN()},
		agent:   "Iam::Agent " + g.agentGRN()}, *got, "another organization's, under its own")

	execSQL(t, p, `SELECT policy_outbox_mark('Iam::User', 'grn:acme/iam/user/lee')`)
	execSQL(t, p, `UPDATE agent_supervisors SET ended_at = now() WHERE grn = $1`, a.supervisorGRN())
	before := len(fake.received())
	_, err = d.pass(t)
	require.NoError(t, err)
	require.Greater(t, len(fake.received()), before)
	_, deleted := read(a)
	assert.True(t, deleted, "an ended registration is deleted from the service")
	got, deleted = read(g)
	assert.False(t, deleted, "and the other organization's supervisor is not")
	assert.Equal(t, "Iam::Agent "+g.agentGRN(), got.agent)
	assert.Empty(t, outboxMarks(t, p))
}

// TestPolicyFeedStart_MarksTheSupervisorsThatStand holds that start
// marks each registration that stands and no ended one, and that those
// marks are the ones the triggers raise for the same state built with
// the feed on, so the backfill and the triggers do not drift apart.
func TestPolicyFeedStart_MarksTheSupervisorsThatStand(t *testing.T) {
	p := pool(t)
	build := func() {
		a := supervisorSeed{org: "acme", agent: "a1", supervisor: "s1"}
		ended := supervisorSeed{org: "acme", agent: "a2", supervisor: "s2"}
		g := supervisorSeed{org: "globex", agent: "g1", supervisor: "gs1"}
		for _, s := range []supervisorSeed{a, ended, g} {
			s.exec(t, p, s.agentSQL("")...)
			s.exec(t, p, s.registrationSQL("")...)
		}
		a.exec(t, p, a.assignSQL("", a.supervisorGRN()))
		execSQL(t, p, `UPDATE agent_supervisors SET ended_at = now() WHERE grn = $1`, ended.supervisorGRN())
	}
	build()
	require.Empty(t, outboxMarks(t, p), "the seed ran with the feed off")

	startFeed(t, p)

	var got []string
	for _, k := range markKeys(outboxMarks(t, p)) {
		if len(k) > len("Iam::Supervisor ") && k[:len("Iam::Supervisor ")] == "Iam::Supervisor " {
			got = append(got, k)
		}
	}
	want := []string{supervisorMark("grn:acme/iam/supervisor/s1"), supervisorMark("grn:globex/iam/supervisor/gs1")}
	assert.Equal(t, want, got, "a registration that ended is not marked")

	execSQL(t, p, `DELETE FROM orgs`)
	execSQL(t, p, `DELETE FROM policy_outbox`)
	build()
	got = nil
	for k := range outboxMarks(t, p) {
		if len(k) > len("Iam::Supervisor ") && k[:len("Iam::Supervisor ")] == "Iam::Supervisor " {
			got = append(got, k)
		}
	}
	assert.Subset(t, got, want, "the triggers mark each the backfill does")
}

// TestPolicyFeedStart_WaitsOutAWriterOfARegistrationThatReadTheFeedAsOff
// holds that start takes agent_supervisors and agent_assignments with
// the other source tables: a registration or an assignment in flight
// when the feed is switched on is committed before the backfill reads.
func TestPolicyFeedStart_WaitsOutAWriterOfARegistrationThatReadTheFeedAsOff(t *testing.T) {
	a := supervisorSeed{org: "acme", agent: "a1", supervisor: "s1"}
	writers := map[string]func(t *testing.T, tx pgx.Tx){
		"a registration": func(t *testing.T, tx pgx.Tx) {
			for _, q := range a.registrationSQL("") {
				_, err := tx.Exec(context.Background(), q)
				require.NoError(t, err)
			}
		},
		"an assignment": func(t *testing.T, tx pgx.Tx) {
			_, err := tx.Exec(context.Background(), a.assignSQL("", a.supervisorGRN()))
			require.NoError(t, err)
		},
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			p := pool(t)
			ctx := context.Background()
			a.exec(t, p, a.agentSQL("")...)
			if name == "an assignment" {
				a.exec(t, p, a.registrationSQL("")...)
			}
			t.Cleanup(func() { _, _ = policyfeedpg.New(p).Stop(ctx) })
			tx, err := p.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback(ctx) })
			write(t, tx)

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
			}, 10*time.Second, 20*time.Millisecond, "start waits on the writer")
			select {
			case err := <-started:
				require.Failf(t, "start returned while a writer held the table", "%v", err)
			default:
			}

			require.NoError(t, tx.Commit(ctx))
			require.NoError(t, <-started)
			assert.Contains(t, markKeys(outboxMarks(t, p)), supervisorMark(a.supervisorGRN()))
		})
	}
}

// TestMigration000080_MarksEverySupervisorThatStandsOfAFeedThatIsOn
// applies 000080 over registrations that exist: a feed that is on marks
// each that stands and not the one that ended, one that is off marks
// none, and the way down removes the triggers, the marks of the kind
// and the kind itself.
func TestMigration000080_MarksEverySupervisorThatStandsOfAFeedThatIsOn(t *testing.T) {
	for _, on := range []bool{true, false} {
		name, schema := "a feed that is off", "migrate_000080_off"
		if on {
			name, schema = "a feed that is on", "migrate_000080_on"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			p := pool(t)
			m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = m.Close() })
			require.NoError(t, m.Migrate(79))
			if on {
				execSQL(t, p, "UPDATE "+schema+".policy_feed SET enabled = true")
			}
			standing := supervisorSeed{org: "acme", agent: "a1", supervisor: "s1"}
			ended := supervisorSeed{org: "acme", agent: "a2", supervisor: "s2"}
			for _, s := range []supervisorSeed{standing, ended} {
				s.exec(t, p, s.agentSQL(schema+".")...)
				s.exec(t, p, s.registrationSQL(schema+".")...)
			}
			execSQL(t, p, "UPDATE "+schema+".agent_supervisors SET ended_at = now() WHERE grn = '"+ended.supervisorGRN()+"'")
			execSQL(t, p, "DELETE FROM "+schema+".policy_outbox")
			require.NoError(t, m.Migrate(80))

			marked := func(sv supervisorSeed) int {
				return countRows(t, p, "SELECT count(*) FROM "+schema+".policy_outbox WHERE entity_type = 'Iam::Supervisor' AND entity_id = '"+sv.supervisorGRN()+"'")
			}
			want := 0
			if on {
				want = 1
			}
			assert.Equal(t, want, marked(standing), "a registration that stands")
			assert.Zero(t, marked(ended), "one that ended is not marked")

			_, err = p.Exec(ctx, "UPDATE "+schema+".agent_supervisors SET ended_at = now() WHERE grn = '"+standing.supervisorGRN()+"'")
			require.NoError(t, err)
			assert.Equal(t, want, marked(standing), "the trigger marks as the feed says")

			require.NoError(t, m.Migrate(79), "000080 rolls back")
			assert.Zero(t, countRows(t, p, "SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = '"+schema+"' AND t.tgname IN ('policy_outbox_agent_supervisors', 'policy_outbox_agent_assignments')"))
			assert.Zero(t, marked(standing), "the marks of the kind went with it")
			_, err = p.Exec(ctx, "INSERT INTO "+schema+".policy_outbox (entity_type, entity_id) VALUES ('Iam::Supervisor', 'x')")
			assert.Error(t, err, "and the kind is refused again")
		})
	}
}

// TestPolicyRaised_AnOrganizationDeleteRecordsTheSupervisorsItTakes
// holds that the recording of an organization's deletion (ADR-0165
// item 3) names the supervisor the cascade removes with its agent, for
// a registration holding its agent and one whose agent holds no
// assignment, and not the supervisor of another organization.
func TestPolicyRaised_AnOrganizationDeleteRecordsTheSupervisorsItTakes(t *testing.T) {
	p, _, _ := seedDelivered(t)
	holding := supervisorSeed{org: "acme", agent: "held", supervisor: "sv1"}
	away := supervisorSeed{org: "acme", agent: "away", supervisor: "sv2"}
	other := supervisorSeed{org: "globex", agent: "g1", supervisor: "gs1"}
	for _, s := range []supervisorSeed{holding, away, other} {
		s.exec(t, p, s.agentSQL("")...)
		s.exec(t, p, s.registrationSQL("")...)
	}
	holding.exec(t, p, holding.assignSQL("", holding.supervisorGRN()))
	other.exec(t, p, other.assignSQL("", other.supervisorGRN()))
	ctx, raised := policyfeed.WithRaised(context.Background())

	require.NoError(t, orgpg.New(p).DeleteOrg(ctx, mustGRN(t, outboxOrg)))

	var recorded []string
	for _, e := range raised.Entities() {
		recorded = append(recorded, e.Type+" "+e.ID)
	}
	assert.Contains(t, recorded, supervisorMark(holding.supervisorGRN()))
	assert.Contains(t, recorded, supervisorMark(away.supervisorGRN()))
	assert.NotContains(t, recorded, supervisorMark(other.supervisorGRN()))
}
