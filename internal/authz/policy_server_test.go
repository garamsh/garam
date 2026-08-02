package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/gaon/internal/authz"
	"github.com/garamsh/gaon/internal/authz/mocks"
	"github.com/garamsh/gaon/internal/grn"
	authzv1 "github.com/garamsh/gaon/proto/gen/gaon/authz/v1"
)

const validDocument = `{"Version":"2026-07-25","Statement":[{"Sid":"s1","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`

func TestPolicyGRPCServer_CreatePolicyRejectsBadInput(t *testing.T) {
	store := mocks.NewMockStore(t)
	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))

	cases := map[string]*authzv1.CreatePolicyRequest{
		"bad org grn": {OrgGrn: "nope", Name: "n", Document: validDocument, CreatedBy: "grn:root:default:user:ada"},
		"bad author":  {OrgGrn: "grn:root:default:org:acme", Name: "n", Document: validDocument, CreatedBy: "nope"},
		"empty name":  {OrgGrn: "grn:root:default:org:acme", Name: "", Document: validDocument, CreatedBy: "grn:root:default:user:ada"},
		"bad document": {
			OrgGrn: "grn:root:default:org:acme", Name: "n", Document: `{"Version":"v"}`,
			CreatedBy: "grn:root:default:user:ada",
		},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := srv.CreatePolicy(context.Background(), req)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestPolicyGRPCServer_ActivateInvalidatesTheOrgCache(t *testing.T) {
	acme := orgGRN("acme")
	store := mocks.NewMockStore(t)
	store.EXPECT().ActiveDocuments(mock.Anything, acme).
		Return(nil, nil).Once()
	store.EXPECT().SetPolicyStatus(mock.Anything, mock.Anything, authz.PolicyStatusActive).
		Return(authz.NewPolicy(policyGRN(t, acme), acme, "read-only"), nil).Once()
	store.EXPECT().ActiveDocuments(mock.Anything, acme).
		Return([]authz.PolicyDocument{policyDocument("activated", authz.EffectAllow)}, nil).Once()

	cache := authz.NewPolicyCache(store)
	srv := authz.NewPolicyGRPCServer(store, cache)

	before, err := cache.OrgStatements(context.Background(), acme)
	require.NoError(t, err)
	assert.Empty(t, before)

	_, err = srv.ActivatePolicy(context.Background(), &authzv1.ActivatePolicyRequest{
		PolicyGrn: "grn:acme:default:policy:p1",
	})
	require.NoError(t, err)

	after, err := cache.OrgStatements(context.Background(), acme)
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.Equal(t, "activated", after[0].Sid)
}

func TestPolicyGRPCServer_ArchiveInvalidatesTheOrgCache(t *testing.T) {
	acme := orgGRN("acme")
	store := mocks.NewMockStore(t)
	store.EXPECT().ActiveDocuments(mock.Anything, acme).
		Return([]authz.PolicyDocument{policyDocument("active", authz.EffectAllow)}, nil).Once()
	store.EXPECT().SetPolicyStatus(mock.Anything, mock.Anything, authz.PolicyStatusArchived).
		Return(authz.NewPolicy(policyGRN(t, acme), acme, "read-only"), nil).Once()
	store.EXPECT().ActiveDocuments(mock.Anything, acme).
		Return(nil, nil).Once()

	cache := authz.NewPolicyCache(store)
	srv := authz.NewPolicyGRPCServer(store, cache)

	before, err := cache.OrgStatements(context.Background(), acme)
	require.NoError(t, err)
	require.Len(t, before, 1)

	_, err = srv.ArchivePolicy(context.Background(), &authzv1.ArchivePolicyRequest{
		PolicyGrn: "grn:acme:default:policy:p1",
	})
	require.NoError(t, err)

	after, err := cache.OrgStatements(context.Background(), acme)
	require.NoError(t, err)
	assert.Empty(t, after)
}

func TestPolicyGRPCServer_ArchiveRejectsBadGRN(t *testing.T) {
	store := mocks.NewMockStore(t)
	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))

	_, err := srv.ArchivePolicy(context.Background(), &authzv1.ArchivePolicyRequest{PolicyGrn: "nope"})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPolicyGRPCServer_RollbackRejectsAndReportsMissing(t *testing.T) {
	store := mocks.NewMockStore(t)
	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))

	_, err := srv.RollbackPolicy(context.Background(), &authzv1.RollbackPolicyRequest{
		PolicyGrn: "grn:acme:default:policy:p1",
		Version:   0,
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	store.EXPECT().SetPolicyDefaultVersion(mock.Anything, mock.Anything, 7).
		Return(nil, authz.ErrPolicyVersionNotFound).Once()
	_, err = srv.RollbackPolicy(context.Background(), &authzv1.RollbackPolicyRequest{
		PolicyGrn: "grn:acme:default:policy:p1",
		Version:   7,
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func policyGRN(t *testing.T, orgGRN grn.GRN) grn.GRN {
	t.Helper()
	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	return g
}
