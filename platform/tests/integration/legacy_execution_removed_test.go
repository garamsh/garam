package integration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	legacyAgent = "grn:mig:default:agent:1e9ac1e9ac1e9ac1"
	fencedAgent = "grn:mig:default:agent:fe1cedfe1cedfe1c"
)

// beforeLegacyRemoval migrates a schema of its own to 000057, the
// version before legacy execution goes, and seeds what aurora held: a
// legacy agent with its definition and a message addressed to it, and
// a fenced agent beside it. It answers the migration and a statement
// runner over that schema, `§` standing for the schema's prefix.
func beforeLegacyRemoval(t *testing.T, p *pgxpool.Pool, schema string) (*migrate.Migrate, func(sql string, args ...any) error) {
	t.Helper()
	ctx := context.Background()
	m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(57))

	exec := func(sql string, args ...any) error {
		_, err := p.Exec(ctx, strings.ReplaceAll(sql, "§", schema+"."), args...)
		return err
	}
	org := "grn:root:default:org:mig"
	operator := "grn:mig:default:operator:field"
	require.NoError(t, exec(`INSERT INTO §orgs (grn, name, created_at, updated_at) VALUES ($1, 'mig', now(), now())`, org))
	require.NoError(t, exec(`INSERT INTO §operators (grn, org_grn, registered_by, created_at, name)
		VALUES ($1, $2, 'grn:root:default:user:ada', now(), 'field')`, operator, org))
	for grn, mode := range map[string]string{legacyAgent: "legacy", fencedAgent: "fenced"} {
		require.NoError(t, exec(`INSERT INTO §agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
			VALUES ($1, $2, 'ready', 'connected', now(), now())`, grn, org))
		require.NoError(t, exec(`INSERT INTO §agent_executions (agent_grn, mode, updated_at) VALUES ($1, $2, now())`, grn, mode))
	}
	// 000035 closed the table to new definitions, so its guard alone is
	// off for the one this seeds, as a definition written before it.
	require.NoError(t, exec(`ALTER TABLE §agent_definitions DISABLE TRIGGER agent_definitions_closed`))
	require.NoError(t, exec(`INSERT INTO §agent_definitions (agent_grn, org_grn, operator_grn, agent_values, defined_by, created_at, name)
		VALUES ($1, $2, $3, '{}', 'grn:root:default:user:ada', now(), 'Sherlock')`, legacyAgent, org, operator))
	require.NoError(t, exec(`ALTER TABLE §agent_definitions ENABLE TRIGGER agent_definitions_closed`))
	require.NoError(t, exec(`INSERT INTO §messages (id, recipient_grn, sender_grn, idempotency_key, body, created_at)
		VALUES ('m-1', $1, $2, 'k-1', 'hello', now())`, legacyAgent, fencedAgent))
	return m, exec
}

// countIn answers how many rows of table in schema match where.
func countIn(t *testing.T, p *pgxpool.Pool, schema, table, where string, args ...any) int {
	t.Helper()
	return countRows(t, p, "SELECT count(*) FROM "+schema+"."+table+" WHERE "+where, args...)
}

// TestMigration000058_DeletesTheLegacyAgentAndKeepsEverythingElse holds
// that migration 000058 removes legacy execution as ADR-0118 decides:
// the legacy agent goes with its definition and its execution, while
// the fenced agent and the message addressed to the legacy one stay,
// and the definition table and the mode column are gone.
func TestMigration000058_DeletesTheLegacyAgentAndKeepsEverythingElse(t *testing.T) {
	const schema = "migrate_000058_deletes"
	p := pool(t)
	m, _ := beforeLegacyRemoval(t, p, schema)

	require.NoError(t, m.Migrate(58))

	assert.Zero(t, countIn(t, p, schema, "agents", "grn = $1", legacyAgent), "the legacy agent stayed")
	assert.Zero(t, countIn(t, p, schema, "agent_executions", "agent_grn = $1", legacyAgent), "its execution stayed")
	assert.Equal(t, 1, countIn(t, p, schema, "agents", "grn = $1", fencedAgent), "the fenced agent went")
	assert.Equal(t, 1, countIn(t, p, schema, "messages", "recipient_grn = $1", legacyAgent), "a message went with its recipient")
	assert.False(t, tableExists(t, p, schema, "agent_definitions"))
	assert.False(t, tableExists(t, p, schema, "agent_cutovers"))
	assert.Zero(t, countRows(t, p, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'agent_executions' AND column_name = 'mode'`, schema))
}

// TestMigration000058_RefusesARowOfANonLegacyAgentAndDeletesNothing
// holds that the migration deletes legacy rows only: each row it would
// otherwise take from a fenced agent, or a grant left holding nothing,
// fails it with the exception naming that row, and the legacy agent is
// still there afterwards. The test above, over the same seed without
// the extra row, is the control.
func TestMigration000058_RefusesARowOfANonLegacyAgentAndDeletesNothing(t *testing.T) {
	for _, tc := range []struct {
		name, schema, row, refusal string
	}{
		{
			name:   "a definition of a fenced agent",
			schema: "migrate_000058_definition",
			row: `INSERT INTO §agent_definitions (agent_grn, org_grn, operator_grn, agent_values, defined_by, created_at, name)
				VALUES ('` + fencedAgent + `', 'grn:root:default:org:mig', 'grn:mig:default:operator:field', '{}',
				        'grn:root:default:user:ada', now(), 'Moved')`,
			refusal: "agent_definitions names a fenced agent",
		},
		{
			name:   "a cut-over attempt of a fenced agent",
			schema: "migrate_000058_cutover",
			row: `INSERT INTO §agent_cutovers (agent_grn, import_id, stage, frozen_digest, audience_grn, grant_id,
				                         operation_ref, created_at)
				VALUES ('` + fencedAgent + `', 'import-1', 'frozen', decode(repeat('ab', 32), 'hex'),
				        'grn:mig:default:operator:field', 'g-1', 'ref-1', now())`,
			refusal: "agent_cutovers holds an attempt of a fenced agent",
		},
		{
			name:   "a grant of agent:cutover alone",
			schema: "migrate_000058_grant",
			row: `INSERT INTO §operator_delegations
				(id, org_grn, operator_grn, request_id, operations, expires_at, granted_by, granted_at)
				VALUES ('g-1', 'grn:root:default:org:mig', 'grn:mig:default:operator:field', 'r-1',
				        ARRAY['agent:cutover'], now() + interval '1 day', 'grn:root:default:user:ada', now())`,
			refusal: "operator_delegations holds a grant of agent:cutover alone",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := pool(t)
			m, exec := beforeLegacyRemoval(t, p, tc.schema)
			if strings.Contains(tc.row, "agent_definitions") {
				require.NoError(t, exec(`ALTER TABLE §agent_definitions DISABLE TRIGGER agent_definitions_closed`))
			}
			require.NoError(t, exec(tc.row))

			err := m.Migrate(58)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.refusal)
			assert.Equal(t, 1, countIn(t, p, tc.schema, "agents", "grn = $1", legacyAgent),
				"the refused migration deleted the legacy agent")
			assert.True(t, tableExists(t, p, tc.schema, "agent_definitions"), "the refused migration dropped a table")
		})
	}
}
