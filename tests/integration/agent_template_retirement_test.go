package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	retiredOrg      = "grn:root:default:org:ret"
	retiredTemplate = "grn:ret:default:agent-template:7c1e2a9b3d4f5a60"
)

// templateWrites are the three writes a release serving the template
// routes made, each against the template retiredSchema keeps.
var templateWrites = map[string]string{
	"an insert": `INSERT INTO agent_templates
		(grn, org_grn, name, description, agent_values, created_by, created_at, updated_at)
		VALUES ('grn:ret:default:agent-template:00000000000000ff', '` + retiredOrg + `', 'New', '', '{}',
		        'grn:ret:default:user:ada', now(), now())`,
	"an update": `UPDATE agent_templates SET name = 'Edited' WHERE grn = '` + retiredTemplate + `'`,
	"a delete":  `DELETE FROM agent_templates WHERE grn = '` + retiredTemplate + `'`,
}

// retiredSchema migrates a schema of its own to 000033, the version
// before the retirement, and keeps one template in it. It answers a
// pool whose connections work in that schema, and the migration.
func retiredSchema(t *testing.T, name string) (*pgxpool.Pool, *migrate.Migrate) {
	t.Helper()
	ctx := context.Background()
	databaseURL := isolatedSchema(t, pool(t), name)
	m, err := migrate.New("file://"+migrationsDir(t), databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(33))
	sp, err := pgxpool.New(ctx, databaseURL)
	require.NoError(t, err)
	t.Cleanup(sp.Close)
	for _, statement := range []string{
		`INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('` + retiredOrg + `', 'ret', now(), now())`,
		`INSERT INTO agent_templates
			(grn, org_grn, name, description, agent_values, created_by, created_at, updated_at)
			VALUES ('` + retiredTemplate + `', '` + retiredOrg + `', 'Researcher', 'Reads papers',
			        '{"workspace":"papers"}', 'grn:ret:default:user:ada', now(), now())`,
	} {
		_, err := sp.Exec(ctx, statement)
		require.NoError(t, err)
	}
	return sp, m
}

// templateState answers the template names retiredSchema's schema
// holds, ordered by GRN: what a write changed, or that it changed
// nothing.
func templateState(t *testing.T, sp *pgxpool.Pool) []string {
	t.Helper()
	rows, err := sp.Query(context.Background(), `SELECT name FROM agent_templates ORDER BY grn`)
	require.NoError(t, err)
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		require.NoError(t, rows.Scan(&n))
		names = append(names, n)
	}
	require.NoError(t, rows.Err())
	return names
}

// backendPID answers the pid of the backend tx runs on.
func backendPID(t *testing.T, tx pgx.Tx) int {
	t.Helper()
	var pid int
	require.NoError(t, tx.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid))
	return pid
}

// waitBlockedBy waits until another backend is waiting for a lock the
// backend blocker holds, which is the state each race below depends on.
func waitBlockedBy(t *testing.T, sp *pgxpool.Pool, blocker int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting bool
		err := sp.QueryRow(context.Background(),
			`SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY (pg_blocking_pids(pid)))`,
			blocker).Scan(&waiting)
		return err == nil && waiting
	}, 10*time.Second, 10*time.Millisecond, "nothing waited for backend %d", blocker)
}

// TestMigration000034_AWriteAlreadyHoldingTheTableCommitsFirst is the
// write in flight when the retirement lands (ADR-0088). Each write is
// made and left open, and 000034 is applied beside it: creating the
// guard waits for the table, so the write commits and the migration
// then completes. The write's own effect is what is asserted, and the
// same write afterwards is refused, which shows the guard was not
// already in place when it committed.
func TestMigration000034_AWriteAlreadyHoldingTheTableCommitsFirst(t *testing.T) {
	want := map[string][]string{
		"an insert": {"New", "Researcher"},
		"an update": {"Edited"},
		"a delete":  {},
	}
	for i, name := range []string{"an insert", "an update", "a delete"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			sp, m := retiredSchema(t, "retire_000034_first_"+string(rune('a'+i)))
			write := templateWrites[name]
			tx, err := sp.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, write)
			require.NoError(t, err)
			writer := backendPID(t, tx)

			migrated := make(chan error, 1)
			go func() { migrated <- m.Migrate(34) }()
			waitBlockedBy(t, sp, writer)
			require.NoError(t, tx.Commit(ctx))
			require.NoError(t, <-migrated)

			assert.Equal(t, want[name], templateState(t, sp), "the write in flight committed")
			_, err = sp.Exec(ctx, `UPDATE agent_templates SET name = 'Later'`)
			if name == "a delete" {
				require.NoError(t, err, "nothing is left to update")
				return
			}
			requireRetired(t, err)
		})
	}
}

