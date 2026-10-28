package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
	"github.com/garamsh/garam/platform/internal/wire"
)

const policyDocument = `{"Version":"2026-07-25","Statement":[{"Sid":"AllowOrgRead","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`

// policyFixture stubs the PolicyManager seam with the policy the
// handlers address, and returns its GRN.
func policyFixture(orgSlug string) (grn.GRN, *api.Policy) {
	orgGRN := grn.Org(orgSlug)
	g := grn.New(orgSlug, "default", "policy", "9f2ac1b40d8e7a35")
	at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	return g, &api.Policy{
		GRN:            g,
		OrgGRN:         orgGRN,
		Name:           "read-only",
		Status:         wire.PolicyStatusDraft,
		DefaultVersion: 1,
		CreatedAt:      at,
		UpdatedAt:      at,
	}
}

// policyActions is every action the policy endpoints enforce.
var policyActions = []string{
	org.ActionPolicyCreate,
	org.ActionPolicyRead,
	org.ActionPolicyUpdate,
	org.ActionPolicyAttach,
	org.ActionPolicyDetach,
	org.ActionPolicyDelete,
}

func TestPolicyFlow_AdminManagesLifecycle(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")

	perms := make([]permission, 0, len(policyActions))
	for _, a := range policyActions {
		perms = append(perms, permission{ownerGRN, a})
	}
	f.permit(perms...)

	g, p := policyFixture(slug)
	subject := grn.New(slug, "default", org.ServiceTypeGroup, "eng")
	version := &api.PolicyVersion{
		PolicyGRN: g,
		Version:   2,
		Document:  json.RawMessage(policyDocument),
		CreatedBy: ownerGRN,
		CreatedAt: p.CreatedAt,
	}
	attachment := &api.PolicyAttachment{PolicyGRN: g, SubjectGRN: subject, CreatedAt: p.CreatedAt}

	f.policies.EXPECT().
		CreatePolicy(mock.Anything, p.OrgGRN, "read-only", json.RawMessage(policyDocument), ownerGRN).
		Return(p, nil)
	f.policies.EXPECT().ListPolicies(mock.Anything, p.OrgGRN).Return([]*api.Policy{p}, nil)
	f.policies.EXPECT().
		AddPolicyVersion(mock.Anything, g, json.RawMessage(policyDocument), ownerGRN).
		Return(version, nil)
	f.policies.EXPECT().ListPolicyVersions(mock.Anything, g).Return([]*api.PolicyVersion{version}, nil)
	f.policies.EXPECT().ActivatePolicy(mock.Anything, g).Return(p, nil)
	f.policies.EXPECT().RollbackPolicy(mock.Anything, g, int32(1)).Return(p, nil)
	f.policies.EXPECT().AttachPolicy(mock.Anything, g, subject).Return(attachment, nil)
	f.policies.EXPECT().ListPolicyAttachments(mock.Anything, g).Return([]*api.PolicyAttachment{attachment}, nil)
	f.policies.EXPECT().DetachPolicy(mock.Anything, g, subject).Return(nil)
	f.policies.EXPECT().DeletePolicy(mock.Anything, g).Return(nil)

	base := "/orgs/" + slug + "/policies"
	rec := f.do(t, "POST", base, `{"name":"read-only","document":`+policyDocument+`}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	created := decodeBody[apigen.Policy](t, rec)
	assert.Equal(t, g.String(), created.Grn)
	assert.Equal(t, apigen.Draft, created.Status)
	assert.Equal(t, 1, created.DefaultVersion)

	rec = f.do(t, "GET", base, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeBody[[]apigen.Policy](t, rec), 1)

	policyPath := base + "/" + g.Identifier
	rec = f.do(t, "POST", policyPath+"/versions", `{"document":`+policyDocument+`}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	addedVersion := decodeBody[apigen.PolicyVersion](t, rec)
	assert.Equal(t, 2, addedVersion.Version)
	assert.JSONEq(t, policyDocument, string(addedVersion.Document))

	rec = f.do(t, "GET", policyPath+"/versions", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeBody[[]apigen.PolicyVersion](t, rec), 1)

	rec = f.do(t, "POST", policyPath+"/activate", "", owner)
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "POST", policyPath+"/rollback", `{"version":1}`, owner)
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "POST", policyPath+"/attachments", `{"subjectGrn":"`+subject.String()+`"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, subject.String(), decodeBody[apigen.PolicyAttachment](t, rec).SubjectGrn)

	rec = f.do(t, "GET", policyPath+"/attachments", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeBody[[]apigen.PolicyAttachment](t, rec), 1)

	rec = f.do(t, "DELETE", policyPath+"/attachments/"+subject.String(), "", owner)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = f.do(t, "DELETE", policyPath, "", owner)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestPolicies_MemberIsDenied(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	orgGRN := grn.Org(slug)

	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	memberGRN := grn.New("root", "default", user.ServiceType, "member-sub")
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))
	// The member role grants no policy action.
	f.permit(permission{ownerGRN, org.ActionPolicyCreate})

	g, p := policyFixture(slug)
	base := "/orgs/" + slug + "/policies"
	policyPath := base + "/" + g.Identifier

	// The owner holds ActionPolicyCreate and takes the same route the
	// first row below is refused on: what stops being accepted when the
	// verdict stops being asked for.
	f.policies.EXPECT().
		CreatePolicy(mock.Anything, orgGRN, "read-only", json.RawMessage(policyDocument), ownerGRN).
		Return(p, nil)
	rec := f.do(t, "POST", base, `{"name":"read-only","document":`+policyDocument+`}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)

	for _, tc := range []struct{ method, path, body string }{
		{"POST", base, `{"name":"read-only","document":` + policyDocument + `}`},
		{"GET", base, ""},
		{"GET", policyPath + "/versions", ""},
		{"POST", policyPath + "/versions", `{"document":` + policyDocument + `}`},
		{"POST", policyPath + "/activate", ""},
		{"DELETE", policyPath, ""},
		{"POST", policyPath + "/rollback", `{"version":1}`},
		{"GET", policyPath + "/attachments", ""},
		{"POST", policyPath + "/attachments", `{"subjectGrn":"grn:acme:default:group:eng"}`},
		{"DELETE", policyPath + "/attachments/grn:acme:default:group:eng", ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := f.do(t, tc.method, tc.path, tc.body, member)
			assert.Equal(t, http.StatusForbidden, rec.Code)
		})
	}
}

