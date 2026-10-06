package api_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// The preview answers what deleting the organization would take. Each
// kind is given a different number of rows, so a field wired to the
// wrong count fails here rather than reading plausibly: nine integers
// of the same type are exactly what a mix-up hides in.
func TestGetOrgDeletionPreview_CountsEachKind(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	ownerGRN := userGRNOf("owner-sub")
	orgGRN := orgGRNOf(slug)
	f.permit(permission{ownerGRN, org.ActionOrgDelete})

	ctx := testCtx(t)
	// 1 group, 2 projects, 3 invitations. Creating the org already
	// made the owner a member, so members reaches 2 with one more.
	require.NoError(t, f.orgs.CreateGroup(ctx, org.NewGroup(
		grn.New("root", "default", org.ServiceTypeGroup, "g1"), "g1", orgGRN)))
	for _, id := range []string{"p1", "p2"} {
		require.NoError(t, f.orgs.CreateProject(ctx, org.NewProject(
			grn.New("root", "default", org.ServiceTypeProject, id), id, orgGRN)))
	}
	for _, tok := range []string{"t1", "t2", "t3"} {
		require.NoError(t, f.orgs.CreateInvitation(ctx, org.NewInvitation(
			tok, orgGRN, org.RoleMember, ownerGRN, time.Now().Add(time.Hour))))
	}
	memberGRN := userGRNOf("member-sub")
	require.NoError(t, f.orgs.AddMember(ctx, org.NewMembership(orgGRN, memberGRN, org.RoleMember)))

	// 4 agents, 5 definitions, 6 operators.
	for range 4 {
		g, err := agent.NewGRN(orgGRN)
		require.NoError(t, err)
		a, err := agent.New(g, orgGRN)
		require.NoError(t, err)
		require.NoError(t, f.agents.Create(ctx, a))
	}
	operatorGRN := registerOperator(t, f, slug, "op-defs")
	for range 5 {
		d, err := agent.NewDefinition(orgGRN, operatorGRN, "Field agent", agent.Values{}, ownerGRN)
		require.NoError(t, err)
		f.agents.keepDefinition(d)
	}
	// One operator is already registered above for the definitions to
	// name, so five more make six.
	for _, id := range []string{"o1", "o2", "o3", "o4", "o5"} {
		registerOperator(t, f, slug, id)
	}

	// 7 consent relations, each with a different counterpart.
	for _, other := range []string{"b1", "b2", "b3", "b4", "b5", "b6", "b7"} {
		_, err := f.consents.Grant(ctx, orgGRN,
			orgGRNOf(other))
		require.NoError(t, err)
	}

	// 9 agent templates, kept from before they were retired.
	for i := range 9 {
		f.agents.keepTemplate(retiredTemplate(slug, fmt.Sprintf("%016x", i), "Researcher"))
	}

	// 8 policies. The engine owns them and returns the whole list.
	f.policies.EXPECT().ListPolicies(mock.Anything, orgGRN).
		Return(make([]*api.Policy, 8), nil).Once()

	rec := f.do(t, "GET", "/orgs/"+slug+"/deletion-preview", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody[apigen.OrgDeletionPreview](t, rec)

	assert.Equal(t, 1, body.Counts.Groups)
	assert.Equal(t, 2, body.Counts.Projects)
	assert.Equal(t, 2, body.Counts.Members)
	assert.Equal(t, 3, body.Counts.Invitations)
	assert.Equal(t, 8, body.Counts.Policies)
	assert.Equal(t, 4, body.Counts.Agents)
	assert.Equal(t, 5, body.Counts.AgentDefinitions)
	assert.Equal(t, 9, body.Counts.AgentTemplates)
	assert.Equal(t, 6, body.Counts.Operators)
	assert.Equal(t, 7, body.Counts.Consents)
}

// The certificate authority is reported for what it is: an
// organization that has needed no certificate has none, and one that
// has holds one. Both cases are asserted together, because a field
// hard-coded either way passes one of them on its own.
func TestGetOrgDeletionPreview_CertificateAuthority(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	ownerGRN := userGRNOf("owner-sub")
	f.permit(permission{ownerGRN, org.ActionOrgDelete})

	withCA := createOrg(t, f, owner, "acme")
	withoutCA := createOrg(t, f, owner, "globex")
	f.authorities.give(orgGRNOf(withCA))
	f.policies.EXPECT().ListPolicies(mock.Anything, mock.Anything).
		Return(nil, nil).Twice()

	rec := f.do(t, "GET", "/orgs/"+withCA+"/deletion-preview", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, decodeBody[apigen.OrgDeletionPreview](t, rec).CertificateAuthority,
		"an organization holding an authority loses it")

	rec = f.do(t, "GET", "/orgs/"+withoutCA+"/deletion-preview", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, decodeBody[apigen.OrgDeletionPreview](t, rec).CertificateAuthority,
		"an organization that has needed no certificate has no authority to lose")
}

// The preview is the delete's own permission, so a member who may read
// the organization still may not read what deleting it would take. The
// owner's 200 is the control: without it the 403 would hold for any
// reason at all, a missing org among them.
func TestGetOrgDeletionPreview_OwnerOnly(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	member := f.sessionCookieFor(t, "member-sub", "Member")
	slug := createOrg(t, f, owner, "acme")
	orgGRN := orgGRNOf(slug)
	ownerGRN := userGRNOf("owner-sub")
	memberGRN := userGRNOf("member-sub")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRN, memberGRN, org.RoleMember)))
	f.permit(
		permission{ownerGRN, org.ActionOrgDelete},
		permission{memberGRN, org.ActionOrgRead},
		permission{memberGRN, org.ActionOrgMemberList},
	)

	rec := f.do(t, "GET", "/orgs/"+slug+"/deletion-preview", "", member)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "permission_denied", decodeBody[apigen.Error](t, rec).Kind)

	// The member can read the organization itself, so what refused the
	// preview is the delete permission and not the membership.
	rec = f.do(t, "GET", "/orgs/"+slug, "", member)
	require.Equal(t, http.StatusOK, rec.Code)

	f.policies.EXPECT().ListPolicies(mock.Anything, orgGRN).Return(nil, nil).Once()
	rec = f.do(t, "GET", "/orgs/"+slug+"/deletion-preview", "", owner)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// An organization that does not exist is refused rather than answered
