package integration_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// currentAuthority stands in for the composition's Authorizer: it
// answers what the test says current state is now, so a decision
// recorded earlier cannot be what a later call reads. The real
// adapter, over membership and CheckCurrent, is composed in
// internal/cli.
type currentAuthority struct {
	mu  sync.Mutex
	err error
}

func (a *currentAuthority) set(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.err = err
}

func (a *currentAuthority) Authorize(context.Context, grn.GRN, grn.GRN, grn.GRN, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.err
}

// placements answers where each agent runs now.
type placements struct {
	mu sync.Mutex
	at map[grn.GRN]operator.Assignment
}

func (p *placements) move(agent grn.GRN, a operator.Assignment) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.at[agent] = a
}

func (p *placements) CurrentAssignment(_ context.Context, agent grn.GRN) (operator.Assignment, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	a, ok := p.at[agent]
	if !ok {
		return operator.Assignment{}, errorx.New(errorx.KindNotFound, "agent not found")
	}
	return a, nil
}

// clock is a time the test moves.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type delegationFixture struct {
	pool       *pgxpool.Pool
	store      *operatorpg.Store
	service    operator.AuthorityService
	authority  *currentAuthority
	placements *placements
	clock      *clock
	org        grn.GRN
	actor      grn.GRN
	hosted     grn.GRN
	controller grn.GRN
	other      grn.GRN
	agent      grn.GRN
}

// newDelegationFixture registers a hosted operator and two controllers
// in acme, and a member to act for.
func newDelegationFixture(t *testing.T) *delegationFixture {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	actor := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "ada")
	require.NoError(t, userpg.New(p).Create(ctx, user.New(actor, "Ada")))
	store := operatorpg.New(p)
	hosted := register(t, store, "hosted")
	controller := register(t, store, "controller")
	other := register(t, store, "other")
	f := &delegationFixture{
		pool: p, store: store, authority: &currentAuthority{},
		placements: &placements{at: map[grn.GRN]operator.Assignment{}},
		clock:      &clock{now: time.Now().UTC()},
		org:        orgRootGRN("acme"), actor: actor, hosted: hosted, controller: controller, other: other,
		agent: scopedGRN("acme", "agent", "0a1b2c3d4e5f6071"),
	}
	f.placements.move(f.agent, operator.Assignment{Assignee: controller, Epoch: "1"})
	delegations := operatorpg.New(p)
	f.service = operator.NewAuthorityService(delegations, delegations, store, f.placements, f.authority,
		operator.Config{Now: f.clock.Now})
	return f
}

func (f *delegationFixture) terms(t *testing.T, controllers []grn.GRN, ops ...operator.Operation) operator.GrantTerms {
	t.Helper()
	cs := make([]string, len(controllers))
	for i, c := range controllers {
		cs[i] = c.String()
	}
	os := make([]string, len(ops))
	for i, op := range ops {
		os[i] = string(op)
	}
	terms, err := operator.ParseGrantTerms(f.org, f.hosted, cs, os, f.clock.Now().Add(24*time.Hour), f.clock.Now())
	require.NoError(t, err)
	return terms
}

func (f *delegationFixture) write(requestID string, expected *operator.GrantID, terms operator.GrantTerms) (*operator.Grant, bool, error) {
	return f.service.WriteGrant(context.Background(), operator.GrantWrite{
		Org: f.org, Operator: f.hosted, RequestID: operator.RequestID(requestID),
		Expected: expected, Terms: terms, Actor: f.actor,
	})
}

// grant writes a first grant covering both mintable operations on the
// controller, and fails the test where it is refused.
func (f *delegationFixture) grant(t *testing.T) *operator.Grant {
	t.Helper()
	g, created, err := f.write("grant-1", nil, f.terms(t, []grn.GRN{f.controller},
		operator.OperationAgentCreate, operator.OperationAgentConfigure))
	require.NoError(t, err)
	require.True(t, created)
	return g
}

func (f *delegationFixture) mint(op operator.Operation, target grn.GRN, requestID string) (*operator.Minted, error) {
	return f.service.Mint(context.Background(), operator.MintRequest{
		Actor: f.actor, Org: f.org, Audience: f.hosted, Operation: op, Target: target,
		RequestID: operator.RequestID(requestID), Digest: operator.BodyDigest(strings.Repeat("a", 64)),
	})
}

