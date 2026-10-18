package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// closedMessage is 000035's refusal of a definition write.
const closedMessage = "agent_definitions accepts no desired value"

// withDefinitionsOpen runs write in a transaction with 000035's guard
// off and commits it. Nothing in garam writes a definition any more
// (ADR-0090), so a test stands one up the way a release before 000035
// wrote it; every other trigger, the foreign keys included, stays on.
func withDefinitionsOpen(t *testing.T, p *pgxpool.Pool, write func(tx pgx.Tx)) {
	t.Helper()
	ctx := context.Background()
	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, "ALTER TABLE agent_definitions DISABLE TRIGGER agent_definitions_closed")
	require.NoError(t, err)
	write(tx)
	_, err = tx.Exec(ctx, "ALTER TABLE agent_definitions ENABLE TRIGGER agent_definitions_closed")
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

// keepDefinition writes d as a definition kept from before garam
// stopped accepting them.
func keepDefinition(t *testing.T, p *pgxpool.Pool, d *agent.Definition) {
	t.Helper()
	values, err := json.Marshal(d.Values())
	require.NoError(t, err)
	withDefinitionsOpen(t, p, func(tx pgx.Tx) {
		_, err := tx.Exec(context.Background(), `INSERT INTO agent_definitions
			(agent_grn, org_grn, operator_grn, name, agent_values, defined_by, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			d.Agent().String(), d.Organization().String(), d.Operator().String(), string(d.Name),
			values, d.DefinedBy().String(), d.CreatedAt())
		require.NoError(t, err)
	})
}

// requireClosed asserts err is 000035's refusal.
func requireClosed(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "P0001", pgErr.Code, "raised by the closure guard")
	assert.Equal(t, closedMessage, pgErr.Message)
}

// TestAgentDefinition_TheSchemaRefusesEveryNewValue holds 000035's
// guard against writes sent straight to the table, as a release still
// serving the define route would send them. A new definition, a
// rewrite of a kept one's values or operator, and a direct delete are
// each refused and change nothing. The controls are a rename, which is
// admitted, the organization's deletion, whose cascade takes the
// definition, and the same insert with the guard off, which is
// accepted: what refuses each write is the guard, and not a check or a
// key it breaks.
func TestAgentDefinition_TheSchemaRefusesEveryNewValue(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := agent.NewPostgresStore(p)
	kept := defineAgent(t, p, s, scopedGRN("acme", agent.ServiceTypeOperator, "one"), agent.Values{"model": "haiku"})
	other := registerOperator(t, p, scopedGRN("acme", agent.ServiceTypeOperator, "two"))
	insert := `INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, name, agent_values, defined_by, created_at)
		VALUES ('grn:acme:default:agent:00000000000000ff', 'grn:root:default:org:acme', '` + kept.Operator().String() + `',
		        'Scout', '{"model":"opus","crd-schema":"openAPIV3Schema"}', 'grn:acme:default:user:ada', now())`

	for name, write := range map[string]struct {
		query string
		args  []any
	}{
		"a new definition":  {insert, nil},
		"a value rewritten": {`UPDATE agent_definitions SET agent_values = '{"model":"opus"}' WHERE agent_grn = $1`, []any{kept.Agent().String()}},
		"a value added": {`UPDATE agent_definitions SET agent_values = agent_values || '{"ego":"curious"}' WHERE agent_grn = $1`,
			[]any{kept.Agent().String()}},
		"the operator moved": {`UPDATE agent_definitions SET operator_grn = $2 WHERE agent_grn = $1`,
			[]any{kept.Agent().String(), other.String()}},
		"a direct delete": {`DELETE FROM agent_definitions WHERE agent_grn = $1`, []any{kept.Agent().String()}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := p.Exec(ctx, write.query, write.args...)
			requireClosed(t, err)
			assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM agent_definitions`))
			read, err := s.GetDefinition(ctx, kept.Agent())
			require.NoError(t, err)
			assert.Equal(t, agent.Values{"model": "haiku"}, read.Values(), "the refused write changed nothing")
			assert.Equal(t, kept.Operator(), read.Operator())
		})
	}

	renamed, err := s.RenameDefinition(ctx, kept.Agent(), "Ranger")
	require.NoError(t, err, "control: a rename is admitted")
	assert.Equal(t, agent.Name("Ranger"), renamed.Name)
	assert.Equal(t, agent.Values{"model": "haiku"}, renamed.Values())

	withDefinitionsOpen(t, p, func(tx pgx.Tx) {
		_, err := tx.Exec(ctx, insert)
		assert.NoError(t, err, "control: the same insert without the guard is accepted")
	})
	require.NoError(t, orgpg.New(p).DeleteOrg(ctx, orgRootGRN("acme")))
	assert.Zero(t, countRows(t, p, `SELECT count(*) FROM agent_definitions`),
		"control: a definition still goes with its organization")
}

