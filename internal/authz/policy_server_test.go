package authz_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/authz/mocks"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
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

// TestPolicyGRPCServer_AddPolicyVersion reads both halves of the RPC:
// the store is handed the policy, document and author the request
// named, and the response carries the version the store returned. The
// stored version's document, author and number all differ from the
// request's, so a response that echoes the request, reads the author
// where the policy belongs, or drops the number stops matching.
func TestPolicyGRPCServer_AddPolicyVersion(t *testing.T) {
	g := policyGRN(t, orgGRN("acme"))
	ada, bob := subjectGRN("ada"), subjectGRN("bob")
	stored := policyDocument("stored", wire.EffectDeny)
	storedAt := time.Date(2026, 7, 25, 9, 30, 0, 0, time.UTC)
	store := mocks.NewMockStore(t)
	store.EXPECT().AddPolicyVersion(mock.Anything, g, documentOf(t, validDocument), ada).
		Return(authz.NewPolicyVersion(g, 4, stored, bob, storedAt), nil).Once()

	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))
	resp, err := srv.AddPolicyVersion(context.Background(), &authzv1.AddPolicyVersionRequest{
		PolicyGrn: g.String(),
		Document:  validDocument,
		CreatedBy: ada.String(),
	})
	require.NoError(t, err)

	v := resp.GetVersion()
	assert.Equal(t, g.String(), v.GetPolicyGrn())
	assert.Equal(t, int32(4), v.GetVersion())
	assert.Equal(t, bob.String(), v.GetCreatedBy())
	assert.Equal(t, storedAt, v.GetCreatedAt().AsTime())
	assert.Equal(t, stored, documentOf(t, v.GetDocument()))
}

// TestPolicyGRPCServer_ListPolicyVersions holds two versions of one
// policy, numbered differently, authored by different users at
// different times and over different documents, so an entry that
// carries its neighbour's field, its own author in place of its
// policy, or nothing at all stops matching.
func TestPolicyGRPCServer_ListPolicyVersions(t *testing.T) {
	g := policyGRN(t, orgGRN("acme"))
	ada, bob := subjectGRN("ada"), subjectGRN("bob")
	original, revised := policyDocument("original", wire.EffectAllow), policyDocument("revised", wire.EffectDeny)
	createdAt := time.Date(2026, 7, 25, 9, 30, 0, 0, time.UTC)
	store := mocks.NewMockStore(t)
	store.EXPECT().ListPolicyVersions(mock.Anything, g).
		Return([]*authz.PolicyVersion{
			authz.NewPolicyVersion(g, 1, original, ada, createdAt),
			authz.NewPolicyVersion(g, 2, revised, bob, createdAt.Add(time.Hour)),
		}, nil).Once()

	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))
	resp, err := srv.ListPolicyVersions(context.Background(), &authzv1.ListPolicyVersionsRequest{
		PolicyGrn: g.String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.GetVersions(), 2)

	first, second := resp.GetVersions()[0], resp.GetVersions()[1]
	assert.Equal(t, g.String(), first.GetPolicyGrn())
	assert.Equal(t, int32(1), first.GetVersion())
	assert.Equal(t, ada.String(), first.GetCreatedBy())
	assert.Equal(t, createdAt, first.GetCreatedAt().AsTime())
	assert.Equal(t, original, documentOf(t, first.GetDocument()))
	assert.Equal(t, int32(2), second.GetVersion())
	assert.Equal(t, bob.String(), second.GetCreatedBy())
	assert.Equal(t, createdAt.Add(time.Hour), second.GetCreatedAt().AsTime())
	assert.Equal(t, revised, documentOf(t, second.GetDocument()))
}

func TestPolicyGRPCServer_ActivateInvalidatesTheOrgCache(t *testing.T) {
	acme := orgGRN("acme")
	store := mocks.NewMockStore(t)
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return(nil, nil).Once()
	store.EXPECT().SetPolicyStatus(mock.Anything, mock.Anything, wire.PolicyStatusActive).
		Return(authz.NewPolicy(policyGRN(t, acme), acme, "read-only"), nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{orgWidePolicy("activated", wire.EffectAllow)}, nil).Once()

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
		Return([]authz.ActivePolicy{orgWidePolicy("active", wire.EffectAllow)}, nil).Once()
	store.EXPECT().SetPolicyStatus(mock.Anything, mock.Anything, wire.PolicyStatusArchived).
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

// TestPolicyGRPCServer_ArchiveRejectsBadGRN reads the GRN that parses
// against the one that does not: the same call over the same server,
// differing only in the string. An archive that refuses whatever it is
// given stops satisfying the first row.
func TestPolicyGRPCServer_ArchiveRejectsBadGRN(t *testing.T) {
	acme := orgGRN("acme")
	g := policyGRN(t, acme)
	store := mocks.NewMockStore(t)
	store.EXPECT().SetPolicyStatus(mock.Anything, g, wire.PolicyStatusArchived).
		Return(authz.NewPolicy(g, acme, "read-only"), nil).Once()
	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))

	cases := map[string]struct {
		policy string
		want   codes.Code
	}{
		"a policy grn that parses": {g.String(), codes.OK},
		"one that does not":        {"nope", codes.InvalidArgument},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := srv.ArchivePolicy(context.Background(), &authzv1.ArchivePolicyRequest{PolicyGrn: c.policy})
			assert.Equal(t, c.want, status.Code(err))
		})
	}
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
		Return([]authz.ActivePolicy{orgWidePolicy("org-wide", wire.EffectAllow)}, nil).Once()
	store.EXPECT().GetPolicy(mock.Anything, g).
		Return(authz.NewPolicy(g, acme, "read-only"), nil).Once()
	store.EXPECT().AttachPolicy(mock.Anything, g, ada).
		Return(&authz.PolicyAttachment{}, nil).Once()
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		Return([]authz.ActivePolicy{attachedPolicy("ada-only", wire.EffectAllow, ada)}, nil).Once()

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

