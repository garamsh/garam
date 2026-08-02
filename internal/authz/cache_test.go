package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/authz"
	"github.com/garamsh/gaon/internal/authz/mocks"
	"github.com/garamsh/gaon/internal/grn"
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

func TestPolicyCache_LoadsOncePerOrg(t *testing.T) {
	store := mocks.NewMockStore(t)
	acme, other := orgGRN("acme"), orgGRN("other")
	store.EXPECT().ActiveDocuments(mock.Anything, acme).
		Return([]authz.PolicyDocument{policyDocument("acme-allow", authz.EffectAllow)}, nil).Once()
	store.EXPECT().ActiveDocuments(mock.Anything, other).
		Return([]authz.PolicyDocument{policyDocument("other-allow", authz.EffectAllow)}, nil).Once()

	cache := authz.NewPolicyCache(store)
	first, err := cache.OrgStatements(context.Background(), acme)
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, "acme-allow", first[0].Sid)

	second, err := cache.OrgStatements(context.Background(), acme)
	require.NoError(t, err)
	assert.Equal(t, first, second)

	third, err := cache.OrgStatements(context.Background(), other)
	require.NoError(t, err)
	require.Len(t, third, 1)
	assert.Equal(t, "other-allow", third[0].Sid)
}

func TestPolicyCache_InvalidateReloadsTheOrg(t *testing.T) {
	store := mocks.NewMockStore(t)
	acme := orgGRN("acme")
	store.EXPECT().ActiveDocuments(mock.Anything, acme).
		Return([]authz.PolicyDocument{policyDocument("before", authz.EffectAllow)}, nil).Once()
	store.EXPECT().ActiveDocuments(mock.Anything, acme).
		Return([]authz.PolicyDocument{policyDocument("after", authz.EffectDeny)}, nil).Once()

	cache := authz.NewPolicyCache(store)
	before, err := cache.OrgStatements(context.Background(), acme)
	require.NoError(t, err)
	assert.Equal(t, "before", before[0].Sid)

	cache.Invalidate(acme)

	after, err := cache.OrgStatements(context.Background(), acme)
	require.NoError(t, err)
	assert.Equal(t, "after", after[0].Sid)
	assert.Equal(t, authz.EffectDeny, after[0].Effect)
}

func TestPolicyCache_InvalidateUnknownOrgIsANoop(t *testing.T) {
	store := mocks.NewMockStore(t)
	cache := authz.NewPolicyCache(store)
	cache.Invalidate(grn.New(grn.RootOwner, grn.DefaultProject, "org", "missing"))
}
