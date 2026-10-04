package authz_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/authz/mocks"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
)

func policyDocument(sid string, effect wire.Effect) authz.PolicyDocument {
	return authz.PolicyDocument{
		Version: "2026-07-25",
		Statement: []authz.Statement{{
			Sid:      sid,
			Effect:   effect,
			Action:   []string{"org:read"},
			Resource: []string{"grn:acme:*"},
		}},
	}
}

// orgWidePolicy is an active policy with no attachment: it applies to
// every subject of its org.
func orgWidePolicy(sid string, effect wire.Effect) authz.ActivePolicy {
	return authz.ActivePolicy{Document: policyDocument(sid, effect)}
}

// attachedPolicy is an active policy that applies only to subjects.
func attachedPolicy(sid string, effect wire.Effect, subjects ...grn.GRN) authz.ActivePolicy {
	return authz.ActivePolicy{Document: policyDocument(sid, effect), Subjects: subjects}
}

func subjectGRN(id string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "user", id)
}

func groupGRN(id string) grn.GRN {
	return grn.New("acme", grn.DefaultProject, "group", id)
}

func TestPolicyCache_LoadsOncePerOrg(t *testing.T) {
	store := mocks.NewMockStore(t)
	acme, other := orgGRN("acme"), orgGRN("other")
	ada := subjectGRN("ada")
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("acme-allow", wire.EffectAllow)}, nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, other).
		Return([]authz.ActivePolicy{orgWidePolicy("other-allow", wire.EffectAllow)}, nil).Once()

	cache := authz.NewPolicyCache(store)
	first, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, "acme-allow", first[0].Sid)

	second, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
	require.NoError(t, err)
	assert.Equal(t, first, second)

	third, err := cache.Statements(context.Background(), other, []grn.GRN{ada})
	require.NoError(t, err)
	require.Len(t, third, 1)
	assert.Equal(t, "other-allow", third[0].Sid)
}

// TestPolicyCache_StatementsBySubject covers what an attachment
// changes: an attached policy reaches the subject it names and the
// groups that subject belongs to, and nobody else. An unattached
// policy keeps reaching everyone.
func TestPolicyCache_StatementsBySubject(t *testing.T) {
	acme := orgGRN("acme")
	ada, bob, eng := subjectGRN("ada"), subjectGRN("bob"), groupGRN("eng")
	store := mocks.NewMockStore(t)
	store.EXPECT().ActivePolicies(mock.Anything, acme).Return([]authz.ActivePolicy{
		orgWidePolicy("org-wide", wire.EffectAllow),
		attachedPolicy("ada-only", wire.EffectAllow, ada),
		attachedPolicy("eng-only", wire.EffectAllow, eng),
	}, nil).Once()
	cache := authz.NewPolicyCache(store)

	cases := map[string]struct {
		subjects []grn.GRN
		want     []string
	}{
		"subject attachment":       {[]grn.GRN{ada}, []string{"org-wide", "ada-only"}},
		"group attachment":         {[]grn.GRN{bob, eng}, []string{"org-wide", "eng-only"}},
		"neither attachment":       {[]grn.GRN{bob}, []string{"org-wide"}},
		"both attachments at once": {[]grn.GRN{ada, eng}, []string{"org-wide", "ada-only", "eng-only"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			statements, err := cache.Statements(context.Background(), acme, c.subjects)
			require.NoError(t, err)
			sids := make([]string, 0, len(statements))
			for _, s := range statements {
				sids = append(sids, s.Sid)
			}
			assert.Equal(t, c.want, sids)
		})
	}
}

// TestPolicyCache_InvalidateAllDropsEveryOrg is the counterpart of the
// test below: what Invalidate does to the org it names, InvalidateAll
// does to every org at once. Both are read together — acme reloading
// alone would also pass on a cache that only dropped what it was
// named, so other is what says the drop was not selective. Each
// ActivePolicies expectation is exhausted by the one load it stands
// for, so a load that should have come from the cache fails the test.
func TestPolicyCache_InvalidateAllDropsEveryOrg(t *testing.T) {
	store := mocks.NewMockStore(t)
	acme, other := orgGRN("acme"), orgGRN("other")
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("acme-before", wire.EffectAllow)}, nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, other).
		Return([]authz.ActivePolicy{orgWidePolicy("other-before", wire.EffectAllow)}, nil).Once()

	cache := authz.NewPolicyCache(store)
	acmeBefore, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	require.Equal(t, "acme-before", acmeBefore[0].Sid)
	otherBefore, err := cache.Statements(context.Background(), other, nil)
	require.NoError(t, err)
	require.Equal(t, "other-before", otherBefore[0].Sid)

	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("acme-after", wire.EffectAllow)}, nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, other).
		Return([]authz.ActivePolicy{orgWidePolicy("other-after", wire.EffectAllow)}, nil).Once()
	cache.InvalidateAll()

	acmeAfter, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	assert.Equal(t, "acme-after", acmeAfter[0].Sid)
	otherAfter, err := cache.Statements(context.Background(), other, nil)
	require.NoError(t, err)
	assert.Equal(t, "other-after", otherAfter[0].Sid)
}

