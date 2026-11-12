package integration_test

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigration000072_RewritesStoredOperationsAndBack rewrites the
// operations 000071 stored to the dot form by 000072's explicit
// mapping, keeping a grant's order, and restores them on the way down.
// The checks follow the rows in both directions: each form is refused
// where the other is the stored one, and accepted where it is its own,
// which is the control for the refusal.
func TestMigration000072_RewritesStoredOperationsAndBack(t *testing.T) {
	const schema = "migrate_000072_case0"
	p := pool(t)
	m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(71))

	hyphen := []string{"iam:agent-template-read", "iam:agent-create", "iam:agent-execution-read"}
	dotted := []string{"iam:agent-template.read", "iam:agent.create", "iam:agent.execution-read"}
	store := migrationRows(t, p, schema, hyphen)
	require.NoError(t, store("kept", "iam:agent-template-read", "/v1/orgs/mig/templates"))
	assert.Error(t, store("refused-dotted", "iam:agent-template.read", "/v1/orgs/mig/templates"),
		"at 000071 the dot form is refused")

	require.NoError(t, m.Up(), "000072 applies over the rows 000071 kept")
	assert.Equal(t, dotted, storedOperations(t, p, schema), "the grant, in the order it was held")
	assert.Equal(t, []string{"iam:agent-template.read"}, storedAuthorityOperations(t, p, schema))
	assert.Error(t, store("refused-hyphen", "iam:agent-template-read", "/v1/orgs/mig/templates"),
		"at 000072 the hyphen form is refused")
	assert.NoError(t, store("new", "iam:agent-template.read", "/v1/orgs/mig/templates"),
		"the control: the dot form is accepted")

	require.NoError(t, m.Migrate(71), "000072 rolls back")
	assert.Equal(t, hyphen, storedOperations(t, p, schema), "the grant, as 000071 left it")
	assert.Equal(t, []string{"iam:agent-template-read", "iam:agent-template-read"},
		storedAuthorityOperations(t, p, schema))
	assert.Error(t, store("refused-dotted-again", "iam:agent-template.read", "/v1/orgs/mig/templates"))
	assert.NoError(t, store("restored", "iam:agent-template-read", "/v1/orgs/mig/templates"),
		"the control: 000071's form is accepted again")
}

// storedOperations is the operations of the one grant in schema.
func storedOperations(t *testing.T, p *pgxpool.Pool, schema string) []string {
	t.Helper()
	var operations []string
	require.NoError(t, p.QueryRow(context.Background(),
		"SELECT operations FROM "+schema+".operator_delegations WHERE id = 'g-1'").Scan(&operations))
	return operations
}

// storedAuthorityOperations is the operation of each authority stored
// in schema, oldest first.
func storedAuthorityOperations(t *testing.T, p *pgxpool.Pool, schema string) []string {
	t.Helper()
	rows, err := p.Query(context.Background(),
		"SELECT operation FROM "+schema+".operation_authorities ORDER BY created_at, ref")
	require.NoError(t, err)
	defer rows.Close()
	var operations []string
	for rows.Next() {
		var operation string
		require.NoError(t, rows.Scan(&operation))
		operations = append(operations, operation)
	}
	require.NoError(t, rows.Err())
	return operations
}
