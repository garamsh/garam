package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// detailsPatch builds a patch the domain accepts, for a test that is
// not about what it refuses.
func detailsPatch(t *testing.T, name, description string) operator.DetailsPatch {
	t.Helper()
	p, err := operator.NewDetailsPatch(&name, &description)
	require.NoError(t, err)
	return p
}

// rowOf reads one text column of the row the query selects, so a test
// can hold what a write left behind against what it left before.
func rowOf(t *testing.T, p *pgxpool.Pool, query string, g grn.GRN) string {
	t.Helper()
	var out string
	require.NoError(t, p.QueryRow(context.Background(), query, g.String()).Scan(&out))
	return out
}

// TestOperatorUpdateDetails_MovesNothingTheOperatorAuthenticatesOrEnrolsBy
// is ADR-0074 against real Postgres and a real handshake: renaming and
// describing an operator changes what the organization calls it and
// nothing it runs on.
//
// The enrolled operator keeps the certificate row a renewal is measured
// against, keeps authenticating with the certificate it holds, is still
// named by its definition, and is still refused a delete for it. The
// pending operator keeps the token it was minted, which still enrols
// it after the update. Both answers carry the new details, so the
// update is shown to have happened on the rows the rest is read from.
func TestOperatorUpdateDetails_MovesNothingTheOperatorAuthenticatesOrEnrolsBy(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)
	store := operator.NewPostgresStore(p)

	enrolled, enrolledToken := register(t, store, "enrolled")
	certificate := enrol(t, url, enrolledToken)
	d, err := agent.NewDefinition(orgRootGRN("acme"), enrolled, agent.Values{"image": "ghcr.io/acme/bot:1"},
		scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	require.NoError(t, agent.NewPostgresStore(p).Define(ctx, d))
	pending, pendingToken := register(t, store, "pending")

	serial := rowOf(t, p, `SELECT serial FROM operator_certificates WHERE operator_grn = $1`, enrolled)
	before, err := store.Get(ctx, enrolled)
	require.NoError(t, err)
	tokenHash := rowOf(t, p, `SELECT token_hash FROM operator_enrollment_tokens WHERE operator_grn = $1`, pending)

	updated, err := store.UpdateDetails(ctx, enrolled, detailsPatch(t, "Field cluster", "Runs the batch agents."))
	require.NoError(t, err)
	assert.Equal(t, "Field cluster", updated.Name)
	assert.Equal(t, "Runs the batch agents.", updated.Description)
	assert.Equal(t, operator.EnrollmentStateEnrolled, updated.EnrollmentState())
	assert.Equal(t, before.CertificateIssuedAt(), updated.CertificateIssuedAt())
	assert.Equal(t, serial, rowOf(t, p, `SELECT serial FROM operator_certificates WHERE operator_grn = $1`, enrolled),
		"the lineage a renewal is measured against is where it was")
	assert.Equal(t, http.StatusOK, identifies(t, url, certificate), "the certificate it holds still authenticates")
	assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM agent_definitions WHERE operator_grn = $1`, enrolled.String()))
	assert.ErrorIs(t, store.Delete(ctx, enrolled), operator.ErrOperatorNamedByDefinition,
		"the definition still refuses the delete")

	renamed, err := store.UpdateDetails(ctx, pending, detailsPatch(t, "Staging", ""))
	require.NoError(t, err)
	assert.Equal(t, "Staging", renamed.Name)
	assert.Equal(t, operator.EnrollmentStatePending, renamed.EnrollmentState())
	assert.Equal(t, tokenHash,
		rowOf(t, p, `SELECT token_hash FROM operator_enrollment_tokens WHERE operator_grn = $1`, pending))
	enrol(t, url, pendingToken)
}

// TestOperatorUpdateDetails_ReachesOneOrganizationsRecord covers the
// tenant boundary and what a partial patch leaves. Two organizations
// register the same identifier, so the two records differ only in the
// organization their GRN names; updating one leaves the other as it
// was, and a patch naming only the description keeps the name. An
// identifier the organization never registered is a missing record,
// and the update beside it that succeeds is the control.
func TestOperatorUpdateDetails_ReachesOneOrganizationsRecord(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))
	store := operator.NewPostgresStore(p)

	mine, _ := register(t, store, "one")
	theirs, err := operator.New(orgRootGRN("globex"), "one", scopedGRN("globex", "user", "ada"), operator.DetailsPatch{})
	require.NoError(t, err)
	e, err := operator.NewEnrollment(theirs.GRN(), scopedGRN("globex", "user", "ada"))
	require.NoError(t, err)
	_, err = store.Register(ctx, theirs, e)
	require.NoError(t, err)

	_, err = store.UpdateDetails(ctx, mine, detailsPatch(t, "Acme one", "Acme's."))
	require.NoError(t, err)
	description := "Only the description."
	onlyDescription, err := operator.NewDetailsPatch(nil, &description)
	require.NoError(t, err)
	got, err := store.UpdateDetails(ctx, mine, onlyDescription)
	require.NoError(t, err)
	assert.Equal(t, "Acme one", got.Name, "a field left out keeps its value")
	assert.Equal(t, description, got.Description)
	name := "Acme renamed"
	onlyName, err := operator.NewDetailsPatch(&name, nil)
	require.NoError(t, err)
	got, err = store.UpdateDetails(ctx, mine, onlyName)
	require.NoError(t, err)
	assert.Equal(t, name, got.Name)
	assert.Equal(t, description, got.Description, "a field left out keeps its value")

	other, err := store.Get(ctx, theirs.GRN())
	require.NoError(t, err)
	assert.Equal(t, "one", other.Name, "another organization's record of the same identifier is untouched")
	assert.Empty(t, other.Description)

	_, err = store.UpdateDetails(ctx, scopedGRN("acme", operator.ServiceType, "never"), detailsPatch(t, "Ghost", ""))
	assert.ErrorIs(t, err, operator.ErrOperatorNotFound)
}

// TestOperatorDetails_TheSchemaRefusesWhatTheDomainRefuses holds
// 000025's checks as the copy no call path routes around. The writes
// go straight to the table, past internal/operator, and the write
// within both bounds is the control: it stops being accepted if the
// refusals are the statement's rather than the constraints'.
func TestOperatorDetails_TheSchemaRefusesWhatTheDomainRefuses(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	g, _ := register(t, operator.NewPostgresStore(p), "one")

	for name, update := range map[string]string{
		"a blank name":            `UPDATE operators SET name = '  ' WHERE grn = $1`,
		"a name past 100":         `UPDATE operators SET name = repeat('가', 101) WHERE grn = $1`,
		"a description past 1000": `UPDATE operators SET description = repeat('가', 1001) WHERE grn = $1`,
		"no name":                 `UPDATE operators SET name = NULL WHERE grn = $1`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := p.Exec(ctx, update, g.String())
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			assert.Contains(t, []string{"23514", "23502"}, pgErr.Code)
		})
	}

	_, err := p.Exec(ctx,
		`UPDATE operators SET name = repeat('가', 100), description = repeat('가', 1000) WHERE grn = $1`, g.String())
	assert.NoError(t, err)
}

// TestMigration25_NamesEveryExistingOperatorByItsIdentifier migrates a
// schema of its own to the version before 000025, registers an operator
// there as a registration made then was written, and applies 000025.
// The row is still there, readable, and called by its identifier with
// no description, which is what "existing registrations remain usable"
// is on the table. The second row is the control for the backfill
// reading the GRN: two identifiers answer two names.
func TestMigration25_NamesEveryExistingOperatorByItsIdentifier(t *testing.T) {
	const schema = "migration_25_test"
	p := pool(t)
	databaseURL := isolatedSchema(t, p, schema)
	dir := t.TempDir()

	entries, err := os.ReadDir(migrationsDir(t))
	require.NoError(t, err)
	var later []string
	for _, e := range entries {
		version, _, ok := strings.Cut(e.Name(), "_")
		n, err := strconv.Atoi(version)
		if !ok || err != nil || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if n >= 25 {
			later = append(later, e.Name())
			continue
		}
		copyMigration(t, dir, e.Name())
	}
	require.Contains(t, later, "000025_operator_details.up.sql")
	require.NoError(t, runMigrate(t, dir, databaseURL))
	version, _ := schemaVersion(t, p, schema)
	require.Equal(t, 24, version)

	ctx := context.Background()
	_, err = p.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.orgs (grn, name, created_at, updated_at)
		VALUES ('grn:root:default:org:acme', 'acme', now(), now())`, schema))
	require.NoError(t, err)
	_, err = p.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.operators (grn, org_grn, registered_by, created_at) VALUES
		('grn:acme:default:operator:field-one', 'grn:root:default:org:acme', 'grn:acme:default:user:ada', now()),
		('grn:acme:default:operator:field-two', 'grn:root:default:org:acme', 'grn:acme:default:user:ada', now())`, schema))
	require.NoError(t, err)

	for _, name := range later {
		copyMigration(t, dir, name)
	}
	require.NoError(t, runMigrate(t, dir, databaseURL))

	rows, err := p.Query(ctx, fmt.Sprintf(`SELECT grn, name, description FROM %s.operators ORDER BY grn`, schema))
	require.NoError(t, err)
	defer rows.Close()
	var got [][3]string
	for rows.Next() {
		var row [3]string
		require.NoError(t, rows.Scan(&row[0], &row[1], &row[2]))
		got = append(got, row)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, [][3]string{
		{"grn:acme:default:operator:field-one", "field-one", ""},
		{"grn:acme:default:operator:field-two", "field-two", ""},
	}, got)
}

// copyMigration copies one file of migrations/ into dir.
func copyMigration(t *testing.T, dir, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(migrationsDir(t), name))
	require.NoError(t, err)
	writeMigration(t, dir, name, string(body))
}