// TestPolicyCache_InvalidateDropsOnlyTheOrgNamed reads the two
// directions together: acme reloads because Invalidate named it, and
// other survives because Invalidate did not. Either half alone passes
// on a cache that never drops anything, or on one that drops
// everything. Each ActivePolicies expectation is exhausted by the one
// load it stands for, so a load that should have come from the cache
// fails the test.
func TestPolicyCache_InvalidateDropsOnlyTheOrgNamed(t *testing.T) {
	store := mocks.NewMockStore(t)
	acme, other := orgGRN("acme"), orgGRN("other")
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("before", wire.EffectAllow)}, nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, other).
		Return([]authz.ActivePolicy{orgWidePolicy("other", wire.EffectAllow)}, nil).Once()

	cache := authz.NewPolicyCache(store)
	before, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	require.Equal(t, "before", before[0].Sid)
	otherBefore, err := cache.Statements(context.Background(), other, nil)
	require.NoError(t, err)
	require.Equal(t, "other", otherBefore[0].Sid)

	// An org nothing has cached leaves both entries where they are.
	cache.Invalidate(grn.New(grn.RootOwner, grn.DefaultProject, "org", "missing"))
	acmeAfterUnknown, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	assert.Equal(t, before, acmeAfterUnknown)
	otherAfterUnknown, err := cache.Statements(context.Background(), other, nil)
	require.NoError(t, err)
	assert.Equal(t, otherBefore, otherAfterUnknown)

	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("after", wire.EffectDeny)}, nil).Once()
	cache.Invalidate(acme)

	after, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	assert.Equal(t, "after", after[0].Sid)
	assert.Equal(t, wire.EffectDeny, after[0].Effect)
	otherAfterAcme, err := cache.Statements(context.Background(), other, nil)
	require.NoError(t, err)
	assert.Equal(t, otherBefore, otherAfterAcme)
}

// gatedFill holds the first ActivePolicies call open so a test can act
// while a fill is in flight, and counts every call. The second call and
// after answer archived: the org grants nothing.
type gatedFill struct {
	inStore chan struct{}
	release chan struct{}
	calls   atomic.Int64
}

func newGatedFill() *gatedFill {
	return &gatedFill{inStore: make(chan struct{}), release: make(chan struct{})}
}

// answer is the ActivePolicies the gated store returns. The first call
// blocks inside the store until release is closed and then answers with
// the policy that was live when it read; every later call answers
// archived.
func (g *gatedFill) answer(active []authz.ActivePolicy) func(context.Context, grn.GRN) ([]authz.ActivePolicy, error) {
	return func(context.Context, grn.GRN) ([]authz.ActivePolicy, error) {
		if g.calls.Add(1) == 1 {
			close(g.inStore)
			<-g.release
			return active, nil
		}
		return nil, nil
	}
}

// TestPolicyCache_HonoursAnInvalidationRaisedDuringAFill drives the
// window between the store read and the map write. The fill reads the
// store, an archive invalidates the org while that read is in flight,
// and the fill then finishes: what it read is a snapshot from before
// the archive, and installing it would put the org back to where it was
// with nothing left to drop it again.
//
// The interleaving is driven rather than described: the invalidation
// runs while the loader is inside ActivePolicies, which is where
// PolicyGRPCServer.ArchivePolicy's own Invalidate lands when a check
// misses at the same moment. The store call count is the second
// assertion because an empty answer alone would also hold for a cache
// that served an empty entry it should not have had.
func TestPolicyCache_HonoursAnInvalidationRaisedDuringAFill(t *testing.T) {
	acme := orgGRN("acme")
	ada := subjectGRN("ada")
	gate := newGatedFill()

	store := mocks.NewMockStore(t)
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		RunAndReturn(gate.answer([]authz.ActivePolicy{orgWidePolicy("stale-allow", wire.EffectAllow)}))

	cache := authz.NewPolicyCache(store)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
		assert.NoError(t, err)
	}()

	<-gate.inStore
	cache.Invalidate(acme)
	close(gate.release)
	wg.Wait()

	got, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
	require.NoError(t, err)
	assert.Empty(t, got, "the archived policy is still being served")
	assert.Equal(t, int64(2), gate.calls.Load(), "the store was not read again after the invalidation")
}

