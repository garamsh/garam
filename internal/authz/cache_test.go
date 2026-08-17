package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/authz/mocks"
	"github.com/garamsh/garam/internal/grn"
)

func policyDocument(sid string, effect authz.Effect) authz.PolicyDocument {
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
func orgWidePolicy(sid string, effect authz.Effect) authz.ActivePolicy {
	return authz.ActivePolicy{Document: policyDocument(sid, effect)}
}

// attachedPolicy is an active policy that applies only to subjects.
func attachedPolicy(sid string, effect authz.Effect, subjects ...grn.GRN) authz.ActivePolicy {
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
		Return([]authz.ActivePolicy{orgWidePolicy("acme-allow", authz.EffectAllow)}, nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, other).
		Return([]authz.ActivePolicy{orgWidePolicy("other-allow", authz.EffectAllow)}, nil).Once()

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
		orgWidePolicy("org-wide", authz.EffectAllow),
		attachedPolicy("ada-only", authz.EffectAllow, ada),
		attachedPolicy("eng-only", authz.EffectAllow, eng),
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
		Return([]authz.ActivePolicy{orgWidePolicy("before", authz.EffectAllow)}, nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, other).
		Return([]authz.ActivePolicy{orgWidePolicy("other", authz.EffectAllow)}, nil).Once()

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
		Return([]authz.ActivePolicy{orgWidePolicy("after", authz.EffectDeny)}, nil).Once()
	cache.Invalidate(acme)

	after, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	assert.Equal(t, "after", after[0].Sid)
	assert.Equal(t, authz.EffectDeny, after[0].Effect)
	otherAfterAcme, err := cache.Statements(context.Background(), other, nil)
	require.NoError(t, err)
	assert.Equal(t, otherBefore, otherAfterAcme)
}