// TestMigration000034_AWriteReachingTheTableAfterTheRetirementIsRefused
// is the write that arrives while the retirement holds the table. Each
// write is sent after 000034's statements have run in an open
// transaction, waits for the table, and is refused once that
// transaction commits, changing nothing. The control is the same write
// when the transaction rolls back instead: it is accepted, so what
// refuses it is the guard the commit installed and not the wait.
func TestMigration000034_AWriteReachingTheTableAfterTheRetirementIsRefused(t *testing.T) {
	up, err := os.ReadFile(filepath.Join(migrationsDir(t), "000034_agent_templates_retired.up.sql"))
	require.NoError(t, err)
	n := 0
	for _, name := range []string{"an insert", "an update", "a delete"} {
		for _, retire := range []bool{true, false} {
			n++
			label := name + map[bool]string{true: ", retirement commits", false: ", retirement rolls back (control)"}[retire]
			t.Run(label, func(t *testing.T) {
				ctx := context.Background()
				sp, _ := retiredSchema(t, "retire_000034_after_"+string(rune('a'+n)))
				tx, err := sp.Begin(ctx)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback(ctx) }()
				_, err = tx.Exec(ctx, string(up))
				require.NoError(t, err)
				retirement := backendPID(t, tx)

				write := templateWrites[name]
				written := make(chan error, 1)
				go func() {
					_, err := sp.Exec(ctx, write)
					written <- err
				}()
				waitBlockedBy(t, sp, retirement)
				if retire {
					require.NoError(t, tx.Commit(ctx))
					requireRetired(t, <-written)
					assert.Equal(t, []string{"Researcher"}, templateState(t, sp), "the refused write changed nothing")
					return
				}
				require.NoError(t, tx.Rollback(ctx))
				assert.NoError(t, <-written, "control: with no retirement the write is accepted")
			})
		}
	}
}

// TestMigration000034_KeepsEveryRowAndRollsBack applies 000034 over a
// template and a definition copied from it, as 000033 left them. Both
// rows read back byte for byte: the retirement changes no template
// and no definition. Rolling 000034 back reopens the write it refused,
// which is the control that the refusal is 000034's, and applying it
// again closes it again.
func TestMigration000034_KeepsEveryRowAndRollsBack(t *testing.T) {
	ctx := context.Background()
	sp, m := retiredSchema(t, "retire_000034_rows")
	for _, statement := range []string{
		`INSERT INTO operators (grn, org_grn, registered_by, created_at, name)
			VALUES ('grn:ret:default:operator:one', '` + retiredOrg + `', 'grn:ret:default:user:ada', now(), 'one')`,
		`INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, agent_values, defined_by, created_at, name)
			VALUES ('grn:ret:default:agent:9f2ac1b40d8e7a35', '` + retiredOrg + `', 'grn:ret:default:operator:one',
			        '{"workspace":"papers"}', 'grn:ret:default:user:ada', now(), 'Scout')`,
	} {
		_, err := sp.Exec(ctx, statement)
		require.NoError(t, err)
	}
	snapshot := func() []string {
		var rows []string
		for _, table := range []string{"agent_templates", "agent_definitions"} {
			var row string
			require.NoError(t, sp.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(r))::text FROM `+table+` r`).Scan(&row))
			rows = append(rows, row)
		}
		return rows
	}
	before := snapshot()

	require.NoError(t, m.Migrate(34))
	assert.Equal(t, before, snapshot(), "no template and no definition changed")
	_, err := sp.Exec(ctx, templateWrites["an update"])
	requireRetired(t, err)

	require.NoError(t, m.Migrate(33), "000034 down")
	_, err = sp.Exec(ctx, templateWrites["an update"])
	require.NoError(t, err, "control: below 000034 the write is accepted")
	require.NoError(t, m.Migrate(34), "000034 up again")
	_, err = sp.Exec(ctx, templateWrites["a delete"])
	requireRetired(t, err)
}
