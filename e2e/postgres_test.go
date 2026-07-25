// Package e2e_test exercises the Postgres store implementations
// against a real database. Set GAON_TEST_DATABASE_URL (e.g. via
// `make e2e`, which starts the docker-compose Postgres) to run them;
// without it every test skips.
package e2e_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/authn"
	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
)

var migrateOnce sync.Once

// pool connects to the test database or skips the test.
func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("GAON_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("GAON_TEST_DATABASE_URL not set; skipping e2e")
	}
	migrateOnce.Do(func() {
		m, err := migrate.New("file://../migrations", url)
		require.NoError(t, err)
		err = m.Up()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			require.NoError(t, err)
		}
	})
	p, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	truncate(t, p)
	return p
}

// truncate resets every table so tests are independent.
func truncate(t *testing.T, p *pgxpool.Pool) {
	t.Helper()
	_, err := p.Exec(context.Background(), `TRUNCATE users, orgs, groups, projects,
		group_members, project_members, org_members, invitations, sessions CASCADE`)
	require.NoError(t, err)
}

func grnOf(serviceType, id string) grn.GRN {
	return grn.New("garam", "default", serviceType, id)
}

func TestUserStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := user.NewPostgresStore(pool(t))

	g := grnOf("user", "ada")
	u := user.New(g, "ada")
	require.NoError(t, s.Create(ctx, u))

	got, err := s.Get(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, g, got.GRN())
	assert.Equal(t, "ada", got.Nickname())

	_, err = s.Get(ctx, grnOf("user", "nobody"))
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))

	require.NoError(t, s.Create(ctx, user.New(grnOf("user", "bob"), "bob")))
	list, err := s.List(ctx, user.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, list, 2)
	list, err = s.List(ctx, user.ListFilter{Limit: 1, Offset: 1})
	require.NoError(t, err)
	assert.Len(t, list, 1)

	updated, err := s.Update(ctx, g, user.Patch{
		Nickname: ptr("ada2"),
		Roles:    []string{"admin"},
	})
	require.NoError(t, err)
	assert.Equal(t, "ada2", updated.Nickname())
	assert.Equal(t, []string{"admin"}, updated.Roles())

	orgGRN := grnOf("org", "acme")
	require.NoError(t, s.AddOrganization(ctx, g, orgGRN))
	got, err = s.Get(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{orgGRN}, got.Organizations())
	require.NoError(t, s.RemoveOrganization(ctx, g, orgGRN))
	got, err = s.Get(ctx, g)
	require.NoError(t, err)
	assert.Empty(t, got.Organizations())

	require.NoError(t, s.Delete(ctx, g))
	_, err = s.Get(ctx, g)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	assert.True(t, errorx.IsKind(s.Delete(ctx, g), errorx.KindNotFound))
}

func TestOrgStore_RoundTripWithCascade(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := org.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")

	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	got, err := s.GetOrg(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, "acme", got.Name())

	renamed, err := s.UpdateOrg(ctx, orgGRN, org.Patch{Name: ptr("acme2")})
	require.NoError(t, err)
	assert.Equal(t, "acme2", renamed.Name())

	groupGRN := grnOf("group", "eng")
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(groupGRN, "eng", orgGRN)))
	projectGRN := grnOf("project", "web")
	require.NoError(t, s.CreateProject(ctx, org.NewProject(projectGRN, "web", orgGRN)))

	userGRN := grnOf("user", "ada")
	require.NoError(t, user.NewPostgresStore(p).Create(ctx, user.New(userGRN, "ada")))
	require.NoError(t, s.AddGroupMember(ctx, groupGRN, userGRN))
	require.NoError(t, s.AddProjectMember(ctx, projectGRN, userGRN))

	members, err := s.ListGroupMembers(ctx, groupGRN)
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{userGRN}, members)

	groups, err := s.ListGroups(ctx, org.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, groups, 1)
	assert.Equal(t, orgGRN, groups[0].Organization())

	// Deleting the org cascades to groups and projects.
	require.NoError(t, s.DeleteOrg(ctx, orgGRN))
	_, err = s.GetGroup(ctx, groupGRN)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	_, err = s.GetProject(ctx, projectGRN)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
}

