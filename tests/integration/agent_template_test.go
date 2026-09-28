package integration_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// templateContent builds content the domain accepts, for a test that is
// not about what it refuses.
func templateContent(t *testing.T, name, description string, values agent.Values) agent.TemplateContent {
	t.Helper()
	n, err := agent.ParseName(name)
	require.NoError(t, err)
	d, err := agent.ParseDescription(description)
	require.NoError(t, err)
	return agent.TemplateContent{Name: n, Description: d, Values: values}
}

// TestAgentTemplate_AnAgentKeepsWhatItCopied is ADR-0075's
// independence against real Postgres. A definition is written from a
// template's values the way the console writes one: the values are
// copied into the request, once. Editing the template and then deleting
// it leaves the definition's name, operator, values and GRN exactly as
// they were, and the template's own row is the control that the edit
// and the delete happened.
func TestAgentTemplate_AnAgentKeepsWhatItCopied(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	ada := scopedGRN("acme", "user", "ada")

	tpl, err := agent.NewTemplate(orgRootGRN("acme"),
		templateContent(t, "Researcher", "Reads papers", agent.Values{"model": "haiku", "ego": "curious"}), ada)
	require.NoError(t, err)
	require.NoError(t, s.CreateTemplate(ctx, tpl))
	stored, err := s.GetTemplate(ctx, tpl.GRN)
	require.NoError(t, err)
	assert.Equal(t, tpl.TemplateContent, stored.TemplateContent)
	assert.Equal(t, ada, stored.CreatedBy)

	// The copy the console makes: the template's values, edited before
	// the definition is written.
	copied := agent.Values{}
	for k, v := range stored.Values {
		copied[k] = v
	}
	copied["ego"] = "careful"
	operatorGRN := registerOperator(t, p, scopedGRN("acme", agent.ServiceTypeOperator, "one"))
	d, err := agent.NewDefinition(orgRootGRN("acme"), operatorGRN, "Scout", copied, ada)
	require.NoError(t, err)
	require.NoError(t, s.Define(ctx, d))

	updated, err := s.UpdateTemplate(ctx, tpl.GRN,
		templateContent(t, "Analyst", "", agent.Values{"model": "sonnet"}))
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Analyst"), updated.Name)
	assert.Equal(t, agent.Values{"model": "sonnet"}, updated.Values)
	assert.True(t, updated.UpdatedAt.After(stored.UpdatedAt) || updated.UpdatedAt.Equal(stored.UpdatedAt))

	after, err := s.GetDefinition(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Scout"), after.Name())
	assert.Equal(t, agent.Values{"model": "haiku", "ego": "careful"}, after.Values(),
		"the template's edit reached no agent")

	require.NoError(t, s.DeleteTemplate(ctx, tpl.GRN))
	_, err = s.GetTemplate(ctx, tpl.GRN)
	assert.ErrorIs(t, err, agent.ErrTemplateNotFound)
	assert.ErrorIs(t, s.DeleteTemplate(ctx, tpl.GRN), agent.ErrTemplateNotFound)

	after, err = s.GetDefinition(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Scout"), after.Name())
	assert.Equal(t, operatorGRN, after.Operator())
	assert.Equal(t, agent.Values{"model": "haiku", "ego": "careful"}, after.Values(),
		"the template's deletion reached no agent")
}

// TestAgentTemplate_BelongsToOneOrganization lists, reads and counts
// templates of two organizations. Each lists its own, ordered by name;
// deleting one organization takes its templates and leaves the other's.
func TestAgentTemplate_BelongsToOneOrganization(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))
	s := agent.NewPostgresStore(p)

	create := func(orgName, name string) *agent.Template {
		tpl, err := agent.NewTemplate(orgRootGRN(orgName), templateContent(t, name, "", nil), scopedGRN(orgName, "user", "ada"))
		require.NoError(t, err)
		require.NoError(t, s.CreateTemplate(ctx, tpl))
		return tpl
	}
	create("acme", "Writer")
	create("acme", "Analyst")
	theirs := create("globex", "Spy")

	mine, err := s.ListTemplatesByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	var names []agent.Name
	for _, tpl := range mine {
		names = append(names, tpl.Name)
		assert.Equal(t, orgRootGRN("acme"), tpl.OrgGRN)
		assert.Equal(t, agent.Values{}, tpl.Values, "an empty payload rather than none")
	}
	assert.Equal(t, []agent.Name{"Analyst", "Writer"}, names)

	counts, err := s.CountByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	assert.Equal(t, 2, counts.Templates)

	require.NoError(t, orgs.DeleteOrg(ctx, orgRootGRN("acme")))
	counts, err = s.CountByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	assert.Zero(t, counts.Templates, "the organization's deletion took its templates")
	kept, err := s.GetTemplate(ctx, theirs.GRN)
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Spy"), kept.Name, "and left the other organization's")
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
	d := defineAgent(t, p, s, scopedGRN("acme", agent.ServiceTypeOperator, "one"), agent.Values{"model": "haiku"})
	claim, err := s.Claim(ctx, d.Agent(), d.Operator())
	require.NoError(t, err)

	renamed, err := s.RenameDefinition(ctx, d.Agent(), "Scout")
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Scout"), renamed.Name())
	assert.Equal(t, d.Agent(), renamed.Agent())
	assert.Equal(t, d.Operator(), renamed.Operator())
	assert.Equal(t, d.Values(), renamed.Values())

	read, err := s.GetDefinition(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.Name("Scout"), read.Name())
	assignment, err := s.GetAssignment(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, claim.Epoch(), assignment.Epoch(), "the claim's binding is where it was")
	assert.Equal(t, d.Operator(), assignment.Operator())

	_, err = s.RenameDefinition(ctx, scopedGRN("acme", agent.ServiceType, "0000000000000000"), "Ghost")
	assert.ErrorIs(t, err, agent.ErrDefinitionNotFound)
}

// TestAgentTemplate_TheSchemaRefusesWhatTheDomainRefuses holds
// 000026's checks as the copy no call path routes around. The writes go
// straight to the tables, and the write within every bound is the
// control.
func TestAgentTemplate_TheSchemaRefusesWhatTheDomainRefuses(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	tpl, err := agent.NewTemplate(orgRootGRN("acme"), templateContent(t, "Researcher", "", nil), scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	require.NoError(t, s.CreateTemplate(ctx, tpl))
	d := defineAgent(t, p, s, scopedGRN("acme", agent.ServiceTypeOperator, "one"), nil)

	for name, write := range map[string]struct {
		query string
		arg   string
	}{
		"a blank template name":    {`UPDATE agent_templates SET name = '  ' WHERE grn = $1`, tpl.GRN.String()},
		"a template name past 100": {`UPDATE agent_templates SET name = repeat('가', 101) WHERE grn = $1`, tpl.GRN.String()},
		"a description past 1000":  {`UPDATE agent_templates SET description = repeat('가', 1001) WHERE grn = $1`, tpl.GRN.String()},
		"values that are an array": {`UPDATE agent_templates SET agent_values = '[]' WHERE grn = $1`, tpl.GRN.String()},
		"a template of another org": {`UPDATE agent_templates SET grn = 'grn:globex:default:agent-template:x' WHERE grn = $1`,
			tpl.GRN.String()},
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

	_, err = p.Exec(ctx,
		`UPDATE agent_templates SET name = repeat('가', 100), description = repeat('가', 1000) WHERE grn = $1`,
		tpl.GRN.String())
	assert.NoError(t, err)
	_, err = p.Exec(ctx, `UPDATE agent_definitions SET name = repeat('가', 100) WHERE agent_grn = $1`, d.Agent().String())
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
