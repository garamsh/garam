package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

const policyDocument = `{"Version":"2026-07-25","Statement":[{"Sid":"AllowOrgRead","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`

// policyFixture stubs the PolicyManager seam with the policy the
// handlers address, and returns its GRN.
func policyFixture(orgSlug string) (grn.GRN, *api.Policy) {
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, orgSlug)
	g := grn.New(orgSlug, "default", "policy", "9f2ac1b40d8e7a35")
	at := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	return g, &api.Policy{
		GRN:            g,
		OrgGRN:         orgGRN,
		Name:           "read-only",
		Status:         api.PolicyStatusDraft,
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
	f.policies.EXPECT().RollbackPolicy(mock.Anything, g, 1).Return(p, nil)
	f.policies.EXPECT().ArchivePolicy(mock.Anything, g).Return(p, nil)
	f.policies.EXPECT().AttachPolicy(mock.Anything, g, subject).Return(attachment, nil)
	f.policies.EXPECT().ListPolicyAttachments(mock.Anything, g).Return([]*api.PolicyAttachment{attachment}, nil)
	f.policies.EXPECT().DetachPolicy(mock.Anything, g, subject).Return(nil)

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

	rec = f.do(t, "POST", policyPath+"/archive", "", owner)
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "POST", policyPath+"/attachments", `{"subjectGrn":"`+subject.String()+`"}`, owner)
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, subject.String(), decodeBody[apigen.PolicyAttachment](t, rec).SubjectGrn)

	rec = f.do(t, "GET", policyPath+"/attachments", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeBody[[]apigen.PolicyAttachment](t, rec), 1)

	rec = f.do(t, "DELETE", policyPath+"/attachments/"+subject.String(), "", owner)
	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestPolicies_MemberIsDenied(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)

	memberGRN := grn.New("root", "default", user.ServiceType, "member-sub")
	member := f.sessionCookieFor(t, "member-sub", "Member")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))
	// The member role grants no policy action.
	f.permit(permission{grn.New("root", "default", user.ServiceType, "owner-sub"), org.ActionPolicyCreate})

	g, _ := policyFixture(slug)
	base := "/orgs/" + slug + "/policies"
	policyPath := base + "/" + g.Identifier
	for _, tc := range []struct{ method, path, body string }{
		{"POST", base, `{"name":"read-only","document":` + policyDocument + `}`},
		{"GET", base, ""},
		{"GET", policyPath + "/versions", ""},
		{"POST", policyPath + "/versions", `{"document":` + policyDocument + `}`},
		{"POST", policyPath + "/activate", ""},
		{"POST", policyPath + "/archive", ""},
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

func TestPolicies_RequireOrgMembership(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")

	rec := f.do(t, "GET", "/orgs/"+slug+"/policies", "", outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestPolicies_RequireSession(t *testing.T) {
	f := newFixture(t)
	rec := f.do(t, "GET", "/orgs/acme/policies", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCreatePolicy_RejectsIncompleteBody(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionPolicyCreate})

	for name, body := range map[string]string{
		"missing name":     `{"document":` + policyDocument + `}`,
		"missing document": `{"name":"read-only"}`,
		"malformed json":   `{"name":`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.do(t, "POST", "/orgs/"+slug+"/policies", body, owner)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
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

func TestAttachPolicy_RejectsMalformedSubject(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(
		permission{ownerGRN, org.ActionPolicyAttach},
		permission{ownerGRN, org.ActionPolicyDetach},
	)

	g, _ := policyFixture(slug)
	policyPath := "/orgs/" + slug + "/policies/" + g.Identifier

	rec := f.do(t, "POST", policyPath+"/attachments", `{"subjectGrn":"eng"}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = f.do(t, "DELETE", policyPath+"/attachments/eng", "", owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPolicy_NotFoundFromEngine(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{ownerGRN, org.ActionPolicyUpdate})

	g, _ := policyFixture(slug)
	f.policies.EXPECT().ActivatePolicy(mock.Anything, g).
		Return(nil, errorx.New(errorx.KindNotFound, "policy not found"))

	rec := f.do(t, "POST", "/orgs/"+slug+"/policies/"+g.Identifier+"/activate", "", owner)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