// TestMigration000035_KeepsEveryDefinitionAndRollsBack migrates a
// schema of its own to 000034, keeps a definition and its claim there,
// and applies 000035. The definition, its values and its claim read as
// they were, the operator's poll still answers it, and a new
// definition is refused; below 000035 the same insert is accepted
// again, which is the control that the refusal is 000035's.
func TestMigration000035_KeepsEveryDefinitionAndRollsBack(t *testing.T) {
	ctx := context.Background()
	databaseURL := isolatedSchema(t, pool(t), "close_000035")
	m, err := migrate.New("file://"+migrationsDir(t), databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(34))
	sp, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(sp.Close)
	const (
		orgGRN      = "grn:root:default:org:cls"
		operatorGRN = "grn:cls:default:operator:one"
		agentGRN    = "grn:cls:default:agent:9f2ac1b40d8e7a35"
	)
	for _, statement := range []string{
		`INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('` + orgGRN + `', 'cls', now(), now())`,
		`INSERT INTO operators (grn, org_grn, registered_by, created_at, name)
			VALUES ('` + operatorGRN + `', '` + orgGRN + `', 'grn:cls:default:user:ada', now(), 'one')`,
		`INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, agent_values, defined_by, created_at, name)
			VALUES ('` + agentGRN + `', '` + orgGRN + `', '` + operatorGRN + `',
			        '{"model":"haiku","tools-pins-github":"v1"}', 'grn:cls:default:user:ada', now(), 'Scout')`,
	} {
		_, err := sp.Exec(ctx, statement)
		require.NoError(t, err)
	}
	snapshot := func() string {
		var row string
		require.NoError(t, sp.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(r))::text FROM agent_definitions r`).Scan(&row))
		return row
	}
	before := snapshot()
	insert := `INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, agent_values, defined_by, created_at, name)
		VALUES ('grn:cls:default:agent:00000000000000ff', '` + orgGRN + `', '` + operatorGRN + `',
		        '{"model":"opus"}', 'grn:cls:default:user:ada', now(), 'New')`

	require.NoError(t, m.Migrate(35))
	assert.Equal(t, before, snapshot(), "no definition changed")
	s := agent.NewPostgresStore(sp)
	states, err := s.ListDefinitionsByOperator(ctx, scopedGRN("cls", agent.ServiceTypeOperator, "one"))
	require.NoError(t, err)
	require.Len(t, states, 1, "the operator's poll still answers the kept definition")
	assert.Equal(t, agent.Values{"model": "haiku", "tools-pins-github": "v1"}, states[0].Definition().Values())
	_, err = s.Claim(ctx, states[0].Definition().Agent(), states[0].Definition().Operator())
	require.NoError(t, err, "the kept definition is still claimable")
	assert.Equal(t, before, snapshot(), "claiming wrote no definition")
	_, err = sp.Exec(ctx, insert)
	requireClosed(t, err)

	require.NoError(t, m.Migrate(34), "000035 down")
	_, err = sp.Exec(ctx, insert)
	require.NoError(t, err, "control: below 000035 the insert is accepted")
	require.NoError(t, m.Migrate(35), "000035 up again")
	_, err = sp.Exec(ctx, `UPDATE agent_definitions SET agent_values = '{}' WHERE agent_grn = '`+agentGRN+`'`)
	requireClosed(t, err)
}

// TestAgentDefinition_NoRoleDefinesAndAManagedCreateStands is ADR-0090
// at both listeners over one database. An owner and a member each send
// the definition the browser specification accepted before, values
// included, and each is answered 405 with no definition written. The
// control is the member's managed creation through the hosted control
// service, which still creates the agent and writes no definition
// either: an organization can still create an agent.
func TestAgentDefinition_NoRoleDefinesAndAManagedCreateStands(t *testing.T) {
	s := newManagedStack(t)
	definitions := func() int { return countRows(t, s.pool, `SELECT count(*) FROM agent_definitions`) }
	before := definitions()
	body := `{"name":"Scout","operatorGrn":"` + s.controller.String() +
		`","values":{"model":"opus","ego":"curious","crd-schema":"openAPIV3Schema"}}`

	for name, cookie := range map[string]*http.Cookie{"owner": s.owner, "member": s.member} {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, s.browser+"/orgs/acme/agent-definitions", strings.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(cookie)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			_ = resp.Body.Close()
			assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
			assert.Equal(t, before, definitions(), "no definition was written")
		})
	}

	created := s.created(t, "create-after-closure")
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agents WHERE grn = $1`, created.agent.String()),
		"control: the managed creation wrote its agent")
	assert.Equal(t, before, definitions(), "and no definition")
}