// TestPolicies_RequireOrgMembership reads one route's three answers
// together, the way TestGroups_RequireOrgMembership reads the group
// listing's: the owner is a member holding the action, and neither
// refusal below is his. Without the first, a listing refused to
// everyone satisfies the other two.
func TestPolicies_RequireOrgMembership(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	orgGRN := grn.Org(slug)
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionPolicyRead})
	f.policies.EXPECT().ListPolicies(mock.Anything, orgGRN).Return(nil, nil)

	path := "/orgs/" + slug + "/policies"
	rec := f.do(t, "GET", path, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "GET", path, "", outsider)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "permission_denied", decodeBody[apigen.Error](t, rec).Kind)

	rec = f.do(t, "GET", path, "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestCreatePolicy_RejectsIncompleteBody reads the complete body
// against the three that are not: each of those is that body with one
// part taken away, so a create that refuses everything stops satisfying
// the first row while still satisfying the rest.
func TestCreatePolicy_RejectsIncompleteBody(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionPolicyCreate})
	_, p := policyFixture(slug)
	f.policies.EXPECT().
		CreatePolicy(mock.Anything, p.OrgGRN, "read-only", json.RawMessage(policyDocument), ownerGRN).
		Return(p, nil).Once()

	for name, c := range map[string]struct {
		body string
		want int
	}{
		"complete":         {`{"name":"read-only","document":` + policyDocument + `}`, http.StatusCreated},
		"missing name":     {`{"document":` + policyDocument + `}`, http.StatusBadRequest},
		"missing document": {`{"name":"read-only"}`, http.StatusBadRequest},
		"malformed json":   {`{"name":`, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(t, "POST", "/orgs/"+slug+"/policies", c.body, owner)
			assert.Equal(t, c.want, rec.Code)
		})
	}
}

