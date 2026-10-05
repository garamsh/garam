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

// cutoverMigrationRows writes, in schema, an organization, its hosted
// operator's grant covering agent:cutover, an agent:cutover authority
// bound to an assignment and a request target, and a legacy agent with
// its execution row, and answers a function storing one attempt of
// that agent at stage.
func cutoverMigrationRows(t *testing.T, p *pgxpool.Pool, schema string) func(stage string) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		_, err := p.Exec(ctx, strings.ReplaceAll(sql, "§", schema+"."), args...)
		require.NoError(t, err)
	}
	org, hosted := "grn:root:default:org:mig", "grn:mig:default:operator:hosted"
	controller, agentGRN := "grn:mig:default:operator:controller", "grn:mig:default:agent:a1"
	exec(`INSERT INTO §orgs (grn, name, created_at, updated_at) VALUES ($1, 'mig', now(), now())`, org)
	exec(`INSERT INTO §operators (grn, org_grn, registered_by, created_at, name)
		VALUES ($1, $2, 'grn:root:default:user:ada', now(), 'hosted')`, hosted, org)
	exec(`INSERT INTO §operator_delegations
		(id, org_grn, operator_grn, request_id, operations, expires_at, granted_by, granted_at)
		VALUES ('g-1', $1, $2, 'r-1', ARRAY['agent:activate', 'agent:cutover'], now() + interval '1 day',
		        'grn:root:default:user:ada', now())`, org, hosted)
	exec(`INSERT INTO §operation_authorities
		(ref, org_grn, audience_grn, request_id, delegation_id, actor_grn, operation, target_grn,
		 assignee_grn, assignment_epoch, body_sha256, request_target, token_hash, token_expires_at, created_at)
		VALUES ('cutover-ref', $1, $2, 'cutover-ref', 'g-1', 'grn:root:default:user:ada', 'agent:cutover', $3,
		        $4, '1', $5, $6, 'cutover-ref', now() + interval '5 minutes', now())`,
		org, hosted, agentGRN, controller, emptyBodySHA256, "/agents/"+agentGRN+"/cutover/switch")
	exec(`INSERT INTO §agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
		VALUES ($1, $2, 'pending', 'unknown', now(), now())`, agentGRN, org)
	exec(`INSERT INTO §agent_executions (agent_grn, updated_at) VALUES ($1, now())`, agentGRN)
	return func(stage string) {
		t.Helper()
		var switchedAt, endedAt any
		switch stage {
		case "switched":
			switchedAt = "now"
		case "rolled_back":
			endedAt = "now"
		}
		exec(`INSERT INTO §agent_cutovers
			(agent_grn, import_id, stage, frozen_digest, audience_grn, grant_id, operation_ref,
			 created_at, switched_at, ended_at)
			VALUES ($1, $2, $2, sha256('x'::bytea), $3, 'g-1', 'cutover-ref', now(),
			        $4::timestamptz, $5::timestamptz)`,
			agentGRN, stage, hosted, switchedAt, endedAt)
	}
}

func authorityCount(t *testing.T, p *pgxpool.Pool, schema string) int {
	t.Helper()
	var n int
	require.NoError(t, p.QueryRow(context.Background(),
		"SELECT count(*) FROM "+schema+".operation_authorities WHERE operation = 'agent:cutover'").Scan(&n))
	return n
}

// TestMigration000033_RollsBackAndReappliesAcross000032 takes the
// schema down through 000033 and 000032 and up again over a stored
// agent:cutover authority and grant, with no cutover held: every check
// either migration (re)adds is NOT VALID, so the kept rows stop neither
// from applying again, and none is deleted.
func TestMigration000033_RollsBackAndReappliesAcross000032(t *testing.T) {
	const schema = "migrate_000033_chain"
	p := pool(t)
	m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Up())
	cutoverMigrationRows(t, p, schema)

	require.NoError(t, m.Migrate(32), "000033 down")
	require.NoError(t, m.Migrate(31), "000032 down")
	require.NoError(t, m.Migrate(32), "000032 up over the kept rows")
	require.NoError(t, m.Migrate(33), "000033 up over the kept rows")
	version, dirty := schemaVersion(t, p, schema)
	assert.Equal(t, 33, version)
	assert.False(t, dirty)
	assert.Equal(t, 1, authorityCount(t, p, schema), "no authority is deleted")
	assert.True(t, tableExists(t, p, schema, "agent_cutovers"))
}

// TestMigration000033_RefusesItsRollbackWhileACutoverIsHeld: a frozen
// or switched attempt is what keeps its agent off its operator's poll
// and its legacy certificate route, so 000033's rollback refuses while
// one exists and changes nothing. A rolled-back attempt holds nothing,
// and the same rollback over it applies, which is the control.
func TestMigration000033_RefusesItsRollbackWhileACutoverIsHeld(t *testing.T) {
	for i, stage := range []string{"frozen", "switched", "rolled_back"} {
		t.Run(stage, func(t *testing.T) {
			schema := "migrate_000033_held_" + []string{"a", "b", "c"}[i]
			p := pool(t)
			m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
			require.NoError(t, err)
			t.Cleanup(func() { _, _ = m.Close() })
			require.NoError(t, m.Up())
			cutoverMigrationRows(t, p, schema)(stage)

			err = m.Migrate(32)
			if stage == "rolled_back" {
				require.NoError(t, err, "control: an ended attempt holds nothing")
				assert.False(t, tableExists(t, p, schema, "agent_cutovers"))
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "requires the reverse migration")
			assert.True(t, tableExists(t, p, schema, "agent_cutovers"), "the refused rollback changed nothing")
			var held int
			require.NoError(t, p.QueryRow(context.Background(),
				"SELECT count(*) FROM "+schema+".agent_cutovers WHERE stage = $1", stage).Scan(&held))
			assert.Equal(t, 1, held)
		})
	}
}
