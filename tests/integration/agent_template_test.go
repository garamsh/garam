package integration_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// retiredMessage is what 000034's guard raises for any write to a
// template (ADR-0088).
const retiredMessage = "agent_templates is retired read-only"

// templateSeq numbers the templates keepTemplate writes, so each has
// an identifier of its own.
var templateSeq atomic.Uint64

// keepTemplate writes a template of orgName as one kept before
// templates were retired, and answers its GRN. 000034 refuses every
// write, so the row is written with the session's ordinary triggers
// off, which the suite's superuser may do; nothing in garam writes
// one.
func keepTemplate(t *testing.T, p *pgxpool.Pool, orgName, name string, values agent.Values) grn.GRN {
	t.Helper()
	ctx := context.Background()
	g := scopedGRN(orgName, agent.ServiceTypeTemplate, fmt.Sprintf("%016x", templateSeq.Add(1)))
	if values == nil {
		values = agent.Values{}
	}
	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "SET LOCAL session_replication_role = replica")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO agent_templates
		(grn, org_grn, name, description, agent_values, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, '', $4, $5, now(), now())`,
		g.String(), orgRootGRN(orgName).String(), name, values, scopedGRN(orgName, "user", "ada").String())
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	return g
}

// requireRetired asserts err is 000034's refusal of a template write.
func requireRetired(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "P0001", pgErr.Code, "raised by the retirement guard")
	assert.Equal(t, retiredMessage, pgErr.Message)
}

// TestAgentTemplate_ARetiredTemplateIsReadAsKept reads kept templates
// of two organizations. Each lists its own, ordered by name, and reads
// one back as it was written; deleting one organization takes its
// templates through the cascade the guard admits, and leaves the
// other's.
func TestAgentTemplate_ARetiredTemplateIsReadAsKept(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))
	s := agent.NewPostgresStore(p)

	writer := keepTemplate(t, p, "acme", "Writer", agent.Values{"workspace": "papers"})
	keepTemplate(t, p, "acme", "Analyst", nil)
	theirs := keepTemplate(t, p, "globex", "Spy", nil)

	mine, err := s.ListTemplatesByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	var names []agent.Name
	for _, tpl := range mine {
		names = append(names, tpl.Name)
		assert.Equal(t, orgRootGRN("acme"), tpl.OrgGRN)
	}
	assert.Equal(t, []agent.Name{"Analyst", "Writer"}, names)
	read, err := s.GetTemplate(ctx, writer)
	require.NoError(t, err)
	assert.Equal(t, agent.Values{"workspace": "papers"}, read.Values)
	assert.Equal(t, scopedGRN("acme", "user", "ada"), read.CreatedBy)

	counts, err := s.CountByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	assert.Equal(t, 2, counts.Templates)

	require.NoError(t, orgs.DeleteOrg(ctx, orgRootGRN("acme")))
	counts, err = s.CountByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	assert.Zero(t, counts.Templates, "the organization's deletion took its templates")
	kept, err := s.GetTemplate(ctx, theirs)
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Spy"), kept.Name, "and left the other organization's")
}

// TestAgentTemplate_TheSchemaRefusesEveryWrite holds 000034's guard
// against writes sent straight to the table, as a release still serving
// the old routes would send them. Each is refused and changes nothing.
// The control is the same write with the session's ordinary triggers
// off, which is accepted: what refuses it is the guard, and not a check
// or a key the write breaks.
func TestAgentTemplate_TheSchemaRefusesEveryWrite(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	kept := keepTemplate(t, p, "acme", "Researcher", agent.Values{"workspace": "papers"})

	for name, write := range map[string]struct {
		query string
		args  []any
	}{
		"an insert": {`INSERT INTO agent_templates
			(grn, org_grn, name, description, agent_values, created_by, created_at, updated_at)
			VALUES ('grn:acme:default:agent-template:00000000000000ff', 'grn:root:default:org:acme', 'New', '', '{}',
			        'grn:acme:default:user:ada', now(), now())`, nil},
		"an update": {`UPDATE agent_templates SET name = 'Analyst' WHERE grn = $1`, []any{kept.String()}},
		"a delete":  {`DELETE FROM agent_templates WHERE grn = $1`, []any{kept.String()}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := p.Exec(ctx, write.query, write.args...)
			requireRetired(t, err)
			read, err := agent.NewPostgresStore(p).GetTemplate(ctx, kept)
			require.NoError(t, err)
			assert.Equal(t, agent.Name("Researcher"), read.Name, "the refused write changed nothing")
			assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM agent_templates`))

			tx, err := p.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, "SET LOCAL session_replication_role = replica")
			require.NoError(t, err)
			_, err = tx.Exec(ctx, write.query, write.args...)
			assert.NoError(t, err, "control: the same write without the guard is accepted")
		})
	}
}

