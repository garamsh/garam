package integration_test

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
)

// The counts the preview reads are of every row the organization
// holds, not of the page a list would answer with. This is the whole
// reason the counts are on the server: a caller totalling its own list
// is right only while that list is neither paged nor filtered.
//
// The control is the page beside the count. A count that silently
// became a page length still equals the page here, and the assertion
// that the two differ is what tells them apart.
func TestOrgDeletionPreview_CountsRowsNotAPage(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	orgGRN := orgRootGRN("preview-paging")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgGRN, "preview-paging")))
	t.Cleanup(func() { _ = orgs.DeleteOrg(ctx, orgGRN) })

	const groups = 5
	for i := range groups {
		g := grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeGroup,
			"preview-paging-g"+string(rune('a'+i)))
		require.NoError(t, orgs.CreateGroup(ctx, org.NewGroup(g, "g", orgGRN)))
	}

	// A page of two is what a caller reading the first page would
	// total, and it is not the answer.
	page, err := orgs.ListGroups(ctx, orgGRN, listfilter.Filter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page, 2)

	contents, err := orgs.CountOrgContents(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, groups, contents.Groups, "the count is of rows")
	assert.NotEqual(t, len(page), contents.Groups,
		"a count equal to the page is the defect this operation exists to prevent")
}

// The preview describes the cascade rather than a list somebody wrote
// down. Every count is taken before the delete — after it there is no
// before to compare against — and then the organization is deleted and
// each counted kind is shown to be gone.
//
// The control against a count that reads zero for a broken query is
// the same numbers taken while the rows are there: a query that
// answered zero throughout would fail the first half.
func TestOrgDeletionPreview_CountsMatchWhatTheCascadeTakes(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	agents := agent.NewPostgresStore(p)
	operators := operator.NewPostgresStore(p)
	consents := message.NewPostgresStore(p)
	users := userpg.New(p)

	orgGRN := orgRootGRN("preview-cascade")
	otherGRN := orgRootGRN("preview-cascade-other")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgGRN, "cascade")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(otherGRN, "other")))
	t.Cleanup(func() {
		_ = orgs.DeleteOrg(ctx, orgGRN)
		_ = orgs.DeleteOrg(ctx, otherGRN)
	})

	ownerGRN := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "preview-owner")
	require.NoError(t, users.Create(ctx, user.New(ownerGRN, "owner")))
	require.NoError(t, orgs.AddMember(ctx, org.NewMembership(orgGRN, ownerGRN, org.RoleOwner)))

	groupGRN := grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeGroup, "preview-cascade-g")
	require.NoError(t, orgs.CreateGroup(ctx, org.NewGroup(groupGRN, "g", orgGRN)))
	projectGRN := grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeProject, "preview-cascade-p")
	require.NoError(t, orgs.CreateProject(ctx, org.NewProject(projectGRN, "p", orgGRN)))
	require.NoError(t, orgs.CreateInvitation(ctx, org.NewInvitation(
		"preview-cascade-token", orgGRN, org.RoleMember, ownerGRN, time.Now().Add(time.Hour))))

	op, err := operator.New(orgGRN, "preview-op", ownerGRN, operator.DetailsPatch{})
	require.NoError(t, err)
	enrollment, err := operator.NewEnrollment(op.GRN(), ownerGRN)
	require.NoError(t, err)
	_, err = operators.Register(ctx, op, enrollment)
	require.NoError(t, err)

	agentGRN, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(agentGRN, orgGRN)
	require.NoError(t, err)
	require.NoError(t, agents.Create(ctx, a))
	d, err := agent.NewDefinition(orgGRN, op.GRN(), "Field agent", agent.Values{}, ownerGRN)
	require.NoError(t, err)
	keepDefinition(t, p, d)

	_, err = consents.Grant(ctx, orgGRN, otherGRN)
	require.NoError(t, err)

	// Before. Taken while the rows exist, because the delete below
	// leaves nothing to compare against.
	contents, err := orgs.CountOrgContents(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, 1, contents.Groups)
	assert.Equal(t, 1, contents.Projects)
	assert.Equal(t, 1, contents.Members)
	assert.Equal(t, 1, contents.Invitations)

	agentCounts, err := agents.CountByOrg(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, 1, agentCounts.Agents)
	assert.Equal(t, 1, agentCounts.Definitions)

	operatorCount, err := operators.CountByOrg(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, 1, operatorCount)

	consentCount, err := consents.CountByOrg(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, 1, consentCount)

	// After. Everything counted is gone, which is what makes the
	// numbers above a statement about the delete and not a census.
	require.NoError(t, orgs.DeleteOrg(ctx, orgGRN))

	_, err = orgs.GetGroup(ctx, groupGRN)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound), "groups go")
	_, err = orgs.GetProject(ctx, projectGRN)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound), "projects go")
	_, err = orgs.GetInvitation(ctx, "preview-cascade-token")
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound), "invitations go")
	_, err = operators.Get(ctx, op.GRN())
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound), "operators go")
	_, err = agents.Get(ctx, agentGRN)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound), "agents go")

	definitions, err := agents.ListDefinitionsByOrg(ctx, orgGRN)
	require.NoError(t, err)
	assert.Empty(t, definitions, "agent definitions go")

	// The other organization survives, so the cascade took one org's
	// rows rather than every row.
	_, err = orgs.GetOrg(ctx, otherGRN)
	assert.NoError(t, err)
}

