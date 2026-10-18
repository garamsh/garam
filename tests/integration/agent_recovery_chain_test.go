package integration_test

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/grn"
)

// forgetChain makes the recovery recorded under requestID read as one
// recorded before 000036, which stored no chain. The check refuses that
// write, so it is dropped and added back NOT VALID inside the
// transaction, as the migration that last defined it adds it: the
// definition is read first, so the one 000040 replaced 000036's with is
// what stands afterwards.
func forgetChain(t *testing.T, p *pgxpool.Pool, agentGRN, requestID string) {
	t.Helper()
	ctx := context.Background()
	tx, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	var definition string
	require.NoError(t, tx.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conname = 'agent_recoveries_chain'`).Scan(&definition))
	_, err = tx.Exec(ctx, `ALTER TABLE agent_recoveries DROP CONSTRAINT agent_recoveries_chain`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE agent_recoveries SET issuer_pem = NULL, server_root_pem = NULL
		WHERE agent_grn = $1 AND request_id = $2`, agentGRN, requestID)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `ALTER TABLE agent_recoveries ADD CONSTRAINT agent_recoveries_chain `+definition)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
}

// requireChainRefused asserts err is 000036's check refusing a row.
func requireChainRefused(t *testing.T, err error) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	assert.Equal(t, "23514", pgErr.Code, "a check violation")
	assert.Equal(t, "agent_recoveries_chain", pgErr.ConstraintName)
}

// TestMigration000036_KeepsEveryRecoveryAndRefusesOneWithoutItsChain
// migrates a schema of its own to 000035, records a recovery there and
// applies 000036. The recovery reads as it was, with no chain, and the
// store answers it as one recorded before 000036; a new recovery
// without its chain is refused, and the same row carrying one is
// accepted, which is the control that the refusal is the chain's.
// 000036 down drops the two columns and keeps the row, and applies
// again over it.
func TestMigration000036_KeepsEveryRecoveryAndRefusesOneWithoutItsChain(t *testing.T) {
	ctx := context.Background()
	databaseURL := isolatedSchema(t, pool(t), "recovery_chain_000036")
	m, err := migrate.New("file://"+migrationsDir(t), databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(35))
	sp := connect(t, databaseURL, nil)
	const (
		orgGRN   = "grn:root:default:org:rcv"
		agentGRN = "grn:rcv:default:agent:9f2ac1b40d8e7a35"
	)
	for _, statement := range []string{
		`INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ('` + orgGRN + `', 'rcv', now(), now())`,
		`INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
			VALUES ('` + agentGRN + `', '` + orgGRN + `', 'pending', 'unknown', now(), now())`,
		`INSERT INTO agent_executions (agent_grn, updated_at) VALUES ('` + agentGRN + `', now())`,
		`INSERT INTO agent_recoveries
			(agent_grn, request_id, operation_ref, body_sha256, lineage, certificate_pem, created_at)
			VALUES ('` + agentGRN + `', 'rec-1', 'ref-1', sha256('a'::bytea), gen_random_uuid(), 'kept', now())`,
	} {
		_, err := sp.Exec(ctx, statement)
		require.NoError(t, err)
	}
	snapshot := func() string {
		var row string
		require.NoError(t, sp.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(r) - 'issuer_pem' - 'server_root_pem')::text
			FROM agent_recoveries r`).Scan(&row))
		return row
	}
	before := snapshot()

	require.NoError(t, m.Migrate(36))
	assert.Equal(t, before, snapshot(), "no recovery changed")
	assert.Equal(t, 1, countRows(t, sp, `SELECT count(*) FROM agent_recoveries
		WHERE issuer_pem IS NULL AND server_root_pem IS NULL`), "nothing is backfilled")
	subject, err := grn.Parse(agentGRN)
	require.NoError(t, err)
	kept, err := agentpg.New(sp).GetRecovery(ctx, subject, "rec-1")
	require.NoError(t, err)
	require.NotNil(t, kept)
	assert.Equal(t, "kept", kept.CertificatePEM)
	assert.Nil(t, kept.Chain, "a recovery recorded before 000036 reads with no chain")

	insert := func(issuer, serverRoot any) error {
		_, err := sp.Exec(ctx, `INSERT INTO agent_recoveries
			(agent_grn, request_id, operation_ref, body_sha256, lineage, certificate_pem,
			 issuer_pem, server_root_pem, created_at)
			VALUES ($1, 'rec-2', 'ref-2', sha256('b'::bytea), gen_random_uuid(), 'new', $2, $3, now())`,
			agentGRN, issuer, serverRoot)
		return err
	}
	requireChainRefused(t, insert(nil, nil))
	requireChainRefused(t, insert("issuer", nil))
	requireChainRefused(t, insert(nil, "server root"))
	require.NoError(t, insert("issuer", "server root"), "control: the same row carrying its chain is accepted")

	require.NoError(t, m.Migrate(35), "000036 down")
	assert.Equal(t, 2, countRows(t, sp, `SELECT count(*) FROM agent_recoveries`), "down keeps every recovery")
	assert.Zero(t, countRows(t, sp, `SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'recovery_chain_000036' AND table_name = 'agent_recoveries'
		  AND column_name IN ('issuer_pem', 'server_root_pem')`), "down drops the two columns")
	require.NoError(t, m.Migrate(36), "000036 up again over the kept rows")
	assert.Equal(t, 2, countRows(t, sp, `SELECT count(*) FROM agent_recoveries WHERE issuer_pem IS NULL`))
}
