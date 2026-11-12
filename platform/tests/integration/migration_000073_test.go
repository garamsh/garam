package integration_test

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigration000073_GivesEachExistingOrganizationACoworkPrincipal
// applies 000073 over organizations that already exist: each gains one
// principal named cowork, GRN grn:<org>/iam/service/cowork, no other
// row changes, the table's check refuses a principal in another
// organization's name, deleting an organization removes its principal,
// and the way down drops the table.
func TestMigration000073_GivesEachExistingOrganizationACoworkPrincipal(t *testing.T) {
	const schema = "migrate_000073_case0"
	ctx := context.Background()
	p := pool(t)
	m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(72))
	assert.False(t, tableExists(t, p, schema, "service_principals"), "none before 000073")

	for _, name := range []string{"acme", "globex"} {
		_, err := p.Exec(ctx, "INSERT INTO "+schema+".orgs (grn, name, created_at, updated_at) VALUES ($1, $2, now(), now())",
			"grn:root/iam/org/"+name, name)
		require.NoError(t, err)
	}

	require.NoError(t, m.Up())
	rows, err := p.Query(ctx, "SELECT grn, org_grn FROM "+schema+".service_principals ORDER BY grn")
	require.NoError(t, err)
	defer rows.Close()
	var got [][2]string
	for rows.Next() {
		var g, o string
		require.NoError(t, rows.Scan(&g, &o))
		got = append(got, [2]string{g, o})
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, [][2]string{
		{"grn:acme/iam/service/cowork", "grn:root/iam/org/acme"},
		{"grn:globex/iam/service/cowork", "grn:root/iam/org/globex"},
	}, got, "one cowork principal per organization that existed")

	_, err = p.Exec(ctx, "INSERT INTO "+schema+".service_principals (grn, org_grn) VALUES ($1, $2)",
		"grn:globex/iam/service/cowork-2", "grn:root/iam/org/acme")
	assert.Error(t, err, "a principal is named for the organization that holds it")

	_, err = p.Exec(ctx, "DELETE FROM "+schema+".orgs WHERE grn = 'grn:root/iam/org/acme'")
	require.NoError(t, err)
	assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM "+schema+".service_principals"),
		"the organization's deletion takes its principal")

	require.NoError(t, m.Migrate(72), "000073 rolls back")
	assert.False(t, tableExists(t, p, schema, "service_principals"))
}
