package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	clusterpg "github.com/garamsh/garam/platform/internal/cluster/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
)

// TestClusterStore_RoundTrip covers the record ADR-0184 decision 8
// lists: GRN, organization, name, description, creator and creation
// time. A record follows its organization out.
func TestClusterStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := clusterpg.New(p)
	orgs := orgpg.New(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))

	name, description := "prod", "the main one"
	details, err := cluster.NewDetailsPatch(&name, &description)
	require.NoError(t, err)
	createdBy := scopedGRN("acme", "user", "ada")
	one, err := cluster.New(orgRootGRN("acme"), createdBy, details)
	require.NoError(t, err)
	created, err := s.Create(ctx, one)
	require.NoError(t, err)
	assert.Equal(t, one.GRN, created.GRN)

	stored, err := s.Get(ctx, one.GRN)
	require.NoError(t, err)
	assert.Equal(t, one.GRN, stored.GRN)
	assert.Equal(t, orgRootGRN("acme"), stored.Organization)
	assert.Equal(t, createdBy, stored.CreatedBy)
	assert.Equal(t, "prod", stored.Name)
	assert.Equal(t, "the main one", stored.Description)
	assert.False(t, stored.CreatedAt.IsZero())

	elsewhere, err := cluster.New(orgRootGRN("globex"), scopedGRN("globex", "user", "ada"), cluster.DetailsPatch{})
	require.NoError(t, err)
	_, err = s.Create(ctx, elsewhere)
	require.NoError(t, err)
	listed, err := s.ListByOrg(ctx, orgRootGRN("acme"))
	require.NoError(t, err)
	require.Len(t, listed, 1, "this org's clusters and no other org's")
	assert.Equal(t, one.GRN, listed[0].GRN)

	renamed := "renamed"
	patch, err := cluster.NewDetailsPatch(&renamed, nil)
	require.NoError(t, err)
	updated, err := s.UpdateDetails(ctx, one.GRN, patch)
	require.NoError(t, err)
	assert.Equal(t, "renamed", updated.Name)
	assert.Equal(t, "the main one", updated.Description, "a description left out keeps its value")

	require.NoError(t, s.Delete(ctx, one.GRN))
	_, err = s.Get(ctx, one.GRN)
	assert.ErrorIs(t, err, cluster.ErrClusterNotFound)
	assert.ErrorIs(t, s.Delete(ctx, one.GRN), cluster.ErrClusterNotFound)
	_, err = s.UpdateDetails(ctx, one.GRN, patch)
	assert.ErrorIs(t, err, cluster.ErrClusterNotFound)

	// The organization's deletion takes its records; the other
	// organization's record is the control.
	require.NoError(t, orgs.DeleteOrg(ctx, orgRootGRN("globex")))
	_, err = s.Get(ctx, elsewhere.GRN)
	assert.ErrorIs(t, err, cluster.ErrClusterNotFound)
}

// TestClusterStore_RefusesARecordNamingAnotherOrganization holds the
// table's ownership constraint, the copy no call path routes around:
// a GRN of one organization stored under another is refused, and the
// same record under its own organization is the control.
func TestClusterStore_RefusesARecordNamingAnotherOrganization(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := clusterpg.New(p)
	orgs := orgpg.New(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))

	c, err := cluster.New(orgRootGRN("acme"), scopedGRN("acme", "user", "ada"), cluster.DetailsPatch{})
	require.NoError(t, err)
	c.Organization = orgRootGRN("globex")
	_, err = s.Create(ctx, c)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)

	c.Organization = orgRootGRN("acme")
	_, err = s.Create(ctx, c)
	require.NoError(t, err)

	operatorShaped := *c
	operatorShaped.GRN = grn.New("acme", "operator", c.GRN.Identifier)
	_, err = s.Create(ctx, &operatorShaped)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)
}