func grantRef(id operator.GrantID) *operator.GrantID { return &id }

// TestGrantWrite_ReplayAfterWithdrawalIsRefused is the lost-answer
// case: a write succeeds, its answer is lost, the grant is withdrawn,
// and the retry arrives. It is refused rather than writing the grant
// again. The repeat before the withdrawal, answered with the same
// grant, is the control.
func TestGrantWrite_ReplayAfterWithdrawalIsRefused(t *testing.T) {
	f := newDelegationFixture(t)
	first := f.grant(t)

	replayed, created, err := f.write("grant-1", nil, f.terms(t, []grn.GRN{f.controller},
		operator.OperationAgentCreate, operator.OperationAgentConfigure))
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, first.ID, replayed.ID)

	require.NoError(t, f.service.EndGrant(context.Background(), f.actor, f.org, f.hosted, first.ID))
	_, _, err = f.write("grant-1", nil, f.terms(t, []grn.GRN{f.controller},
		operator.OperationAgentCreate, operator.OperationAgentConfigure))
	require.ErrorIs(t, err, operator.ErrGrantEnded)
	_, err = f.service.CurrentGrant(context.Background(), f.org, f.hosted)
	require.ErrorIs(t, err, operator.ErrGrantNotFound, "the replay recreated nothing")
}

// TestGrantEnd_RetriedWithdrawalNeverEndsAReplacement is the lost
// answer to a withdrawal: G1 is ended, its answer is lost, another
// administrator grants G2, and the withdrawal of G1 is retried. The
// retry succeeds and changes nothing; G2 stays current. Ending G2 by
// name is the control, and a grant the operator never held is refused.
func TestGrantEnd_RetriedWithdrawalNeverEndsAReplacement(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	first := f.grant(t)
	require.NoError(t, f.service.EndGrant(ctx, f.actor, f.org, f.hosted, first.ID))
	second, created, err := f.write("grant-2", nil, f.terms(t, []grn.GRN{f.other}, operator.OperationAgentCreate))
	require.NoError(t, err)
	require.True(t, created)

	require.NoError(t, f.service.EndGrant(ctx, f.actor, f.org, f.hosted, first.ID))
	current, err := f.service.CurrentGrant(ctx, f.org, f.hosted)
	require.NoError(t, err)
	assert.Equal(t, second.ID, current.ID)

	err = f.service.EndGrant(ctx, f.actor, f.org, f.hosted, "never-granted")
	require.ErrorIs(t, err, operator.ErrGrantNotFound)
	err = f.service.EndGrant(ctx, f.actor, f.org, f.controller, second.ID)
	require.ErrorIs(t, err, operator.ErrGrantNotFound, "another operator's grant")

	require.NoError(t, f.service.EndGrant(ctx, f.actor, f.org, f.hosted, second.ID))
	_, err = f.service.CurrentGrant(ctx, f.org, f.hosted)
	require.ErrorIs(t, err, operator.ErrGrantNotFound)
}

// TestGrantWrite_ReplayBindsTheWholeWrite holds that a request
// identifier replays only the write that used it: the same terms under
// another expected grant, or by another actor, conflict. The identical
// retry is the control.
func TestGrantWrite_ReplayBindsTheWholeWrite(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	first := f.grant(t)
	terms := f.terms(t, []grn.GRN{f.other}, operator.OperationAgentCreate)
	second, _, err := f.write("grant-2", grantRef(first.ID), terms)
	require.NoError(t, err)

	_, _, err = f.write("grant-2", grantRef(second.ID), terms)
	require.ErrorIs(t, err, operator.ErrGrantRequestConflict, "another expected grant")
	_, _, err = f.service.WriteGrant(ctx, operator.GrantWrite{
		Org: f.org, Operator: f.hosted, RequestID: "grant-2", Expected: grantRef(first.ID),
		Terms: terms, Actor: grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "grace"),
	})
	require.ErrorIs(t, err, operator.ErrGrantRequestConflict, "another actor")

	replayed, created, err := f.write("grant-2", grantRef(first.ID), terms)
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, second.ID, replayed.ID)
}