func TestOrgStore_Membership(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := org.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")
	userGRN := grnOf("user", "ada")

	require.NoError(t, user.NewPostgresStore(p).Create(ctx, user.New(userGRN, "ada")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	m := org.NewMembership(orgGRN, userGRN, org.RoleOwner)
	require.NoError(t, s.AddMember(ctx, m))
	assert.True(t, errorx.IsKind(s.AddMember(ctx, m), errorx.KindAlreadyExists))

	got, err := s.GetMember(ctx, orgGRN, userGRN)
	require.NoError(t, err)
	assert.Equal(t, org.RoleOwner, got.Role())

	_, err = s.GetMember(ctx, orgGRN, grnOf("user", "nobody"))
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))

	members, err := s.ListMembers(ctx, orgGRN)
	require.NoError(t, err)
	assert.Len(t, members, 1)

	orgs, err := s.ListOrgsByMember(ctx, userGRN, org.ListFilter{})
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, orgGRN, orgs[0].GRN())

	require.NoError(t, s.RemoveMember(ctx, orgGRN, userGRN))
	members, err = s.ListMembers(ctx, orgGRN)
	require.NoError(t, err)
	assert.Empty(t, members)
}

func TestOrgStore_InvitationLifecycle(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := org.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")
	owner := grnOf("user", "ada")
	invitee := grnOf("user", "bob")

	users := user.NewPostgresStore(p)
	require.NoError(t, users.Create(ctx, user.New(owner, "ada")))
	require.NoError(t, users.Create(ctx, user.New(invitee, "bob")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	// timestamptz stores microseconds; truncate for exact comparison.
	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	inv := org.NewInvitation("tok-1", orgGRN, org.RoleAdmin, owner, expires)
	require.NoError(t, s.CreateInvitation(ctx, inv))

	got, err := s.GetInvitation(ctx, "tok-1")
	require.NoError(t, err)
	assert.False(t, got.Accepted())
	assert.Equal(t, org.RoleAdmin, got.Role())
	assert.True(t, expires.Equal(got.ExpiresAt()))

	_, err = s.GetInvitation(ctx, "nope")
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))

	accepted, err := s.AcceptInvitation(ctx, "tok-1", invitee)
	require.NoError(t, err)
	assert.True(t, accepted.Accepted())
	assert.Equal(t, invitee, accepted.AcceptedBy())

	_, err = s.AcceptInvitation(ctx, "tok-1", invitee)
	assert.ErrorIs(t, err, org.ErrInvitationAccepted)
	_, err = s.AcceptInvitation(ctx, "nope", invitee)
	assert.ErrorIs(t, err, org.ErrInvitationNotFound)
}

func TestSessionStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	users := user.NewPostgresStore(p)
	s := authn.NewPostgresSessionStore(p)
	userGRN := grnOf("user", "ada")
	require.NoError(t, users.Create(ctx, user.New(userGRN, "ada")))

	token, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, token, authn.NewSession(userGRN, time.Hour)))

	got, err := s.Get(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, userGRN, got.User())

	_, err = s.Get(ctx, "unknown-token")
	assert.ErrorIs(t, err, authn.ErrSessionNotFound)

	// Expired sessions behave as absent.
	stale, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, stale, authn.NewSession(userGRN, -time.Hour)))
	_, err = s.Get(ctx, stale)
	assert.ErrorIs(t, err, authn.ErrSessionNotFound)

	require.NoError(t, s.Delete(ctx, token))
	_, err = s.Get(ctx, token)
	assert.ErrorIs(t, err, authn.ErrSessionNotFound)
	// Delete is idempotent.
	require.NoError(t, s.Delete(ctx, token))
}

func ptr[T any](v T) *T { return &v }
