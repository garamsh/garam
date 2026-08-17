package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/authz/mocks"
	"github.com/garamsh/garam/internal/grn"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

const validDocument = `{"Version":"2026-07-25","Statement":[{"Sid":"s1","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`

// TestPolicyGRPCServer_CreatePolicyRejectsBadInput reads the accepted
// row against the four refused ones: each of those is the accepted
// request with one field spoiled, so a CreatePolicy that refuses
// everything stops satisfying the first row while still satisfying the
// rest.
func TestPolicyGRPCServer_CreatePolicyRejectsBadInput(t *testing.T) {
	acme := orgGRN("acme")
	store := mocks.NewMockStore(t)
	store.EXPECT().CreatePolicy(mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(authz.NewPolicy(policyGRN(t, acme), acme, "n"), nil).Once()
	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))

	cases := map[string]struct {
		req  *authzv1.CreatePolicyRequest
		want codes.Code
	}{
		"accepted": {
			&authzv1.CreatePolicyRequest{
				OrgGrn: "grn:root:default:org:acme", Name: "n", Document: validDocument,
				CreatedBy: "grn:root:default:user:ada",
			},
			codes.OK,
		},
		"bad org grn": {
			&authzv1.CreatePolicyRequest{OrgGrn: "nope", Name: "n", Document: validDocument, CreatedBy: "grn:root:default:user:ada"},
			codes.InvalidArgument,
		},
		"bad author": {
			&authzv1.CreatePolicyRequest{OrgGrn: "grn:root:default:org:acme", Name: "n", Document: validDocument, CreatedBy: "nope"},
			codes.InvalidArgument,
		},
		"empty name": {
			&authzv1.CreatePolicyRequest{OrgGrn: "grn:root:default:org:acme", Name: "", Document: validDocument, CreatedBy: "grn:root:default:user:ada"},
			codes.InvalidArgument,
		},
		"bad document": {
			&authzv1.CreatePolicyRequest{
				OrgGrn: "grn:root:default:org:acme", Name: "n", Document: `{"Version":"v"}`,
				CreatedBy: "grn:root:default:user:ada",
			},
			codes.InvalidArgument,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			resp, err := srv.CreatePolicy(context.Background(), c.req)
			require.Equal(t, c.want, status.Code(err))
			if c.want == codes.OK {
				assert.Equal(t, "n", resp.GetPolicy().GetName())
			}
		})
	}
}

func TestPolicyGRPCServer_ActivateInvalidatesTheOrgCache(t *testing.T) {
	acme := orgGRN("acme")
	store := mocks.NewMockStore(t)
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return(nil, nil).Once()
	store.EXPECT().SetPolicyStatus(mock.Anything, mock.Anything, authz.PolicyStatusActive).
		Return(authz.NewPolicy(policyGRN(t, acme), acme, "read-only"), nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("activated", authz.EffectAllow)}, nil).Once()

	cache := authz.NewPolicyCache(store)
	srv := authz.NewPolicyGRPCServer(store, cache)

	before, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	assert.Empty(t, before)

	_, err = srv.ActivatePolicy(context.Background(), &authzv1.ActivatePolicyRequest{
		PolicyGrn: "grn:acme:default:policy:p1",
	})
	require.NoError(t, err)

	after, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.Equal(t, "activated", after[0].Sid)
}

func TestPolicyGRPCServer_ArchiveInvalidatesTheOrgCache(t *testing.T) {
	acme := orgGRN("acme")
	store := mocks.NewMockStore(t)
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("active", authz.EffectAllow)}, nil).Once()
	store.EXPECT().SetPolicyStatus(mock.Anything, mock.Anything, authz.PolicyStatusArchived).
		Return(authz.NewPolicy(policyGRN(t, acme), acme, "read-only"), nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return(nil, nil).Once()

	cache := authz.NewPolicyCache(store)
	srv := authz.NewPolicyGRPCServer(store, cache)

	before, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	require.Len(t, before, 1)

	_, err = srv.ArchivePolicy(context.Background(), &authzv1.ArchivePolicyRequest{
		PolicyGrn: "grn:acme:default:policy:p1",
	})
	require.NoError(t, err)

	after, err := cache.Statements(context.Background(), acme, nil)
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

// TestPolicyGRPCServer_RollbackRejectsAndReportsMissing reads version 1
// against the two refusals: it is the lowest version a policy has, so
// it is what stops being accepted if the guard below zero is moved one
// place up.
func TestPolicyGRPCServer_RollbackRejectsAndReportsMissing(t *testing.T) {
	acme := orgGRN("acme")
	store := mocks.NewMockStore(t)
	store.EXPECT().SetPolicyDefaultVersion(mock.Anything, mock.Anything, 1).
		Return(authz.NewPolicy(policyGRN(t, acme), acme, "read-only"), nil).Once()
	store.EXPECT().SetPolicyDefaultVersion(mock.Anything, mock.Anything, 7).
		Return(nil, authz.ErrPolicyVersionNotFound).Once()
	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))

	cases := map[string]struct {
		version int32
		want    codes.Code
	}{
		"the version every policy has": {1, codes.OK},
		"no version is numbered zero":  {0, codes.InvalidArgument},
		"a version this policy lacks":  {7, codes.NotFound},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := srv.RollbackPolicy(context.Background(), &authzv1.RollbackPolicyRequest{
				PolicyGrn: "grn:acme:default:policy:p1",
				Version:   c.version,
			})
			assert.Equal(t, c.want, status.Code(err))
		})
	}
}