// TestGrantWrite_ReplayMatchesAtStoredPrecision holds that an expiry
// carrying more precision than Postgres stores answers, on the write
// and on its replay, the value that was stored, and that the replay is
// recognised. A microsecond later is a different write and conflicts.
func TestGrantWrite_ReplayMatchesAtStoredPrecision(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	expiresAt := time.Date(2026, time.October, 5, 1, 2, 3, 123456789, time.UTC)
	f.clock.now = expiresAt.Add(-time.Hour)
	termsAt := func(e time.Time) operator.GrantTerms {
		terms, err := operator.ParseGrantTerms(f.org, f.hosted, []string{f.controller.String()},
			[]string{string(operator.OperationAgentCreate)}, e, f.clock.Now())
		require.NoError(t, err)
		return terms
	}

	written, created, err := f.write("grant-1", nil, termsAt(expiresAt))
	require.NoError(t, err)
	require.True(t, created)
	stored, err := f.service.CurrentGrant(ctx, f.org, f.hosted)
	require.NoError(t, err)
	assert.Equal(t, stored.ExpiresAt, written.ExpiresAt, "the write answers what was stored")
	assert.Equal(t, stored.GrantedAt, written.GrantedAt)

	replayed, created, err := f.write("grant-1", nil, termsAt(expiresAt))
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, written.ID, replayed.ID)

	_, _, err = f.write("grant-1", nil, termsAt(expiresAt.Add(time.Microsecond)))
	require.ErrorIs(t, err, operator.ErrGrantRequestConflict)
}

// TestGrantWrite_RefusesAStaleViewAndAReusedRequest holds the
// compare-and-set: a write must name the grant that is current, and a
// request identifier answers only the terms it first carried.
func TestGrantWrite_RefusesAStaleViewAndAReusedRequest(t *testing.T) {
	f := newDelegationFixture(t)
	first := f.grant(t)
	wider := f.terms(t, []grn.GRN{f.controller, f.other}, operator.OperationAgentCreate)

	_, _, err := f.write("grant-2", nil, wider)
	require.ErrorIs(t, err, operator.ErrGrantStale, "a view that saw no grant")

	second, created, err := f.write("grant-2", grantRef(first.ID), wider)
	require.NoError(t, err)
	require.True(t, created)
	assert.NotEqual(t, first.ID, second.ID, "a replacement is a grant of its own")

	_, _, err = f.write("grant-3", grantRef(first.ID), wider)
	require.ErrorIs(t, err, operator.ErrGrantStale, "a view of the replaced grant")

	_, _, err = f.write("grant-2", grantRef(first.ID), f.terms(t, []grn.GRN{f.other}, operator.OperationAgentCreate))
	require.ErrorIs(t, err, operator.ErrGrantRequestConflict)
}

// TestGrantWrite_ConcurrentReplacementsLeaveOneWinner races writers
// holding one view. Exactly one replaces the grant; every other is
// stale, and one grant is current afterwards.
func TestGrantWrite_ConcurrentReplacementsLeaveOneWinner(t *testing.T) {
	f := newDelegationFixture(t)
	first := f.grant(t)
	terms := f.terms(t, []grn.GRN{f.other}, operator.OperationAgentCreate)

	const writers = 8
	results := make(chan error, writers)
	var start sync.WaitGroup
	start.Add(1)
	for i := range writers {
		go func() {
			start.Wait()
			_, _, err := f.write("race-"+string(rune('a'+i)), grantRef(first.ID), terms)
			results <- err
		}()
	}
	start.Done()
	won := 0
	for range writers {
		err := <-results
		if err == nil {
			won++
			continue
		}
		require.ErrorIs(t, err, operator.ErrGrantStale)
	}
	assert.Equal(t, 1, won)
	assert.Equal(t, 1, countRows(t, f.pool,
		`SELECT count(*) FROM operator_delegations WHERE operator_grn = $1 AND ended_at IS NULL`, f.hosted.String()))
}

