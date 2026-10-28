package integration_test

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// certificatePathTables are the tables only the certificate path wrote,
// which 000059 drops (ADR-0118).
var certificatePathTables = []string{
	"operator_certificates", "agent_initial_certificates", "agent_supervisor_renewals", "agent_credentials",
}

// certificatePathColumns are the certificate and key-fingerprint
// columns 000059 drops from the tables it keeps, by table.
var certificatePathColumns = map[string][]string{
	"agent_recoveries":  {"certificate_pem", "issuer_pem", "server_root_pem"},
	"agent_supervisors": {"serial", "not_after"},
	"agent_enrollments": {
		"supervisor_certificate_pem", "agent_certificate_pem", "issuer_pem", "server_root_pem",
		"supervisor_key_sha256", "agent_key_sha256",
	},
}

// tablesIn answers which of names schema holds.
func tablesIn(t *testing.T, p *pgxpool.Pool, schema string, names []string) []string {
	t.Helper()
	rows, err := p.Query(context.Background(), `SELECT table_name FROM information_schema.tables
		WHERE table_schema = $1 AND table_name = ANY($2) ORDER BY table_name`, schema, names)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		out = append(out, name)
	}
	require.NoError(t, rows.Err())
	return out
}

// columnsIn answers which of columns schema's table holds.
func columnsIn(t *testing.T, p *pgxpool.Pool, schema, table string, columns []string) []string {
	t.Helper()
	rows, err := p.Query(context.Background(), `SELECT column_name FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = ANY($3) ORDER BY column_name`, schema, table, columns)
	require.NoError(t, err)
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		out = append(out, name)
	}
	require.NoError(t, rows.Err())
	return out
}