// TestAgentTemplate_ADefinitionKeepsWhatItCopied is ADR-0075's
// copy-once held across the retirement. A definition is written from a
// kept template's values, edited first, the way the console wrote one.
// The template's retirement leaves the definition's name, operator and
// values exactly as they were, and the template's own refused edit is
// the control that the retirement is in force.
func TestAgentTemplate_ADefinitionKeepsWhatItCopied(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	kept := keepTemplate(t, p, "acme", "Researcher", agent.Values{"workspace": "papers", "notes": "weekly"})
	stored, err := s.GetTemplate(ctx, kept)
	require.NoError(t, err)

	copied := agent.Values{}
	for k, v := range stored.Values {
		copied[k] = v
	}
	copied["notes"] = "daily"
	operatorGRN := registerOperator(t, p, scopedGRN("acme", agent.ServiceTypeOperator, "one"))
	d, err := agent.NewDefinition(orgRootGRN("acme"), operatorGRN, "Scout", copied, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	keepDefinition(t, p, d)

	_, err = p.Exec(ctx, `UPDATE agent_templates SET agent_values = '{}' WHERE grn = $1`, kept.String())
	requireRetired(t, err)
	after, err := s.GetDefinition(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Scout"), after.Name)
	assert.Equal(t, operatorGRN, after.Operator())
	assert.Equal(t, agent.Values{"workspace": "papers", "notes": "daily"}, after.Values())
}

// TestAgentDefinition_RenameMovesOnlyTheName renames a claimed
// definition. The name changes; the operator, the values, the GRN and
// the assignment the claim wrote do not, so an operator already
// running the agent is bound as it was.
func TestAgentDefinition_RenameMovesOnlyTheName(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	d := defineAgent(t, p, s, scopedGRN("acme", agent.ServiceTypeOperator, "one"), agent.Values{"workspace": "papers"})
	claim, err := s.Claim(ctx, d.Agent(), d.Operator())
	require.NoError(t, err)

	renamed, err := s.RenameDefinition(ctx, d.Agent(), "Scout")
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Scout"), renamed.Name)
	assert.Equal(t, d.Agent(), renamed.Agent())
	assert.Equal(t, d.Operator(), renamed.Operator())
	assert.Equal(t, d.Values(), renamed.Values())

	read, err := s.GetDefinition(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Scout"), read.Name)
	assignment, err := s.GetAssignment(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, claim.Epoch(), assignment.Epoch(), "the claim's binding is where it was")
	assert.Equal(t, d.Operator(), assignment.Assignee())

	_, err = s.RenameDefinition(ctx, scopedGRN("acme", agent.ServiceType, "0000000000000000"), "Ghost")
	assert.ErrorIs(t, err, agent.ErrDefinitionNotFound)
}

// TestAgentDefinition_TheSchemaRefusesWhatTheDomainRefuses holds
// 000026's checks on a definition's name as the copy no call path
// routes around. The writes go straight to the table, and the write
// within every bound is the control. A template's own checks are
// behind its retirement, which refuses every write first.
func TestAgentDefinition_TheSchemaRefusesWhatTheDomainRefuses(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	d := defineAgent(t, p, s, scopedGRN("acme", agent.ServiceTypeOperator, "one"), nil)

	for name, write := range map[string]struct {
		query string
		arg   string
	}{
		"a blank agent name":    {`UPDATE agent_definitions SET name = '' WHERE agent_grn = $1`, d.Agent().String()},
		"an agent name of null": {`UPDATE agent_definitions SET name = NULL WHERE agent_grn = $1`, d.Agent().String()},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := p.Exec(ctx, write.query, write.arg)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			assert.Contains(t, []string{"23514", "23502"}, pgErr.Code)
		})
	}

	_, err := p.Exec(ctx, `UPDATE agent_definitions SET name = repeat('가', 100) WHERE agent_grn = $1`, d.Agent().String())
	assert.NoError(t, err)
}

// TestMigration26_NamesEveryExistingDefinitionByItsIdentifier migrates
// a schema of its own to the version before 000026, writes two
// definitions as they were written then, and applies 000026. Each is
// still there and called by its identifier, which is what the console
// showed it by; two identifiers answering two names is the control for
// the backfill reading the GRN.
func TestMigration26_NamesEveryExistingDefinitionByItsIdentifier(t *testing.T) {
	const schema = "migration_26_test"
	p := pool(t)
	databaseURL := isolatedSchema(t, p, schema)
	dir := t.TempDir()

	entries, err := os.ReadDir(migrationsDir(t))
	require.NoError(t, err)
	var later []string
	for _, e := range entries {
		version, _, ok := strings.Cut(e.Name(), "_")
		n, err := strconv.Atoi(version)
		if !ok || err != nil || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if n >= 26 {
			later = append(later, e.Name())
			continue
		}
		copyMigration(t, dir, e.Name())
	}
	require.Contains(t, later, "000026_agent_templates.up.sql")
	require.NoError(t, runMigrate(t, dir, databaseURL))
	version, _ := schemaVersion(t, p, schema)
	require.Equal(t, 25, version)

	ctx := context.Background()
	for _, statement := range []string{
		`INSERT INTO %s.orgs (grn, name, created_at, updated_at)
			VALUES ('grn:root:default:org:acme', 'acme', now(), now())`,
		`INSERT INTO %s.operators (grn, org_grn, name, registered_by, created_at)
			VALUES ('grn:acme:default:operator:one', 'grn:root:default:org:acme', 'one', 'grn:acme:default:user:ada', now())`,
		`INSERT INTO %s.agent_definitions (agent_grn, org_grn, operator_grn, agent_values, defined_by, created_at) VALUES
			('grn:acme:default:agent:9f2ac1b40d8e7a35', 'grn:root:default:org:acme', 'grn:acme:default:operator:one',
			 '{}', 'grn:acme:default:user:ada', now()),
			('grn:acme:default:agent:0a1b2c3d4e5f6071', 'grn:root:default:org:acme', 'grn:acme:default:operator:one',
			 '{}', 'grn:acme:default:user:ada', now())`,
	} {
		_, err = p.Exec(ctx, fmt.Sprintf(statement, schema))
		require.NoError(t, err)
	}

	for _, name := range later {
		copyMigration(t, dir, name)
	}
	require.NoError(t, runMigrate(t, dir, databaseURL))

	rows, err := p.Query(ctx, fmt.Sprintf(`SELECT agent_grn, name FROM %s.agent_definitions ORDER BY agent_grn`, schema))
	require.NoError(t, err)
	defer rows.Close()
	var got [][2]string
	for rows.Next() {
		var row [2]string
		require.NoError(t, rows.Scan(&row[0], &row[1]))
		got = append(got, row)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, [][2]string{
		{"grn:acme:default:agent:0a1b2c3d4e5f6071", "0a1b2c3d4e5f6071"},
		{"grn:acme:default:agent:9f2ac1b40d8e7a35", "9f2ac1b40d8e7a35"},
	}, got)
}

// TestAgentDefinition_ATeamHoldsAnAgentBeforeAnyOperatorClaimsIt adds a
// defined agent no operator has claimed to a team. A membership is the
// agent's GRN, which its definition minted, so the membership is stored
// and read back although no `agents` row exists yet (ADR-0075). The
// agent row's absence is asserted beside it, so what the team holds is
// shown to be the unclaimed agent and not one an operator reported.
func TestAgentDefinition_ATeamHoldsAnAgentBeforeAnyOperatorClaimsIt(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	d := defineAgent(t, p, s, scopedGRN("acme", agent.ServiceTypeOperator, "one"), nil)
	team := scopedGRN("acme", org.ServiceTypeGroup, "eng")
	require.NoError(t, orgs.CreateGroup(ctx, org.NewGroup(team, "Engineering", orgRootGRN("acme"))))

	_, err := s.Get(ctx, d.Agent())
	require.ErrorIs(t, err, agent.ErrAgentNotFound, "no operator has claimed it")
	require.NoError(t, orgs.AddGroupMember(ctx, team, d.Agent()))

	members, err := orgs.ListGroupMembers(ctx, team)
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{d.Agent()}, members)
}