// TestMint_RemintKeepsTheReferenceAndReplacesTheToken holds the retry
// after a lost or expired handoff: the same binding under the same
// grant answers the same reference with a new token, and the token it
// replaced introspects as unknown.
func TestMint_RemintKeepsTheReferenceAndReplacesTheToken(t *testing.T) {
	f := newDelegationFixture(t)
	f.grant(t)
	first, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)
	second, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)
	assert.Equal(t, first.Ref, second.Ref)
	assert.NotEqual(t, first.Authority, second.Authority)

	_, err = f.service.Introspect(context.Background(), f.hosted, first.Authority)
	require.ErrorIs(t, err, operator.ErrAuthorityNotFound)
	got, err := f.service.Introspect(context.Background(), f.hosted, second.Authority)
	require.NoError(t, err)
	assert.Equal(t, second.Ref, got.Ref)
	assert.Equal(t, f.actor, got.Actor)
	assert.Equal(t, f.controller, got.Target)

	_, err = f.service.Introspect(context.Background(), f.hosted, second.Authority)
	require.NoError(t, err, "introspection consumes nothing")
}

// TestMint_ConcurrentRemintsLeaveOneReference races mints of one
// request identifier: they answer one reference between them.
func TestMint_ConcurrentRemintsLeaveOneReference(t *testing.T) {
	f := newDelegationFixture(t)
	f.grant(t)
	const minters = 8
	refs := make(chan operator.OperationRef, minters)
	var wg sync.WaitGroup
	for range minters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
			if assert.NoError(t, err) {
				refs <- m.Ref
			}
		}()
	}
	wg.Wait()
	close(refs)
	seen := map[operator.OperationRef]bool{}
	for r := range refs {
		seen[r] = true
	}
	assert.Len(t, seen, 1)
	assert.Equal(t, 1, countRows(t, f.pool, `SELECT count(*) FROM operation_authorities`))
}

// TestMint_ChangedDigestUnderARequestConflicts holds that a request
// identifier is bound to the body it first carried.
func TestMint_ChangedDigestUnderARequestConflicts(t *testing.T) {
	f := newDelegationFixture(t)
	f.grant(t)
	_, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)
	_, err = f.service.Mint(context.Background(), operator.MintRequest{
		Actor: f.actor, Org: f.org, Audience: f.hosted, Operation: operator.OperationAgentCreate,
		Target: f.controller, RequestID: "req-1", Digest: operator.BodyDigest(strings.Repeat("b", 64)),
	})
	require.ErrorIs(t, err, operator.ErrAuthorityRequestConflict)
}

// TestReference_ReplacedOrWithdrawnGrantIsFinal holds that an operation
// authorized under one grant is never authorized under another: after a
// widening replacement, a withdrawal or a re-grant, its reference is
// refused and its request identifier cannot be reminted. A new request
// identifier under the new grant is the control.
func TestReference_ReplacedOrWithdrawnGrantIsFinal(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	first := f.grant(t)
	minted, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)
	_, err = f.service.Reference(ctx, f.hosted, minted.Ref)
	require.NoError(t, err)

	wider := f.terms(t, []grn.GRN{f.controller, f.other}, operator.OperationAgentCreate, operator.OperationAgentConfigure)
	second, _, err := f.write("grant-2", grantRef(first.ID), wider)
	require.NoError(t, err)
	_, err = f.service.Reference(ctx, f.hosted, minted.Ref)
	require.ErrorIs(t, err, operator.ErrAuthorityRevoked, "widened")
	_, err = f.service.Introspect(ctx, f.hosted, minted.Authority)
	require.ErrorIs(t, err, operator.ErrAuthorityRevoked)
	_, err = f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.ErrorIs(t, err, operator.ErrAuthorityRequestConflict)
	fresh, err := f.mint(operator.OperationAgentCreate, f.controller, "req-2")
	require.NoError(t, err)
	assert.Equal(t, second.ID, fresh.Grant)

	require.NoError(t, f.service.EndGrant(ctx, f.actor, f.org, f.hosted, second.ID))
	_, err = f.service.Reference(ctx, f.hosted, fresh.Ref)
	require.ErrorIs(t, err, operator.ErrAuthorityRevoked, "withdrawn")
	_, _, err = f.write("grant-3", nil, wider)
	require.NoError(t, err)
	_, err = f.service.Reference(ctx, f.hosted, fresh.Ref)
	require.ErrorIs(t, err, operator.ErrAuthorityRevoked, "re-granted")
}