// TestPolicyCache_InvalidateAllDuringAFillIsAlsoHonoured is the
// listener's half of the same window. A reopened connection drops every
// org because it cannot know what it missed, and a fill in flight when
// that happens carries exactly what the reconnect exists to distrust.
func TestPolicyCache_InvalidateAllDuringAFillIsAlsoHonoured(t *testing.T) {
	acme := orgGRN("acme")
	ada := subjectGRN("ada")
	gate := newGatedFill()

	store := mocks.NewMockStore(t)
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		RunAndReturn(gate.answer([]authz.ActivePolicy{orgWidePolicy("stale-allow", wire.EffectAllow)}))

	cache := authz.NewPolicyCache(store)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
		assert.NoError(t, err)
	}()

	<-gate.inStore
	cache.InvalidateAll()
	close(gate.release)
	wg.Wait()

	got, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, int64(2), gate.calls.Load(), "the store was not read again after the invalidation")
}

// TestPolicyCache_ConcurrentMissesConvergeOnOneSnapshot holds what the
// generation check must not break. Several checks missing on one org at
// once each read the store, and the entry left behind is one snapshot:
// a later check reads what they all read rather than whichever fill
// happened to be last past a guard that turned some of them away.
func TestPolicyCache_ConcurrentMissesConvergeOnOneSnapshot(t *testing.T) {
	acme := orgGRN("acme")
	ada := subjectGRN("ada")
	store := mocks.NewMockStore(t)
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("acme-allow", wire.EffectAllow)}, nil)

	cache := authz.NewPolicyCache(store)
	const callers = 8
	got := make([][]authz.Statement, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statements, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
			assert.NoError(t, err)
			got[i] = statements
		}()
	}
	wg.Wait()
	for _, statements := range got {
		assert.Equal(t, got[0], statements, "two concurrent misses served different snapshots")
	}

	after, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
	require.NoError(t, err)
	assert.Equal(t, got[0], after, "the entry left behind is not what the callers were served")
}

// TestPolicyCache_CurrentStatementsReadsTheStoreOnEveryCall holds a
// stale cache: CurrentStatements reads the store on each call, and
// Statements still answers from the cache.
func TestPolicyCache_CurrentStatementsReadsTheStoreOnEveryCall(t *testing.T) {
	store := mocks.NewMockStore(t)
	acme, ada := orgGRN("acme"), subjectGRN("ada")
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("before", wire.EffectAllow)}, nil).Once()
	cache := authz.NewPolicyCache(store)
	cached, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
	require.NoError(t, err)
	require.Equal(t, "before", cached[0].Sid)

	store.EXPECT().ActivePolicies(mock.Anything, acme).Return([]authz.ActivePolicy{
		orgWidePolicy("after", wire.EffectDeny),
		attachedPolicy("bob-only", wire.EffectAllow, subjectGRN("bob")),
	}, nil).Twice()
	for range 2 {
		current, err := cache.CurrentStatements(context.Background(), acme, []grn.GRN{ada})
		require.NoError(t, err)
		require.Len(t, current, 1, "a policy attached to another subject does not apply")
		assert.Equal(t, "after", current[0].Sid)
	}

	stale, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
	require.NoError(t, err)
	assert.Equal(t, cached, stale, "control: Statements still answers from the cache")
}

// TestPolicyCache_CurrentStatementsFailsWhenTheStoreDoes fails with
// the store even though the cache holds the org.
func TestPolicyCache_CurrentStatementsFailsWhenTheStoreDoes(t *testing.T) {
	store := mocks.NewMockStore(t)
	acme := orgGRN("acme")
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("cached", wire.EffectAllow)}, nil).Once()
	cache := authz.NewPolicyCache(store)
	_, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)

	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return(nil, errorx.New(errorx.KindUnavailable, "database unreachable")).Once()
	current, err := cache.CurrentStatements(context.Background(), acme, nil)
	require.True(t, errorx.IsKind(err, errorx.KindUnavailable), "got %v", err)
	assert.Nil(t, current)

	cached, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err, "control: the cached entry is still there to answer from")
	assert.Equal(t, "cached", cached[0].Sid)
}