// A consent relation is a pair, so deleting one organization removes
// the relation the other organization is still a side of. The other
// organization is not the one being deleted and takes no action, and
// its count falls all the same — which is why the preview names
// consents rather than leaving them to be inferred.
func TestOrgDeletionPreview_ConsentGoesForBothSides(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	consents := message.NewPostgresStore(p)

	deleted := orgRootGRN("preview-consent-deleted")
	survivor := orgRootGRN("preview-consent-survivor")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(deleted, "deleted")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(survivor, "survivor")))
	t.Cleanup(func() {
		_ = orgs.DeleteOrg(ctx, deleted)
		_ = orgs.DeleteOrg(ctx, survivor)
	})

	_, err := consents.Grant(ctx, deleted, survivor)
	require.NoError(t, err)
	_, err = consents.Grant(ctx, survivor, deleted)
	require.NoError(t, err)

	// Before: both sides count the relation, from either direction.
	deletedCount, err := consents.CountByOrg(ctx, deleted)
	require.NoError(t, err)
	require.Equal(t, 1, deletedCount)
	survivorCount, err := consents.CountByOrg(ctx, survivor)
	require.NoError(t, err)
	require.Equal(t, 1, survivorCount)

	require.NoError(t, orgs.DeleteOrg(ctx, deleted))

	// After: the organization that was not deleted has lost it too.
	survivorCount, err = consents.CountByOrg(ctx, survivor)
	require.NoError(t, err)
	assert.Zero(t, survivorCount,
		"deleting one side of a consent pair takes the other side's relation")
}

// The certificate authority is reported only where there is one, and
// deleting the organization takes it with no way back. `internal/ca`
// declares Create and Get and nothing else, so the row that goes here
// cannot be put back by any route in the package.
func TestOrgDeletionPreview_CertificateAuthorityGoesAndDoesNotReturn(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	authorities := capg.New(p)

	withCA := orgRootGRN("preview-ca-holder")
	withoutCA := orgRootGRN("preview-ca-none")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(withCA, "holder")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(withoutCA, "none")))
	t.Cleanup(func() {
		_ = orgs.DeleteOrg(ctx, withCA)
		_ = orgs.DeleteOrg(ctx, withoutCA)
	})

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	svc, err := ca.NewService(authorities, sealer, ca.Config{})
	require.NoError(t, err)

	// An authority is created on the first certificate the org needs,
	// so issuing one is what gives this organization an authority.
	agentGRN, err := agent.NewGRN(withCA)
	require.NoError(t, err)
	_, err = svc.Issue(ctx, agentGRN)
	require.NoError(t, err)

	// Before: one organization holds an authority and the other, which
	// has needed no certificate, does not. The second is the control:
	// a presence check that answered yes for everything would pass the
	// first assertion alone.
	held, err := authorities.Get(ctx, withCA)
	require.NoError(t, err)
	require.NotEmpty(t, held.SealedKey, "the private key is stored sealed and goes with the row")
	_, err = authorities.Get(ctx, withoutCA)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound),
		"an organization that has needed no certificate has no authority")

	require.NoError(t, orgs.DeleteOrg(ctx, withCA))

	// After: the row is gone. ca.Store declares Create and Get, so
	// nothing in the package restores it, and Create needs an org row
	// that the same delete removed.
	_, err = authorities.Get(ctx, withCA)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound),
		"the authority goes with the organization")
}