// TestReference_OutlivesTheShortToken holds that token expiry ends the
// handoff and not the operation: the token introspects as unknown once
// it runs out, while its reference still rechecks.
func TestReference_OutlivesTheShortToken(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	f.grant(t)
	minted, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)
	assert.True(t, minted.ExpiresAt.Equal(f.clock.Now().Truncate(time.Microsecond).Add(operator.AuthorityTTL)))

	f.clock.advance(operator.AuthorityTTL)
	_, err = f.service.Introspect(ctx, f.hosted, minted.Authority)
	require.ErrorIs(t, err, operator.ErrAuthorityNotFound)
	_, err = f.service.Reference(ctx, f.hosted, minted.Ref)
	require.NoError(t, err)
}

// TestMint_TokenNeverOutlivesItsGrant holds that a handoff minted near
// the grant's expiry expires with the grant, not five minutes later.
func TestMint_TokenNeverOutlivesItsGrant(t *testing.T) {
	f := newDelegationFixture(t)
	terms, err := operator.ParseGrantTerms(f.org, f.hosted, []string{f.controller.String()},
		[]string{string(operator.OperationAgentCreate)}, f.clock.Now().Add(2*time.Minute), f.clock.Now())
	require.NoError(t, err)
	g, _, err := f.write("grant-1", nil, terms)
	require.NoError(t, err)
	minted, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)
	assert.True(t, minted.ExpiresAt.Equal(g.ExpiresAt), "%v != %v", minted.ExpiresAt, g.ExpiresAt)
}

// TestIntrospect_AnswersItsAudienceOnly holds that another operator of
// the same organization, holding a valid authority, learns nothing
// about it: the same answer as an unknown one.
func TestIntrospect_AnswersItsAudienceOnly(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	f.grant(t)
	minted, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)

	_, err = f.service.Introspect(ctx, f.controller, minted.Authority)
	require.ErrorIs(t, err, operator.ErrAuthorityNotFound)
	_, err = f.service.Reference(ctx, f.controller, minted.Ref)
	require.ErrorIs(t, err, operator.ErrAuthorityNotFound)
	_, err = f.service.Introspect(ctx, f.hosted, minted.Authority)
	require.NoError(t, err)
}

// TestMint_RefusesWhatTheGrantDoesNotCover holds the delegation's
// bound: a controller outside it, an operation outside it, a cross
// organization audience, and agent:activate, which no member mints.
func TestMint_RefusesWhatTheGrantDoesNotCover(t *testing.T) {
	f := newDelegationFixture(t)
	f.grant(t)
	_, err := f.mint(operator.OperationAgentCreate, f.other, "req-1")
	require.ErrorIs(t, err, operator.ErrNotDelegated, "controller outside the grant")
	_, err = f.mint(operator.OperationAgentActivate, f.controller, "req-2")
	require.ErrorIs(t, err, operator.ErrOperationNotMintable)
	_, err = f.service.Mint(context.Background(), operator.MintRequest{
		Actor: f.actor, Org: f.org, Audience: scopedGRN("globex", "operator", "hosted"),
		Operation: operator.OperationAgentCreate, Target: f.controller, RequestID: "req-3",
		Digest: operator.BodyDigest(strings.Repeat("a", 64)),
	})
	require.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "cross-organization audience: %v", err)
	_, err = f.mint(operator.OperationAgentCreate, f.controller, "req-4")
	require.NoError(t, err)
}

// TestReference_ConfigurationIsBoundToItsPlacement holds that a
// configuration change authorized for one placement is refused once the
// agent moves, including back to the same controller at a later epoch,
// and that its request identifier cannot be reminted for the new one.
func TestReference_ConfigurationIsBoundToItsPlacement(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	f.grant(t)
	minted, err := f.mint(operator.OperationAgentConfigure, f.agent, "req-1")
	require.NoError(t, err)
	got, err := f.service.Reference(ctx, f.hosted, minted.Ref)
	require.NoError(t, err)
	require.NotNil(t, got.Assignment)
	assert.Equal(t, operator.Assignment{Assignee: f.controller, Epoch: "1"}, *got.Assignment)

	f.placements.move(f.agent, operator.Assignment{Assignee: f.controller, Epoch: "3"})
	_, err = f.service.Reference(ctx, f.hosted, minted.Ref)
	require.ErrorIs(t, err, operator.ErrAuthorityRevoked)
	_, err = f.mint(operator.OperationAgentConfigure, f.agent, "req-1")
	require.ErrorIs(t, err, operator.ErrAuthorityRequestConflict)
	_, err = f.mint(operator.OperationAgentConfigure, f.agent, "req-2")
	require.NoError(t, err)
}