// TestPolicyGRPCServer_DetachRefusesTheLastAttachment maps the store's
// refusal to failed_precondition: a caller telling that apart from the
// not_found above can tell "no such attachment" from "this one stays".
// The detach the store allows is the control — the same call over the
// same policy, differing only in what the store answers, so a detach
// that refuses whatever it is given stops satisfying the first row.
func TestPolicyGRPCServer_DetachRefusesTheLastAttachment(t *testing.T) {
	acme := orgGRN("acme")
	g, ada, bob := policyGRN(t, acme), subjectGRN("ada"), subjectGRN("bob")
	store := mocks.NewMockStore(t)
	store.EXPECT().GetPolicy(mock.Anything, g).
		Return(authz.NewPolicy(g, acme, "read-only"), nil).Twice()
	store.EXPECT().DetachPolicy(mock.Anything, g, bob).Return(nil).Once()
	store.EXPECT().DetachPolicy(mock.Anything, g, ada).
		Return(authz.ErrLastAttachment).Once()

	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))
	cases := map[string]struct {
		subject grn.GRN
		want    codes.Code
	}{
		"an attachment that is not the last": {bob, codes.OK},
		"the last one":                       {ada, codes.FailedPrecondition},
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

// TestPolicyGRPCServer_AttachmentRejectsBadGRN reads the attach whose
// two GRNs both parse against the two that spoil one of them: an
// attach that refuses whatever it is given stops satisfying the first
// row while still satisfying the other two.
func TestPolicyGRPCServer_AttachmentRejectsBadGRN(t *testing.T) {
	acme := orgGRN("acme")
	g, ada := policyGRN(t, acme), subjectGRN("ada")
	store := mocks.NewMockStore(t)
	store.EXPECT().GetPolicy(mock.Anything, g).
		Return(authz.NewPolicy(g, acme, "read-only"), nil).Once()
	store.EXPECT().AttachPolicy(mock.Anything, g, ada).
		Return(&authz.PolicyAttachment{}, nil).Once()
	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))

	cases := map[string]struct {
		req  *authzv1.AttachPolicyRequest
		want codes.Code
	}{
		"both grns parse": {&authzv1.AttachPolicyRequest{PolicyGrn: g.String(), SubjectGrn: ada.String()}, codes.OK},
		"bad policy grn":  {&authzv1.AttachPolicyRequest{PolicyGrn: "nope", SubjectGrn: ada.String()}, codes.InvalidArgument},
		"bad subject grn": {&authzv1.AttachPolicyRequest{PolicyGrn: g.String(), SubjectGrn: "nope"}, codes.InvalidArgument},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := srv.AttachPolicy(context.Background(), c.req)
			assert.Equal(t, c.want, status.Code(err))
		})
	}
}

// TestPolicyGRPCServer_ListPolicyAttachments answers for the listing
// alone: the store is a mock, so an attach made through the server
// could not reach the list it returns. What the response carries is
// what the store held, one entry each. The two attachments name one
// policy and two different subjects at two different times, so an
// entry that carries its neighbour's subject, its own policy in place
// of its subject, or nothing at all stops matching.
func TestPolicyGRPCServer_ListPolicyAttachments(t *testing.T) {
	g := policyGRN(t, orgGRN("acme"))
	ada, bob := subjectGRN("ada"), subjectGRN("bob")
	attachedAt := time.Date(2026, 7, 25, 9, 30, 0, 0, time.UTC)
	store := mocks.NewMockStore(t)
	store.EXPECT().ListPolicyAttachments(mock.Anything, g).
		Return([]*authz.PolicyAttachment{
			authz.NewPolicyAttachment(g, ada, attachedAt),
			authz.NewPolicyAttachment(g, bob, attachedAt.Add(time.Hour)),
		}, nil).Once()

	srv := authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store))
	resp, err := srv.ListPolicyAttachments(context.Background(), &authzv1.ListPolicyAttachmentsRequest{
		PolicyGrn: g.String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.GetAttachments(), 2)

	first, second := resp.GetAttachments()[0], resp.GetAttachments()[1]
	assert.Equal(t, g.String(), first.GetPolicyGrn())
	assert.Equal(t, ada.String(), first.GetSubjectGrn())
	assert.Equal(t, attachedAt, first.GetCreatedAt().AsTime())
	assert.Equal(t, bob.String(), second.GetSubjectGrn())
	assert.Equal(t, attachedAt.Add(time.Hour), second.GetCreatedAt().AsTime())
}

// documentOf parses the document a response carries, so a response
// that carries something other than a policy document fails here.
func documentOf(t *testing.T, raw string) authz.PolicyDocument {
	t.Helper()
	doc, err := authz.ParsePolicyDocument([]byte(raw))
	require.NoError(t, err)
	return doc
}

func policyGRN(t *testing.T, orgGRN grn.GRN) grn.GRN {
	t.Helper()
	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	return g
}