// TestMigration000059_RefusesWhatItWouldStrandAndDropsTheCertificatePath
// migrates a schema of its own to 000058 and applies 000059 over what
// an earlier release left. An unspent approval binding a host's keys,
// and a supervisor holding a live certificate, each refuse the
// migration by name and change nothing; the same row once withdrawn, or
// once its certificate has expired, is the control that lets it run.
// It then drops every table and column only the certificate path wrote
// and keeps every row of the tables it alters, and it leaves
// certificate_authorities, whose rows hold a key sealed under the
// key-encryption key, as it was. 000059 down restores the dropped
// tables and columns empty and applies again over them.
func TestMigration000059_RefusesWhatItWouldStrandAndDropsTheCertificatePath(t *testing.T) {
	ctx := context.Background()
	const schema = "certificate_path_000059"
	databaseURL := isolatedSchema(t, pool(t), schema)
	m, err := migrate.New("file://"+migrationsDir(t), databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(58))
	sp := connect(t, databaseURL, nil)
	const (
		orgGRN     = "grn:root:default:org:cert"
		agentGRN   = "grn:cert:default:agent:9f2ac1b40d8e7a35"
		keyedGRN   = "grn:cert:default:agent:0a1b2c3d4e5f6071"
		supervisor = "grn:cert:default:supervisor:4c3b2a1908f7e6d5"
	)
	for _, statement := range []string{
		`INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('` + orgGRN + `', 'cert', now(), now())`,
		`INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
			VALUES ('` + agentGRN + `', '` + orgGRN + `', 'pending', 'unknown', now(), now())`,
		`INSERT INTO agent_executions (agent_grn, lineage, updated_at) VALUES ('` + agentGRN + `', gen_random_uuid(), now())`,
		`INSERT INTO certificate_authorities (org_grn, certificate, sealed_key, not_after, created_at)
			VALUES ('` + orgGRN + `', 'certificate', 'sealed', now() + interval '1 year', now())`,
		// A spent keyless approval and the supervisor it registered,
		// whose certificate is live.
		`INSERT INTO agent_enrollments (id, org_grn, agent_grn, supervisor_grn, host_name, request_id, approved_by,
			secret_sha256, expires_at, created_at, consumed_at, request_sha256, epoch, lineage)
			VALUES ('00000000-0000-0000-0000-000000000001', '` + orgGRN + `', '` + agentGRN + `', '` + supervisor + `',
			'laptop', 'spent', 'grn:root:default:user:ada', sha256('spent'::bytea), now() + interval '1 hour', now(),
			now(), sha256('request'::bytea), 1, gen_random_uuid())`,
		`INSERT INTO agent_supervisors (grn, agent_grn, enrollment_id, host_name, serial, not_after, registered_at)
			VALUES ('` + supervisor + `', '` + agentGRN + `', '00000000-0000-0000-0000-000000000001', 'laptop',
			'1001', now() + interval '1 hour', now())`,
		// An unspent approval binding a host's keys.
		`INSERT INTO agent_enrollments (id, org_grn, agent_grn, supervisor_grn, host_name, request_id, approved_by,
			secret_sha256, expires_at, created_at, supervisor_key_sha256, agent_key_sha256)
			VALUES ('00000000-0000-0000-0000-000000000002', '` + orgGRN + `', '` + keyedGRN + `',
			'grn:cert:default:supervisor:5d4c3b2a1908f7e6', 'laptop', 'keyed', 'grn:root:default:user:ada',
			sha256('keyed'::bytea), now() + interval '1 hour', now(), sha256('s'::bytea), sha256('a'::bytea))`,
	} {
		_, err := sp.Exec(ctx, statement)
		require.NoError(t, err, statement)
	}
	authority := func() string {
		var row string
		require.NoError(t, sp.QueryRow(ctx, `SELECT to_jsonb(c)::text FROM certificate_authorities c`).Scan(&row))
		return row
	}
	sealed := authority()

	refused := func(t *testing.T, named string) {
		t.Helper()
		err := m.Migrate(59)
		require.Error(t, err)
		assert.Contains(t, err.Error(), named)
		require.NoError(t, m.Force(58), "the refused migration is reset to the version it left")
		assert.Equal(t, certificatePathTables, sortedTables(tablesIn(t, sp, schema, certificatePathTables)),
			"a refusal drops nothing")
	}
	refused(t, "agent_enrollments holds an unspent approval binding host keys")
	_, err = sp.Exec(ctx, `UPDATE agent_enrollments SET withdrawn_at = now() WHERE request_id = 'keyed'`)
	require.NoError(t, err)
	refused(t, "agent_supervisors holds a live certificate")
	_, err = sp.Exec(ctx, `UPDATE agent_supervisors SET not_after = now() - interval '1 second' WHERE grn = $1`, supervisor)
	require.NoError(t, err)

	require.NoError(t, m.Migrate(59), "the control: nothing left to strand, the migration runs")
	assert.Empty(t, tablesIn(t, sp, schema, certificatePathTables), "every table only the certificate path wrote")
	for table, columns := range certificatePathColumns {
		assert.Empty(t, columnsIn(t, sp, schema, table, columns), "the certificate columns of %s", table)
	}
	assert.Equal(t, 2, countRows(t, sp, `SELECT count(*) FROM agent_enrollments`), "every approval is kept")
	assert.Equal(t, 1, countRows(t, sp, `SELECT count(*) FROM agent_supervisors`), "and every registration")
	assert.Equal(t, sealed, authority(), "certificate_authorities is untouched")

	require.NoError(t, m.Migrate(58), "000059 down")
	assert.Equal(t, certificatePathTables, sortedTables(tablesIn(t, sp, schema, certificatePathTables)))
	for table, columns := range certificatePathColumns {
		assert.ElementsMatch(t, columns, columnsIn(t, sp, schema, table, columns), "down restores the columns of %s", table)
	}
	assert.Zero(t, countRows(t, sp, `SELECT count(*) FROM agent_credentials`), "restored empty")
	assert.Equal(t, sealed, authority())
	require.NoError(t, m.Migrate(59), "000059 up again over what down left")
}

// sortedTables answers names in the order certificatePathTables lists
// them, for a comparison that reads as the list.
func sortedTables(names []string) []string {
	held := map[string]bool{}
	for _, n := range names {
		held[n] = true
	}
	var out []string
	for _, n := range certificatePathTables {
		if held[n] {
			out = append(out, n)
		}
	}
	return out
}
