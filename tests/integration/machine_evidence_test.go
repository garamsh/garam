package integration_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	machinepg "github.com/garamsh/garam/internal/machine/postgres"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// evidenceRow is one machine_authentication_evidence row as a test
// reads it back.
type evidenceRow struct {
	firstAt, lastAt time.Time
	issuedUntil     *time.Time
	lastRefreshAt   *time.Time
}

// evidenceOf reads caller's row for path, and whether there is one.
func evidenceOf(t *testing.T, p *pgxpool.Pool, caller grn.GRN, path machine.Path) (evidenceRow, bool) {
	t.Helper()
	var r evidenceRow
	n := countRows(t, p, `SELECT count(*) FROM machine_authentication_evidence WHERE caller_grn = $1 AND path = $2`,
		caller.String(), string(path))
	if n == 0 {
		return r, false
	}
	require.NoError(t, p.QueryRow(context.Background(), `SELECT first_at, last_at, issued_until, last_refresh_at
		FROM machine_authentication_evidence WHERE caller_grn = $1 AND path = $2`, caller.String(), string(path)).
		Scan(&r.firstAt, &r.lastAt, &r.issuedUntil, &r.lastRefreshAt))
	return r, true
}

func createOrgs(t *testing.T, p *pgxpool.Pool, names ...string) {
	t.Helper()
	for _, name := range names {
		require.NoError(t, orgpg.New(p).CreateOrg(context.Background(), org.NewOrganization(orgRootGRN(name), name)))
	}
}

// TestMachineEvidence_RecordsTheFirstAndLastSightingAndARefresh is the
// store ADR-0109 records through. A path's row keeps when it was first
// and last seen. An issuance that arrives while the token the row last
// recorded is still accepted is a refresh; one that arrives after that
// token has expired is not, which is the control that a refresh is
// read off the previous token rather than off any second issuance. A
// sighting whose organization garam does not hold writes nothing.
func TestMachineEvidence_RecordsTheFirstAndLastSightingAndARefresh(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	createOrgs(t, p, "acme")
	store := machinepg.New(p)
	refreshing := scopedGRN("acme", operator.ServiceType, "refreshing")
	lapsed := scopedGRN("acme", operator.ServiceType, "lapsed")
	certified := scopedGRN("acme", "agent", "bot-1")

	require.NoError(t, store.Record(ctx, machine.Sighting{Caller: certified, Path: machine.PathCertificate}))
	first, ok := evidenceOf(t, p, certified, machine.PathCertificate)
	require.True(t, ok)
	assert.Equal(t, first.firstAt, first.lastAt)
	assert.Nil(t, first.issuedUntil, "a listener sighting issued nothing")
	require.NoError(t, store.Record(ctx, machine.Sighting{Caller: certified, Path: machine.PathCertificate}))
	again, _ := evidenceOf(t, p, certified, machine.PathCertificate)
	assert.Equal(t, first.firstAt, again.firstAt, "the first sighting is kept")
	assert.True(t, again.lastAt.After(first.lastAt), "the last sighting moves")

	live := time.Now().Add(time.Hour)
	require.NoError(t, store.Record(ctx, machine.Sighting{Caller: refreshing, Path: machine.PathPrivateKeyJWT, IssuedUntil: live}))
	exchanged, _ := evidenceOf(t, p, refreshing, machine.PathPrivateKeyJWT)
	require.Nil(t, exchanged.lastRefreshAt, "a first exchange is no refresh")
	require.NoError(t, store.Record(ctx, machine.Sighting{Caller: refreshing, Path: machine.PathPrivateKeyJWT,
		IssuedUntil: live.Add(time.Hour)}))
	refreshed, _ := evidenceOf(t, p, refreshing, machine.PathPrivateKeyJWT)
	require.NotNil(t, refreshed.lastRefreshAt, "an exchange while the last token is accepted is a refresh")
	assert.Equal(t, refreshed.lastAt, *refreshed.lastRefreshAt)

	require.NoError(t, store.Record(ctx, machine.Sighting{Caller: lapsed, Path: machine.PathPrivateKeyJWT,
		IssuedUntil: time.Now().Add(-time.Minute)}))
	require.NoError(t, store.Record(ctx, machine.Sighting{Caller: lapsed, Path: machine.PathPrivateKeyJWT, IssuedUntil: live}))
	after, _ := evidenceOf(t, p, lapsed, machine.PathPrivateKeyJWT)
	assert.Nil(t, after.lastRefreshAt, "an exchange after the last token expired is no refresh")

	stranger := scopedGRN("ghost", operator.ServiceType, "one")
	require.NoError(t, store.Record(ctx, machine.Sighting{Caller: stranger, Path: machine.PathCertificate}))
	_, ok = evidenceOf(t, p, stranger, machine.PathCertificate)
	assert.False(t, ok, "an organization garam does not hold keeps no evidence")
}

