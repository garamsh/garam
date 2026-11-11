package integration_test

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
)

// TestMigration000069_DropsTheChecksThatReadAGRNAndRewritesNoRow
// migrates a schema of its own to 000068 and applies 000069 over a
// database holding a name in the five-segment form. Before it, a name
// in ADR-0142's grammar is refused by the check that reads a name at
// the five-segment positions; after it no constraint of the schema
// reads a GRN with split_part, the same name is stored, and the row in
// the five-segment form is as it was: the migration rewrites nothing.
// 000069 down is applied and runs over the result.
func TestMigration000069_DropsTheChecksThatReadAGRNAndRewritesNoRow(t *testing.T) {
	ctx := context.Background()
	const schema = "grn_grammar_000069"
	databaseURL := isolatedSchema(t, pool(t), schema)
	m, err := migrate.New("file://"+migrationsDir(t), databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(68))
	sp := connect(t, databaseURL, nil)

	former := formerGRN("grm", "org", "grm")
	formerAgent := formerGRN("grm", "agent", "9f2ac1b40d8e7a35")
	org := grn.New(grn.RootOwner, "org", "grm2")
	agent := grn.New("grm2", "agent", "9f2ac1b40d8e7a35")

	insertAgent := func(agentName, orgName string) error {
		_, err := sp.Exec(ctx, `INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
			VALUES ($1, $2, 'pending', 'unknown', now(), now())`, agentName, orgName)
		return err
	}
	for _, statement := range []string{
		`INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('` + former + `', 'grm', now(), now())`,
		`INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('` + org.String() + `', 'grm2', now(), now())`,
	} {
		_, err := sp.Exec(ctx, statement)
		require.NoError(t, err, statement)
	}
	require.NoError(t, insertAgent(formerAgent, former), "a five-segment row is stored before the migration")
	require.Error(t, insertAgent(agent.String(), org.String()),
		"the control: before the migration the grammar's name is refused by a check")

	splitChecks := func() int {
		var n int
		require.NoError(t, sp.QueryRow(ctx, `SELECT count(*) FROM pg_constraint
			WHERE contype = 'c' AND connamespace = current_schema()::regnamespace
			AND pg_get_constraintdef(oid) LIKE '%split_part%'`).Scan(&n))
		return n
	}
	require.NotZero(t, splitChecks(), "the control: 000068 holds checks that split a GRN")

	require.NoError(t, m.Migrate(69))
	assert.Zero(t, splitChecks(), "no constraint reads a GRN with split_part")
	require.NoError(t, insertAgent(agent.String(), org.String()), "the grammar's name is stored")

	var stored string
	require.NoError(t, sp.QueryRow(ctx, `SELECT grn FROM agents WHERE grn = $1`, formerAgent).Scan(&stored))
	assert.Equal(t, formerAgent, stored, "the five-segment row keeps its text")
	var rows int
	require.NoError(t, sp.QueryRow(ctx, `SELECT count(*) FROM orgs WHERE grn = $1`, former).Scan(&rows))
	assert.Equal(t, 1, rows, "the five-segment org row is not rewritten")

	var trigger int
	require.NoError(t, sp.QueryRow(ctx, `SELECT count(*) FROM pg_trigger
		WHERE tgname = 'policy_attachments_same_org'`).Scan(&trigger))
	assert.Zero(t, trigger, "the attachment trigger is dropped: the policy server compares orgs")

	require.NoError(t, m.Migrate(68), "000069 down runs over the rows it left")
	require.NoError(t, sp.QueryRow(ctx, `SELECT count(*) FROM pg_trigger
		WHERE tgname = 'policy_attachments_same_org'`).Scan(&trigger))
	assert.Equal(t, 1, trigger, "000069 down restores the trigger")
}