func TestPolicyGRPCServer_AttachInvalidatesTheOrgCache(t *testing.T) {
	acme := orgGRN("acme")
	g, ada := policyGRN(t, acme), subjectGRN("ada")
	store := mocks.NewMockStore(t)
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("org-wide", authz.EffectAllow)}, nil).Once()
	store.EXPECT().GetPolicy(mock.Anything, g).
		Return(authz.NewPolicy(g, acme, "read-only"), nil).Once()
	store.EXPECT().AttachPolicy(mock.Anything, g, ada).
		Return(&authz.PolicyAttachment{}, nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{attachedPolicy("ada-only", authz.EffectAllow, ada)}, nil).Once()

	cache := authz.NewPolicyCache(store)
	srv := authz.NewPolicyGRPCServer(store, cache)

	before, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	require.Len(t, before, 1)

	_, err = srv.AttachPolicy(context.Background(), &authzv1.AttachPolicyRequest{
		PolicyGrn:  g.String(),
		SubjectGrn: ada.String(),
	})
	require.NoError(t, err)

	// The reloaded policy now reaches its subject and nobody else.
	after, err := cache.Statements(context.Background(), acme, []grn.GRN{ada})
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.Equal(t, "ada-only", after[0].Sid)

	others, err := cache.Statements(context.Background(), acme, []grn.GRN{subjectGRN("bob")})
	require.NoError(t, err)
	assert.Empty(t, others)
}

// TestPolicyGRPCServer_DetachReportsMissing reads the attachment that
// is there against the one that is not: the same call over the same
// policy, differing only in whether the store holds the subject. A
// detach that reports missing whatever it is given stops satisfying
// the first row.
func TestPolicyGRPCServer_DetachReportsMissing(t *testing.T) {
	acme := orgGRN("acme")
	g, ada, bob := policyGRN(t, acme), subjectGRN("ada"), subjectGRN("bob")
	store := mocks.NewMockStore(t)
	store.EXPECT().GetPolicy(mock.Anything, g).
		Return(authz.NewPolicy(g, acme, "read-only"), nil).Twice()
	store.EXPECT().DetachPolicy(mock.Anything, g, ada).Return(nil).Once()
	store.EXPECT().DetachPolicy(mock.Anything, g, bob).
		Return(authz.ErrAttachmentNotFound).Once()

	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))
	cases := map[string]struct {
		subject grn.GRN
		want    codes.Code
	}{
		"an attachment the policy has": {ada, codes.OK},
		"one it does not have":         {bob, codes.NotFound},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := srv.DetachPolicy(context.Background(), &authzv1.DetachPolicyRequest{
				PolicyGrn:  g.String(),
				SubjectGrn: c.subject.String(),
			})
			assert.Equal(t, c.want, status.Code(err))
		})
	}
}

func TestPolicyGRPCServer_AttachmentRejectsBadGRN(t *testing.T) {
	store := mocks.NewMockStore(t)
	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))

	cases := map[string]*authzv1.AttachPolicyRequest{
		"bad policy grn":  {PolicyGrn: "nope", SubjectGrn: "grn:root:default:user:ada"},
		"bad subject grn": {PolicyGrn: "grn:acme:default:policy:p1", SubjectGrn: "nope"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := srv.AttachPolicy(context.Background(), req)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

// TestPolicyGRPCServer_ListPolicyAttachments answers for the listing
// alone: the store is a mock, so an attach made through the server
// could not reach the list it returns. What the response carries is
// what the store held, one entry each.
func TestPolicyGRPCServer_ListPolicyAttachments(t *testing.T) {
	g := policyGRN(t, orgGRN("acme"))
	store := mocks.NewMockStore(t)
	store.EXPECT().ListPolicyAttachments(mock.Anything, g).
		Return([]*authz.PolicyAttachment{{}, {}}, nil).Once()

	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))
	resp, err := srv.ListPolicyAttachments(context.Background(), &authzv1.ListPolicyAttachmentsRequest{
		PolicyGrn: g.String(),
	})
	require.NoError(t, err)
	assert.Len(t, resp.GetAttachments(), 2)
}

func policyGRN(t *testing.T, orgGRN grn.GRN) grn.GRN {
	t.Helper()
	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	return g
}
