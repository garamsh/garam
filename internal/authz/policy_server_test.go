package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

const validDocument = `{"Version":"2026-07-25","Statement":[{"Sid":"s1","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`

// newPolicyServer serves the policy lifecycle over store, with the
// cache its writes invalidate.
func newPolicyServer(store authz.Store) (*authz.PolicyGRPCServer, *authz.PolicyCache) {
	cache := authz.NewPolicyCache(store)
	return authz.NewPolicyGRPCServer(store, cache), cache
}

// TestPolicyGRPCServer_CreatePolicyRejectsBadInput reads the accepted
// row against the four refused ones: each of those is the accepted
// request with one field spoiled, so a CreatePolicy that refuses
// everything stops satisfying the first row while still satisfying the
// rest. The accepted request is the one policy stored.
func TestPolicyGRPCServer_CreatePolicyRejectsBadInput(t *testing.T) {
	acme := orgGRN("acme")
	store := newFakeStore()
	srv, _ := newPolicyServer(store)

	cases := map[string]struct {
		req  *authzv1.CreatePolicyRequest
		want codes.Code
	}{
		"accepted": {
			&authzv1.CreatePolicyRequest{
				OrgGrn: acme.String(), Name: "n", Document: validDocument,
				CreatedBy: "grn:root:default:user:ada",
			},
			codes.OK,
		},
		"bad org grn": {
			&authzv1.CreatePolicyRequest{OrgGrn: "nope", Name: "n", Document: validDocument, CreatedBy: "grn:root:default:user:ada"},
			codes.InvalidArgument,
		},
		"bad author": {
			&authzv1.CreatePolicyRequest{OrgGrn: acme.String(), Name: "n", Document: validDocument, CreatedBy: "nope"},
			codes.InvalidArgument,
		},
		"empty name": {
			&authzv1.CreatePolicyRequest{OrgGrn: acme.String(), Name: "", Document: validDocument, CreatedBy: "grn:root:default:user:ada"},
			codes.InvalidArgument,
		},
		"bad document": {
			&authzv1.CreatePolicyRequest{
				OrgGrn: acme.String(), Name: "n", Document: `{"Version":"v"}`,
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

	stored, err := store.ListPolicies(context.Background(), acme)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "n", stored[0].Name())
}

// TestPolicyGRPCServer_AddPolicyVersion reads both halves of the RPC:
// the store holds a second version with the document and author the
// request named, and the response carries that stored version. The
// request's author differs from the first version's, so a version that
// keeps its predecessor's author stops matching.
func TestPolicyGRPCServer_AddPolicyVersion(t *testing.T) {
	store := newFakeStore()
	g := store.seed(t, orgGRN("acme"), wire.PolicyStatusDraft, policyDocument("original", wire.EffectDeny))
	ada := subjectGRN("ada")
	srv, _ := newPolicyServer(store)

	resp, err := srv.AddPolicyVersion(context.Background(), &authzv1.AddPolicyVersionRequest{
		PolicyGrn: g.String(),
		Document:  validDocument,
		CreatedBy: ada.String(),
	})
	require.NoError(t, err)

	versions, err := store.ListPolicyVersions(context.Background(), g)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	stored := versions[1]
	assert.Equal(t, documentOf(t, validDocument), stored.Document())
	assert.Equal(t, ada, stored.CreatedBy())

	v := resp.GetVersion()
	assert.Equal(t, g.String(), v.GetPolicyGrn())
	assert.Equal(t, int32(2), v.GetVersion())
	assert.Equal(t, ada.String(), v.GetCreatedBy())
	assert.Equal(t, stored.CreatedAt(), v.GetCreatedAt().AsTime())
	assert.Equal(t, stored.Document(), documentOf(t, v.GetDocument()))
}

// TestPolicyGRPCServer_ListPolicyVersions holds two versions of one
// policy, numbered differently, authored by different users at
// different times and over different documents, so an entry that
// carries its neighbour's field, its own author in place of its
// policy, or nothing at all stops matching.
func TestPolicyGRPCServer_ListPolicyVersions(t *testing.T) {
	store := newFakeStore()
	g := store.seed(t, orgGRN("acme"), wire.PolicyStatusDraft, policyDocument("original", wire.EffectAllow))
	_, err := store.AddPolicyVersion(context.Background(), g, policyDocument("revised", wire.EffectDeny), subjectGRN("bob"))
	require.NoError(t, err)
	stored, err := store.ListPolicyVersions(context.Background(), g)
	require.NoError(t, err)
	srv, _ := newPolicyServer(store)

	resp, err := srv.ListPolicyVersions(context.Background(), &authzv1.ListPolicyVersionsRequest{
		PolicyGrn: g.String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.GetVersions(), len(stored))
	for i, got := range resp.GetVersions() {
		assert.Equal(t, g.String(), got.GetPolicyGrn())
		assert.Equal(t, int32(stored[i].Version()), got.GetVersion())
		assert.Equal(t, stored[i].CreatedBy().String(), got.GetCreatedBy())
		assert.Equal(t, stored[i].CreatedAt(), got.GetCreatedAt().AsTime())
		assert.Equal(t, stored[i].Document(), documentOf(t, got.GetDocument()))
	}
}

func TestPolicyGRPCServer_ActivateInvalidatesTheOrgCache(t *testing.T) {
	acme := orgGRN("acme")
	store := newFakeStore()
	g := store.seed(t, acme, wire.PolicyStatusDraft, policyDocument("activated", wire.EffectAllow))
	srv, cache := newPolicyServer(store)

	before, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	assert.Empty(t, before)

	_, err = srv.ActivatePolicy(context.Background(), &authzv1.ActivatePolicyRequest{PolicyGrn: g.String()})
	require.NoError(t, err)

	after, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.Equal(t, "activated", after[0].Sid)
}

func TestPolicyGRPCServer_ArchiveInvalidatesTheOrgCache(t *testing.T) {
	acme := orgGRN("acme")
	store := newFakeStore()
	g := store.seed(t, acme, wire.PolicyStatusActive, policyDocument("active", wire.EffectAllow))
	srv, cache := newPolicyServer(store)

	before, err := cache.Statements(context.Background(), acme, nil)
	require.NoError(t, err)
	require.Len(t, before, 1)

	_, err = srv.ArchivePolicy(context.Background(), &authzv1.ArchivePolicyRequest{PolicyGrn: g.String()})
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
	store := newFakeStore()
	g := store.seed(t, orgGRN("acme"), wire.PolicyStatusActive, policyDocument("active", wire.EffectAllow))
	srv, _ := newPolicyServer(store)

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
	store := newFakeStore()
	g := store.seed(t, orgGRN("acme"), wire.PolicyStatusDraft, policyDocument("original", wire.EffectAllow))
	srv, _ := newPolicyServer(store)

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
				PolicyGrn: g.String(),
				Version:   c.version,
			})
			assert.Equal(t, c.want, status.Code(err))
		})
	}
}