// TestReference_RechecksTheActorEveryTime holds that an allow decided
// at mint is not what a later check reads: once current authorization
// denies, introspection and the reference refuse, and once it cannot be
// decided, they refuse as unavailable. Nothing falls back.
func TestReference_RechecksTheActorEveryTime(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	f.grant(t)
	minted, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)

	f.authority.set(errorx.New(errorx.KindPermissionDenied, "insufficient role"))
	_, err = f.service.Introspect(ctx, f.hosted, minted.Authority)
	require.ErrorIs(t, err, operator.ErrAuthorityRevoked)
	_, err = f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "a fresh mint cannot bypass the revocation: %v", err)

	f.authority.set(errorx.New(errorx.KindUnimplemented, "CheckCurrent is not served"))
	_, err = f.service.Reference(ctx, f.hosted, minted.Ref)
	require.True(t, errorx.IsKind(err, errorx.KindUnavailable), "an engine that cannot decide currently: %v", err)

	f.authority.set(nil)
	_, err = f.service.Reference(ctx, f.hosted, minted.Ref)
	require.NoError(t, err)
}

// TestReference_DeletionLeavesNoTransferableAuthority holds the
// cascades: deleting a controller takes it out of every grant, so a
// later registration of the same identifier revives nothing, and
// deleting the hosted operator removes its grants and authorities.
func TestReference_DeletionLeavesNoTransferableAuthority(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	f.grant(t)
	minted, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)

	require.NoError(t, f.store.Delete(ctx, f.controller))
	register(t, f.store, "controller")
	_, err = f.service.Reference(ctx, f.hosted, minted.Ref)
	require.ErrorIs(t, err, operator.ErrAuthorityRevoked)

	require.NoError(t, f.store.Delete(ctx, f.hosted))
	assert.Equal(t, 0, countRows(t, f.pool, `SELECT count(*) FROM operation_authorities`))
	assert.Equal(t, 0, countRows(t, f.pool, `SELECT count(*) FROM operator_delegations`))
}

// TestReference_ActorDeletionKeepsTheReplayRecord holds that deleting
// the user an operation was minted for keeps the operation's record:
// its request identifier still refuses another actor, while current
// authorization refuses the deleted user's operation. The same request
// identifier minted by the original actor before deletion is the
// control.
func TestReference_ActorDeletionKeepsTheReplayRecord(t *testing.T) {
	ctx := context.Background()
	f := newDelegationFixture(t)
	f.grant(t)
	minted, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)
	_, err = f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)

	_, err = f.pool.Exec(ctx, `DELETE FROM users WHERE grn = $1`, f.actor.String())
	require.NoError(t, err)
	assert.Equal(t, 1, countRows(t, f.pool, `SELECT count(*) FROM operation_authorities WHERE ref = $1`, string(minted.Ref)))

	grace := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "grace")
	require.NoError(t, userpg.New(f.pool).Create(ctx, user.New(grace, "Grace")))
	_, err = f.service.Mint(ctx, operator.MintRequest{
		Actor: grace, Org: f.org, Audience: f.hosted, Operation: operator.OperationAgentCreate,
		Target: f.controller, RequestID: "req-1", Digest: operator.BodyDigest(strings.Repeat("a", 64)),
	})
	require.ErrorIs(t, err, operator.ErrAuthorityRequestConflict)

	f.authority.set(errorx.New(errorx.KindPermissionDenied, "not a member of this org"))
	_, err = f.service.Reference(ctx, f.hosted, minted.Ref)
	require.ErrorIs(t, err, operator.ErrAuthorityRevoked)
}

// TestAuthority_UnavailableStorageRefuses holds that a store that does
// not answer is unavailable and never an answer.
func TestAuthority_UnavailableStorageRefuses(t *testing.T) {
	f := newDelegationFixture(t)
	f.grant(t)
	minted, err := f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.NoError(t, err)

	f.pool.Close()
	_, err = f.service.Reference(context.Background(), f.hosted, minted.Ref)
	require.True(t, errorx.IsKind(err, errorx.KindUnavailable), "%v", err)
	_, err = f.mint(operator.OperationAgentCreate, f.controller, "req-1")
	require.True(t, errorx.IsKind(err, errorx.KindUnavailable), "%v", err)
}