func TestCreatePolicy_RejectsDocumentTheEngineRefuses(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionPolicyCreate})

	f.policies.EXPECT().
		CreatePolicy(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errorx.New(errorx.KindInvalidArgument, "policy document: Version is required"))

	rec := f.do(t, "POST", "/orgs/"+slug+"/policies", `{"name":"read-only","document":{"Statement":[]}}`, owner)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, errorx.KindInvalidArgument.String(), decodeBody[apigen.Error](t, rec).Kind)
}

// TestAttachPolicy_RejectsMalformedSubject reads the subject GRN that
// parses against the one that does not, on both halves of the pair: the
// well-formed subject attaches and detaches through the same routes, so
// the two refusals track the string rather than a surface that refuses
// every attachment.
func TestAttachPolicy_RejectsMalformedSubject(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(
		permission{ownerGRN, org.ActionPolicyAttach},
		permission{ownerGRN, org.ActionPolicyDetach},
	)

	g, p := policyFixture(slug)
	subject := grn.New(slug, "default", org.ServiceTypeGroup, "eng")
	policyPath := "/orgs/" + slug + "/policies/" + g.Identifier
	f.policies.EXPECT().AttachPolicy(mock.Anything, g, subject).
		Return(&api.PolicyAttachment{PolicyGRN: g, SubjectGRN: subject, CreatedAt: p.CreatedAt}, nil).Once()
	f.policies.EXPECT().DetachPolicy(mock.Anything, g, subject).Return(nil).Once()

	rec := f.do(t, "POST", policyPath+"/attachments", `{"subjectGrn":"`+subject.String()+`"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	rec = f.do(t, "DELETE", policyPath+"/attachments/"+subject.String(), "", owner)
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = f.do(t, "POST", policyPath+"/attachments", `{"subjectGrn":"eng"}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = f.do(t, "DELETE", policyPath+"/attachments/eng", "", owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestPolicy_NotFoundFromEngine holds the kind the engine answered
// against the one it did not: the same route over the same policy,
// differing only in what the seam returned, so the 404 tracks the
// engine's answer rather than a route that reports missing whatever it
// is asked for.
func TestPolicy_NotFoundFromEngine(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionPolicyUpdate})

	g, p := policyFixture(slug)
	activate := "/orgs/" + slug + "/policies/" + g.Identifier + "/activate"

	f.policies.EXPECT().ActivatePolicy(mock.Anything, g).Return(p, nil).Once()
	rec := f.do(t, "POST", activate, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)

	f.policies.EXPECT().ActivatePolicy(mock.Anything, g).
		Return(nil, errorx.New(errorx.KindNotFound, "policy not found")).Once()
	rec = f.do(t, "POST", activate, "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestRollbackPolicy_RefusesAVersionTheEngineCannotHold holds the
// refusal against the version that must still be accepted: the
// specification's `version` is what the engine stores in an int32
// column, and one wider than that used to be narrowed on the way to the
// seam — 2^32+1 arriving as 1, which is an existing version and a
// rollback that was never asked for. The control is version 1 itself,
// which stops being accepted if the boundary refuses more than the
// width; the call count is what says the wide one was refused rather
// than performed under another number.
func TestRollbackPolicy_RefusesAVersionTheEngineCannotHold(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionPolicyUpdate})

	g, p := policyFixture(slug)
	f.policies.EXPECT().RollbackPolicy(mock.Anything, g, mock.Anything).Return(p, nil)

	path := "/orgs/" + slug + "/policies/" + g.Identifier + "/rollback"

	rec := f.do(t, "POST", path, `{"version":1}`, owner)
	require.Equal(t, http.StatusOK, rec.Code, "a version the engine holds is the control")

	for _, version := range []string{"2147483648", "4294967297", "-2147483649"} {
		rec = f.do(t, "POST", path, `{"version":`+version+`}`, owner)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "version %s", version)
		assert.NotContains(t, decodeBody[apigen.Error](t, rec).Message, version,
			"the refusal does not repeat what was submitted")
	}
	f.policies.AssertNumberOfCalls(t, "RollbackPolicy", 1)
}