func TestPolicyGRPCServer_AttachInvalidatesTheOrgCache(t *testing.T) {
	acme := orgGRN("acme")
	ada := subjectGRN("ada")
	store := newFakeStore()
	g := store.seed(t, acme, wire.PolicyStatusActive, policyDocument("ada-only", wire.EffectAllow))
	srv, cache := newPolicyServer(store)

	before, err := cache.Statements(context.Background(), acme, []grn.GRN{subjectGRN("bob")})
	require.NoError(t, err)
	require.Len(t, before, 1, "unattached, the policy reaches everyone")

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
	ada, bob := subjectGRN("ada"), subjectGRN("bob")
	store := newFakeStore()
	g := store.seed(t, orgGRN("acme"), wire.PolicyStatusDraft, policyDocument("draft", wire.EffectAllow), ada)
	srv, _ := newPolicyServer(store)

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
// The detach of the other attachment is the control — the same call
// over the same policy, made first while it is not the last — so a
// detach that refuses whatever it is given stops satisfying the first
// row. The refused attachment is still stored.
func TestPolicyGRPCServer_DetachRefusesTheLastAttachment(t *testing.T) {
	ada, bob := subjectGRN("ada"), subjectGRN("bob")
	store := newFakeStore()
	g := store.seed(t, orgGRN("acme"), wire.PolicyStatusActive, policyDocument("attached", wire.EffectAllow), ada, bob)
	srv, _ := newPolicyServer(store)

	// In order: the second detach is refused because the first succeeded.
	cases := []struct {
		name    string
		subject grn.GRN
		want    codes.Code
	}{
		{"an attachment that is not the last", bob, codes.OK},
		{"the last one", ada, codes.FailedPrecondition},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := srv.DetachPolicy(context.Background(), &authzv1.DetachPolicyRequest{
				PolicyGrn:  g.String(),
				SubjectGrn: c.subject.String(),
			})
			assert.Equal(t, c.want, status.Code(err))
		})
	}

	left, err := store.ListPolicyAttachments(context.Background(), g)
	require.NoError(t, err)
	require.Len(t, left, 1)
	assert.Equal(t, ada, left[0].Subject())
}

// TestPolicyGRPCServer_AttachmentRejectsBadGRN reads the attach whose
// two GRNs both parse against the two that spoil one of them: an
// attach that refuses whatever it is given stops satisfying the first
// row while still satisfying the other two.
func TestPolicyGRPCServer_AttachmentRejectsBadGRN(t *testing.T) {
	ada := subjectGRN("ada")
	store := newFakeStore()
	g := store.seed(t, orgGRN("acme"), wire.PolicyStatusDraft, policyDocument("draft", wire.EffectAllow))
	srv, _ := newPolicyServer(store)

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

// TestPolicyGRPCServer_ListPolicyAttachments reads each entry of the
// response against the stored attachment it stands for. The two name
// one policy and two different subjects at two different times, so an
// entry that carries its neighbour's subject, its own policy in place
// of its subject, or nothing at all stops matching.
func TestPolicyGRPCServer_ListPolicyAttachments(t *testing.T) {
	store := newFakeStore()
	g := store.seed(t, orgGRN("acme"), wire.PolicyStatusDraft, policyDocument("draft", wire.EffectAllow), subjectGRN("ada"), subjectGRN("bob"))
	stored, err := store.ListPolicyAttachments(context.Background(), g)
	require.NoError(t, err)
	srv, _ := newPolicyServer(store)

	resp, err := srv.ListPolicyAttachments(context.Background(), &authzv1.ListPolicyAttachmentsRequest{
		PolicyGrn: g.String(),
	})
	require.NoError(t, err)
	require.Len(t, resp.GetAttachments(), 2)
	for i, got := range resp.GetAttachments() {
		assert.Equal(t, g.String(), got.GetPolicyGrn())
		assert.Equal(t, stored[i].Subject().String(), got.GetSubjectGrn())
		assert.Equal(t, stored[i].CreatedAt(), got.GetCreatedAt().AsTime())
	}
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