// TestMachineEvidence_GoesWithTheTenantAndOutlivesTheCaller is the
// retention ADR-0109 states. An operator deleted while its
// organization remains keeps its rows, which are the evidence that it
// stopped; deleting the organization takes every row naming it. The
// other organization's row is the control that the deletion took the
// tenant's rows and not the table's.
func TestMachineEvidence_GoesWithTheTenantAndOutlivesTheCaller(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	createOrgs(t, p, "acme", "globex")
	store := machinepg.New(p)
	deleted := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "deleted"))
	other := registerOperator(t, p, scopedGRN("globex", operator.ServiceType, "other"))
	for _, caller := range []grn.GRN{deleted, other} {
		require.NoError(t, store.Record(ctx, machine.Sighting{Caller: caller, Path: machine.PathCertificate}))
	}

	require.NoError(t, operator.NewPostgresStore(p).Delete(ctx, deleted))
	_, ok := evidenceOf(t, p, deleted, machine.PathCertificate)
	assert.True(t, ok, "a deleted caller keeps its evidence while its organization remains")

	require.NoError(t, orgpg.New(p).DeleteOrg(ctx, orgRootGRN("acme")))
	_, ok = evidenceOf(t, p, deleted, machine.PathCertificate)
	assert.False(t, ok, "the organization's deletion takes its evidence")
	_, ok = evidenceOf(t, p, other, machine.PathCertificate)
	assert.True(t, ok, "another organization's evidence stays")
}

// inventoryRow is one row of inventory.sql.
type inventoryRow struct {
	cohort, caller  string
	registered      bool
	lastCertificate *time.Time
	lastToken       *time.Time
	lastSharedEx    *time.Time
	lastAssertionEx *time.Time
	lastMint        *time.Time
	lastAudience    *time.Time
	lastRefresh     *time.Time
}

// inventory runs internal/machine/postgres/inventory.sql as it stands,
// the read a person runs, and answers its rows by caller.
func inventory(t *testing.T, p *pgxpool.Pool) map[string]inventoryRow {
	t.Helper()
	query, err := os.ReadFile(filepath.Join("..", "..", "internal", "machine", "postgres", "inventory.sql"))
	require.NoError(t, err)
	rows, err := p.Query(context.Background(), string(query))
	require.NoError(t, err)
	defer rows.Close()
	found := map[string]inventoryRow{}
	for rows.Next() {
		var r inventoryRow
		require.NoError(t, rows.Scan(&r.cohort, &r.caller, &r.registered, &r.lastCertificate, &r.lastToken,
			&r.lastSharedEx, &r.lastAssertionEx, &r.lastMint, &r.lastAudience, &r.lastRefresh))
		found[r.caller] = r
	}
	require.NoError(t, rows.Err())
	return found
}

// TestMachineEvidence_TheInventoryNamesEveryCallerAndTheIdleOnes runs
// the shipped listener over real Postgres and reads the result through
// inventory.sql. An operator that authenticated by certificate and one
// that exchanged an assertion of its key and presented the token each
// show the paths they took. A registered operator that did neither
// shows as registered with every path empty — the idle caller G3 has to
// name — and a legacy agent with no evidence shows in its own cohort.
func TestMachineEvidence_TheInventoryNamesEveryCallerAndTheIdleOnes(t *testing.T) {
	p := pool(t)
	createOrgs(t, p, "acme")
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	issuer, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	store := operator.NewPostgresStore(p)

	certifiedGRN := scopedGRN("acme", operator.ServiceType, "certified")
	certified := machineClient(t, installed(t, p, issuer, certifiedGRN))
	status, body := machineRequest(t, certified, http.MethodGet, url+"/identity")
	require.Equal(t, http.StatusOK, status, string(body))

	exchangedGRN, key := keyed(t, store, "exchanged")
	client, _ := tokenClient(t)
	token := exchangeKey(t, client, url, exchangedGRN, key, nil)
	require.Equal(t, http.StatusOK, identifiesWith(t, client, url, token))

	idle := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "idle"))
	legacy := seedAgent(t, agent.NewPostgresStore(p), orgRootGRN("acme"))

	found := inventory(t, p)
	require.Contains(t, found, certifiedGRN.String())
	assert.Equal(t, "operator", found[certifiedGRN.String()].cohort)
	assert.NotNil(t, found[certifiedGRN.String()].lastCertificate, "a certificate request is the old path")
	assert.Nil(t, found[certifiedGRN.String()].lastToken)

	require.Contains(t, found, exchangedGRN.String())
	exchanged := found[exchangedGRN.String()]
	assert.NotNil(t, exchanged.lastAssertionEx, "the exchange is recorded under its method")
	assert.NotNil(t, exchanged.lastToken, "the token's request is recorded")
	assert.Nil(t, exchanged.lastCertificate)
	assert.Nil(t, exchanged.lastSharedEx)

	require.Contains(t, found, idle.String())
	assert.True(t, found[idle.String()].registered)
	assert.Equal(t, inventoryRow{cohort: "operator", caller: idle.String(), registered: true}, found[idle.String()],
		"a registered caller that never authenticated shows every path empty")

	require.Contains(t, found, legacy.String())
	assert.Equal(t, "legacy_agent", found[legacy.String()].cohort)
	assert.NotContains(t, found, token, "no token reaches the inventory")
}