// with zeros. Zeros would read as an organization holding nothing,
// which is a different fact from there being no organization. The
// refusal is 403 and not 404 because membership is the precondition
// and a missing organization has no members, which is the answer a
// non-member gets too (ADR-0011).
//
// The organization that does exist is the control: it is previewed by
// the same caller in the same test, so the refusal above is the
// missing organization and not a permission the fixture never granted.
func TestGetOrgDeletionPreview_MissingOrgIsNotZero(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	ownerGRN := userGRNOf("owner-sub")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{ownerGRN, org.ActionOrgDelete})

	rec := f.do(t, "GET", "/orgs/nosuchorg/deletion-preview", "", owner)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, "permission_denied", decodeBody[apigen.Error](t, rec).Kind)

	f.policies.EXPECT().ListPolicies(mock.Anything, orgGRNOf(slug)).Return(nil, nil).Once()
	rec = f.do(t, "GET", "/orgs/"+slug+"/deletion-preview", "", owner)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// A zero in the preview means the organization holds none of that
// kind, and never that the caller may not see them. The delete does
// not consult a per-kind permission — the cascade takes every row
// whatever the caller could list — so a preview that filtered by one
// would under-report what the deletion destroys, which is the failure
// this operation exists to prevent.
//
// The caller here is permitted `org:delete` and nothing else. The
// control is the agents route in the same test: it needs
// `agent:read`, this caller is refused it, and the preview still
// counts the agents. Without that refusal the non-zero count would be
// consistent with the permission simply having been granted.
//
// The empty kind is asserted beside the counted ones, so both meanings
// of zero are read together rather than one standing for both.
func TestGetOrgDeletionPreview_ZeroMeansEmptyNotUnauthorized(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	orgGRN := orgGRNOf(slug)
	ownerGRN := userGRNOf("owner-sub")
	// Only the delete. No agent:read, no operator:read, no consent read.
	f.permit(permission{ownerGRN, org.ActionOrgDelete})

	ctx := testCtx(t)
	operatorGRN := registerOperator(t, f, slug, "op-unseen")
	agentGRN, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(agentGRN, orgGRN)
	require.NoError(t, err)
	require.NoError(t, f.agents.Create(ctx, a))
	d, err := agent.NewDefinition(orgGRN, operatorGRN, "Field agent", agent.Values{}, ownerGRN)
	require.NoError(t, err)
	f.agents.keepDefinition(d)
	_, err = f.consents.Grant(ctx, orgGRN, orgGRNOf("elsewhere"))
	require.NoError(t, err)

	// The control: this caller may not list agents.
	rec := f.do(t, "GET", "/orgs/"+slug+"/agents", "", owner)
	require.Equal(t, http.StatusForbidden, rec.Code,
		"the caller must genuinely lack agent:read for the count below to prove anything")

	f.policies.EXPECT().ListPolicies(mock.Anything, orgGRN).Return(nil, nil).Once()
	rec = f.do(t, "GET", "/orgs/"+slug+"/deletion-preview", "", owner)
	require.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody[apigen.OrgDeletionPreview](t, rec)

	// Counted although the caller may not list them: the delete takes
	// them regardless, so the preview names them regardless.
	assert.Equal(t, 1, body.Counts.Agents, "agents are counted without agent:read")
	assert.Equal(t, 1, body.Counts.AgentDefinitions)
	assert.Equal(t, 1, body.Counts.Operators)
	assert.Equal(t, 1, body.Counts.Consents)

	// A real zero, from an organization that holds none of these.
	assert.Zero(t, body.Counts.Groups, "zero here is nothing-there")
	assert.Zero(t, body.Counts.Projects)
	assert.Zero(t, body.Counts.Invitations)
	assert.Zero(t, body.Counts.Policies)
}
