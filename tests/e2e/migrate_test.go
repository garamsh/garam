package e2e_test

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/errorx"
)

// isolatedSchema creates an empty schema of its own and returns a
// database URL that resolves to it. golang-migrate keeps
// schema_migrations in the connection's current schema, so a migration
// run through this URL — one written to fail included — reaches nothing
// the rest of this suite shares.
func isolatedSchema(t *testing.T, p *pgxpool.Pool, name string) string {
	t.Helper()
	drop := fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", name)
	_, err := p.Exec(context.Background(), drop)
	require.NoError(t, err)
	_, err = p.Exec(context.Background(), fmt.Sprintf("CREATE SCHEMA %s", name))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := p.Exec(context.Background(), drop)
		assert.NoError(t, err)
	})

	u, err := url.Parse(os.Getenv("GARAM_TEST_DATABASE_URL"))
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", name)
	u.RawQuery = q.Encode()
	return u.String()
}

// runMigrate runs `garam migrate` as the deployment runs it, against the
// migrations in dir and the database databaseURL names.
func runMigrate(t *testing.T, dir, databaseURL string) error {
	t.Helper()
	t.Setenv("GARAM_DATABASE_URL", databaseURL)
	t.Setenv("GARAM_MIGRATIONS_DIR", dir)
	root := cli.NewRootCommand()
	root.SetArgs([]string{"migrate"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SilenceUsage = true
	root.SilenceErrors = true
	return root.Execute()
}

// writeMigration puts one migration file in dir.
func writeMigration(t *testing.T, dir, name, sql string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(sql), 0o600))
}

// schemaVersion reads golang-migrate's own bookkeeping row.
func schemaVersion(t *testing.T, p *pgxpool.Pool, schema string) (int, bool) {
	t.Helper()
	var version int
	var dirty bool
	err := p.QueryRow(context.Background(),
		fmt.Sprintf("SELECT version, dirty FROM %s.schema_migrations", schema)).Scan(&version, &dirty)
	require.NoError(t, err)
	return version, dirty
}

// tableExists answers whether a migration's CREATE TABLE survived.
func tableExists(t *testing.T, p *pgxpool.Pool, schema, table string) bool {
	t.Helper()
	var exists bool
	err := p.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = $1 AND tablename = $2)`,
		schema, table).Scan(&exists)
	require.NoError(t, err)
	return exists
}

// TestMigrate_RefusesADirtyDatabaseNamingTheRecovery fails a migration
// against Postgres and then retries, which is what the deployment does
// when its PreSync hook fails. The flag is never written by hand: a test
// that set it would pass against a command that had never met a failed
// migration, and the retry is the whole subject here.
func TestMigrate_RefusesADirtyDatabaseNamingTheRecovery(t *testing.T) {
	const schema = "migrate_dirty_test"
	p := pool(t)
	databaseURL := isolatedSchema(t, p, schema)
	dir := t.TempDir()

	// The control: the same command, directory and database with nothing
	// dirty. It is the accepted case that stops being accepted below, so
	// the refusals cannot be the URL or the directory being wrong.
	writeMigration(t, dir, "000001_probe.up.sql", "CREATE TABLE probe (id integer);")
	writeMigration(t, dir, "000001_probe.down.sql", "DROP TABLE probe;")
	require.NoError(t, runMigrate(t, dir, databaseURL))

	writeMigration(t, dir, "000002_broken.up.sql",
		"CREATE TABLE half_applied (id integer);\nSELECT this_function_does_not_exist();\n")
	writeMigration(t, dir, "000002_broken.down.sql", "DROP TABLE IF EXISTS half_applied;")

	first := runMigrate(t, dir, databaseURL)
	require.Error(t, first)
	assert.Contains(t, first.Error(), "this_function_does_not_exist",
		"the run that fails names what failed")
	version, dirty := schemaVersion(t, p, schema)
	assert.Equal(t, 2, version)
	assert.True(t, dirty)
	assert.False(t, tableExists(t, p, schema, "half_applied"),
		"migration 2 is applied as one transaction, so its first statement rolls back with its second")

	retry := runMigrate(t, dir, databaseURL)
	require.Error(t, retry)
	assert.True(t, errorx.IsKind(retry, errorx.KindFailedPrecondition))
	assert.Contains(t, retry.Error(), "dirty at version 2")
	assert.Contains(t, retry.Error(), "force the schema version to 1")
	assert.Contains(t, retry.Error(), "in that run's log")
	assert.Contains(t, retry.Error(), "inside a transaction")
	assert.NotContains(t, retry.Error(), "Fix and force version",
		"golang-migrate's own message names neither the version to force nor what to check first")

	version, dirty = schemaVersion(t, p, schema)
	assert.Equal(t, 2, version)
	assert.True(t, dirty, "clearing the flag is the human step; migrate must not do it")

	// The fresh database, where the first migration is the one that
	// failed: there is no earlier version to force back to. The control
	// above covers this too — it is the same refusal, and only the
	// version the message names differs.
	t.Run("nothing precedes the failed migration", func(t *testing.T) {
		const schema = "migrate_first_dirty_test"
		databaseURL := isolatedSchema(t, p, schema)
		dir := t.TempDir()
		writeMigration(t, dir, "000001_broken.up.sql", "SELECT this_function_does_not_exist();\n")
		writeMigration(t, dir, "000001_broken.down.sql", "SELECT 1;")

		require.Error(t, runMigrate(t, dir, databaseURL))
		retry := runMigrate(t, dir, databaseURL)
		require.Error(t, retry)
		assert.Contains(t, retry.Error(), "dirty at version 1")
		assert.Contains(t, retry.Error(), "force the schema version to -1")
	})
}
