package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
)

// managerLockStack is an organization holding the operators a test of
// ADR-0124's lock order registers, and the pool a parked transaction
// stands in for the other side on.
type managerLockStack struct {
	p     *pgxpool.Pool
	store *operatorpg.Store
}

func newManagerLockStack(t *testing.T, ids ...string) managerLockStack {
	t.Helper()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(context.Background(), org.NewOrganization(orgRootGRN("acme"), "acme")))
	s := managerLockStack{p: p, store: operatorpg.New(p)}
	for _, id := range ids {
		register(t, s.store, id)
	}
	return s
}

func managedOperator(id string) grn.GRN {
	return scopedGRN("acme", operator.ServiceType, id)
}

// park begins a transaction that runs statements as the other side of
// the interleaving would, and answers it still open.
func (s managerLockStack) park(t *testing.T, statements ...string) pgx.Tx {
	t.Helper()
	tx, err := s.p.Begin(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	for _, statement := range statements {
		_, err = tx.Exec(context.Background(), statement)
		require.NoError(t, err)
	}
	return tx
}

// TestOperatorManagerLock_SettingAManagerWaitsForARegistrationNamingTheRecord
// holds the order a link relies on: a registration naming aurora as
// its manager holds aurora FOR KEY SHARE and commits a control service
// naming it, and SetManager holding aurora FOR UPDATE waits for that
// commit, so it reads aurora as a manager and refuses rather than
// making the new control service's manager a control service itself.
func TestOperatorManagerLock_SettingAManagerWaitsForARegistrationNamingTheRecord(t *testing.T) {
	ctx := context.Background()
	s := newManagerLockStack(t, "aurora", "borealis")
	registration := s.park(t,
		`INSERT INTO operators (grn, org_grn, registered_by, created_at, name, description, manager_grn)
		 VALUES ('`+managedOperator("aurora-control").String()+`', '`+orgRootGRN("acme").String()+`',
		         '`+scopedGRN("acme", "user", "ada").String()+`', now(), 'aurora-control', '',
		         '`+managedOperator("aurora").String()+`')`,
		`SELECT manager_grn FROM operators WHERE grn = '`+managedOperator("aurora").String()+`' FOR KEY SHARE`,
	)

	linked := make(chan error, 1)
	go func() {
		_, err := s.store.SetManager(ctx, managedOperator("aurora"), managedOperator("borealis"))
		linked <- err
	}()
	awaitSettled(t, s.p, 1, returned(linked))
	require.Empty(t, linked, "the link waits for the registration holding aurora")
	require.NoError(t, registration.Commit(ctx))

	assert.ErrorIs(t, receive(t, linked), operator.ErrIdentityIsManager)
	assert.Nil(t, stateOf(t, s.store, managedOperator("aurora")).Manager, "aurora stays a manager")
}

// TestOperatorManagerLock_ARegistrationWaitsForALinkOnItsManager holds
// the other side: SetManager making aurora borealis's control service
// holds aurora FOR UPDATE until it commits, and a registration naming
// aurora as its manager waits on its FOR KEY SHARE for that commit, so
// it reads aurora as a control service and refuses the chain.
func TestOperatorManagerLock_ARegistrationWaitsForALinkOnItsManager(t *testing.T) {
	ctx := context.Background()
	s := newManagerLockStack(t, "aurora", "borealis")
	link := s.park(t,
		`SELECT manager_grn FROM operators WHERE grn = '`+managedOperator("aurora").String()+`' FOR UPDATE`,
		`UPDATE operators SET manager_grn = '`+managedOperator("borealis").String()+`'
		 WHERE grn = '`+managedOperator("aurora").String()+`'`,
	)

	registered := make(chan error, 1)
	go func() {
		o, err := operator.New(orgRootGRN("acme"), "aurora-control", scopedGRN("acme", "user", "ada"), operator.DetailsPatch{})
		if err == nil {
			err = o.ServeManager(managedOperator("aurora").String())
		}
		if err == nil {
			_, err = s.store.Register(ctx, o)
		}
		registered <- err
	}()
	awaitSettled(t, s.p, 1, returned(registered))
	require.Empty(t, registered, "the registration waits for the link holding aurora")
	require.NoError(t, link.Commit(ctx))

	assert.ErrorIs(t, receive(t, registered), operator.ErrManagerIsControlService)
	listed, err := s.store.ListByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	assert.Len(t, listed, 2, "the control service was not recorded")
}
