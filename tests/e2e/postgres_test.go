// Package e2e_test exercises the Postgres store implementations, and
// the services layered over them, against a real database.
// GARAM_TEST_DATABASE_URL names that database; `make db-up e2e` starts
// the docker-compose Postgres and sets it. The suite fails without it
// rather than skipping.
package e2e_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	"github.com/garamsh/garam/internal/watch"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

var migrateOnce sync.Once

// migrationsDir returns the repo-root `migrations/` directory. It walks
// up from this source file until it finds go.mod, so the suite keeps
// working wherever the test directory is moved to.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "cannot locate the test source file")
	for dir := filepath.Dir(file); ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "migrations")
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no go.mod above %s", file)
		dir = parent
	}
}

// TestMain requires the test database before any test runs. This layer is
// defined by real infrastructure, and skipping instead left `go test`
// printing the same `ok` for a run that touched no database as for one
// that passed against it.
func TestMain(m *testing.M) {
	if os.Getenv("GARAM_TEST_DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "e2e: GARAM_TEST_DATABASE_URL is not set; run `make db-up e2e`")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// pool connects to the test database TestMain checked for.
func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("GARAM_TEST_DATABASE_URL")
	migrateOnce.Do(func() {
		m, err := migrate.New("file://"+migrationsDir(t), url)
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
		group_members, project_members, org_members, invitations, sessions,
		oauth_states, policies, policy_versions, policy_attachments, agents,
		agent_assignments, agent_definitions, certificate_authorities,
		messages, agent_watches CASCADE`)
	require.NoError(t, err)
}

// countRows answers a count query against the test database. A store
// that removes a row is asserted on the table: a getter that filters by
// expiry answers the same for a row removed and a row ignored.
func countRows(t *testing.T, p *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, p.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

func grnOf(serviceType, id string) grn.GRN {
	owner := "acme"
	if serviceType == "org" {
		owner = "root"
	}
	return grn.New(owner, "default", serviceType, id)
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

	// Many GRNs, one query. GRNs without a user are absent, not an error.
	byGRN, err := s.ListByGRNs(ctx, []grn.GRN{g, grnOf("user", "nobody")})
	require.NoError(t, err)
	require.Len(t, byGRN, 1)
	assert.Equal(t, "ada", byGRN[0].Nickname())

	byGRN, err = s.ListByGRNs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, byGRN)

	updated, err := s.Update(ctx, g, user.Patch{
		Nickname: ptr("ada2"),
		Roles:    []string{"admin"},
	})
	require.NoError(t, err)
	assert.Equal(t, "ada2", updated.Nickname())
	assert.Equal(t, []string{"admin"}, updated.Roles())

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
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(groupGRN, "eng", orgGRN, string(org.RoleMember))))
	projectGRN := grnOf("project", "web")
	require.NoError(t, s.CreateProject(ctx, org.NewProject(projectGRN, "web", orgGRN, string(org.RoleMember))))

	// The roles column survives the round-trip and still resolves to
	// the actions the scoped check reads.
	gotGroup, err := s.GetGroup(ctx, groupGRN)
	require.NoError(t, err)
	assert.Equal(t, []string{string(org.RoleMember)}, gotGroup.Roles())
	assert.Contains(t, org.ActionsFor(gotGroup.Roles()), org.ActionGroupRead)

	gotProject, err := s.GetProject(ctx, projectGRN)
	require.NoError(t, err)
	assert.Equal(t, []string{string(org.RoleMember)}, gotProject.Roles())
	assert.Contains(t, org.ActionsFor(gotProject.Roles()), org.ActionProjectRead)

	userGRN := grnOf("user", "ada")
	require.NoError(t, user.NewPostgresStore(p).Create(ctx, user.New(userGRN, "ada")))
	require.NoError(t, s.AddGroupMember(ctx, groupGRN, userGRN))
	require.NoError(t, s.AddProjectMember(ctx, projectGRN, userGRN))

	members, err := s.ListGroupMembers(ctx, groupGRN)
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{userGRN}, members)

	groups, err := s.ListGroups(ctx, orgGRN, org.ListFilter{})
	require.NoError(t, err)
	assert.Len(t, groups, 1)
	assert.Equal(t, orgGRN, groups[0].Organization())
	assert.Equal(t, []string{string(org.RoleMember)}, groups[0].Roles())

	// A patch without roles keeps the stored ones (the coalesce
	// branch); one carrying roles replaces the whole set, and an empty
	// slice clears it rather than reading as "unchanged".
	patchedGroup, err := s.UpdateGroup(ctx, groupGRN, org.GroupPatch{Name: ptr("eng2")})
	require.NoError(t, err)
	assert.Equal(t, "eng2", patchedGroup.Name())
	assert.Equal(t, []string{string(org.RoleMember)}, patchedGroup.Roles())

	patchedGroup, err = s.UpdateGroup(ctx, groupGRN, org.GroupPatch{Roles: []string{string(org.RoleAdmin)}})
	require.NoError(t, err)
	assert.Equal(t, []string{string(org.RoleAdmin)}, patchedGroup.Roles())
	assert.Contains(t, org.ActionsFor(patchedGroup.Roles()), org.ActionOrgUpdate)

	patchedProject, err := s.UpdateProject(ctx, projectGRN, org.ProjectPatch{Name: ptr("web2")})
	require.NoError(t, err)
	assert.Equal(t, "web2", patchedProject.Name())
	assert.Equal(t, []string{string(org.RoleMember)}, patchedProject.Roles())

	patchedProject, err = s.UpdateProject(ctx, projectGRN, org.ProjectPatch{Roles: []string{}})
	require.NoError(t, err)
	assert.Empty(t, patchedProject.Roles())
	assert.Empty(t, org.ActionsFor(patchedProject.Roles()))

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

	// The refusal, against the removal above as its control: the first
	// call matched a row and this one matches none.
	assert.ErrorIs(t, s.RemoveMember(ctx, orgGRN, userGRN), org.ErrMemberNotFound)
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

// TestOrgStore_CreateInvitationPrunesExpiredPending holds that a pending
// invitation past its expiry is removed rather than merely refused, and
// that an accepted one that is equally past its expiry stays: it records
// where a membership came from, where the pending one is a capability
// nobody spent. The row is read from the table, because GetInvitation
// declines an expired invitation whether or not the row is still there.
//
// Two controls stand before the sweeping call, one per half of what it
// matches: an accepted invitation equally past its expiry, which a sweep
// reading expiry alone would take, and a pending one still live, which a
// sweep reading acceptance alone would take. Neither is written by the
// statement that sweeps — a row that statement inserts is out of reach
// of its own delete, so it would survive any sweep at all.
func TestOrgStore_CreateInvitationPrunesExpiredPending(t *testing.T) {
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

	lapsed := time.Now().UTC().Add(-time.Hour)
	ahead := time.Now().UTC().Add(time.Hour)

	spent := org.NewInvitation("tok-accepted", orgGRN, org.RoleMember, owner, lapsed)
	require.NoError(t, s.CreateInvitation(ctx, spent))
	_, err := s.AcceptInvitation(ctx, "tok-accepted", invitee)
	require.NoError(t, err)

	standing := org.NewInvitation("tok-standing", orgGRN, org.RoleMember, owner, ahead)
	require.NoError(t, s.CreateInvitation(ctx, standing))

	pending := org.NewInvitation("tok-pending", orgGRN, org.RoleMember, owner, lapsed)
	require.NoError(t, s.CreateInvitation(ctx, pending))
	// The row survives its own create: nothing here refuses to write an
	// invitation that is already past its expiry.
	require.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM invitations WHERE token = $1", "tok-pending"))

	// The next create is what removes it.
	fresh := org.NewInvitation("tok-fresh", orgGRN, org.RoleMember, owner, ahead)
	require.NoError(t, s.CreateInvitation(ctx, fresh))

	assert.Equal(t, 0, countRows(t, p,
		"SELECT count(*) FROM invitations WHERE token = $1", "tok-pending"),
		"the expired pending invitation was pruned")
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM invitations WHERE token = $1", "tok-accepted"),
		"an accepted invitation is kept, expiry or not")
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM invitations WHERE token = $1", "tok-standing"),
		"a pending invitation that is still live outlives the sweep")
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM invitations WHERE token = $1", "tok-fresh"))
}

// TestOrgStore_ListOrgsPagesEveryOrg runs ListOrgs, which is scoped by
// the page rather than by an owner. Two orgs are present, so the bounds
// decide the result instead of the fixture's size.
func TestOrgStore_ListOrgsPagesEveryOrg(t *testing.T) {
	ctx := context.Background()
	s := org.NewPostgresStore(pool(t))
	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")

	acmeOrg := org.NewOrganization(acme, "acme")
	require.NoError(t, s.CreateOrg(ctx, acmeOrg))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	all, err := s.ListOrgs(ctx, org.ListFilter{})
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{acme, globex}, grnsOf(all), "every org, ordered by GRN")
	require.Len(t, all, 2)
	assert.Equal(t, "acme", all[0].Name())
	assert.Equal(t, "globex", all[1].Name())

	// The stamp CreateOrg wrote, to the microsecond a timestamptz keeps.
	// A row read back zeroed, or stamped by a clock read at read time,
	// fails here.
	assert.WithinDuration(t, acmeOrg.CreatedAt(), all[0].CreatedAt(), time.Microsecond)

	page, err := s.ListOrgs(ctx, org.ListFilter{Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{acme}, grnsOf(page), "the limit takes the first page and stops")

	page, err = s.ListOrgs(ctx, org.ListFilter{Offset: 1})
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{globex}, grnsOf(page), "the offset skips the first org")
}

// TestOrgStore_GroupQueriesStayInsideTheirGroupAndOrg runs
// ListGroupsByMember, RemoveGroupMember and DeleteGroup. One member
// belongs to a group in each org and one org holds a second group, so
// the org filter and the member filter each have a row to exclude.
func TestOrgStore_GroupQueriesStayInsideTheirGroupAndOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := org.NewPostgresStore(p)

	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	engAcme, opsAcme := scopedGRN("acme", "group", "eng"), scopedGRN("acme", "group", "ops")
	engGlobex := scopedGRN("globex", "group", "eng")
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(engAcme, "eng", acme, string(org.RoleMember))))
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(opsAcme, "ops", acme, string(org.RoleMember))))
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(engGlobex, "eng", globex, string(org.RoleMember))))

	users := user.NewPostgresStore(p)
	ada, bob := scopedGRN("acme", "user", "ada"), scopedGRN("acme", "user", "bob")
	require.NoError(t, users.Create(ctx, user.New(ada, "ada")))
	require.NoError(t, users.Create(ctx, user.New(bob, "bob")))

	require.NoError(t, s.AddGroupMember(ctx, engAcme, ada))
	require.NoError(t, s.AddGroupMember(ctx, engAcme, bob))
	require.NoError(t, s.AddGroupMember(ctx, opsAcme, bob))
	require.NoError(t, s.AddGroupMember(ctx, engGlobex, ada))

	t.Run("a member's groups are the ones inside the org asked about", func(t *testing.T) {
		groups, err := s.ListGroupsByMember(ctx, acme, ada)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{engAcme}, groups)

		// Each group the answer above left out is reached by changing
		// only the argument that excluded it, so neither absence is a
		// row the fixture never wrote.
		groups, err = s.ListGroupsByMember(ctx, globex, ada)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{engGlobex}, groups, "the org filter is what kept this group out of acme's answer")

		groups, err = s.ListGroupsByMember(ctx, acme, bob)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{engAcme, opsAcme}, groups, "the member filter is what kept ops out of ada's answer")
	})

	t.Run("removing a member leaves the group's other member and the member's other group", func(t *testing.T) {
		require.NoError(t, s.RemoveGroupMember(ctx, engAcme, ada))

		members, err := s.ListGroupMembers(ctx, engAcme)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{bob}, members, "the removal took ada out of eng and left bob in it")

		groups, err := s.ListGroupsByMember(ctx, globex, ada)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{engGlobex}, groups, "ada's membership of the other org's group is untouched")

		// The two refusals, against the removal above as their
		// control: it matched a row, and neither of these matches
		// one. Without them a removal reports the same success
		// whether or not it removed anything.
		assert.ErrorIs(t, s.RemoveGroupMember(ctx, engAcme, ada), org.ErrGroupMemberNotFound,
			"ada is no longer a member of eng")
		assert.ErrorIs(t, s.RemoveGroupMember(ctx, scopedGRN("acme", "group", "nope"), bob), org.ErrGroupMemberNotFound,
			"the group named does not exist")
	})

	t.Run("deleting a group takes its members and leaves its neighbours", func(t *testing.T) {
		require.NoError(t, s.DeleteGroup(ctx, engAcme))

		_, err := s.GetGroup(ctx, engAcme)
		assert.ErrorIs(t, err, org.ErrGroupNotFound)

		members, err := s.ListGroupMembers(ctx, engAcme)
		require.NoError(t, err)
		assert.Empty(t, members, "bob's membership row went with the group")

		_, err = s.GetGroup(ctx, engGlobex)
		assert.NoError(t, err, "the other org's group of the same name survives")
		_, err = s.GetOrg(ctx, acme)
		assert.NoError(t, err, "the org survives its group")

		remaining, err := s.ListGroups(ctx, acme, org.ListFilter{})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{opsAcme}, grnsOf(remaining), "the org's other group survives")

		// The refusal, against the delete above as its control: the
		// first call matched a row and this one matches none.
		assert.ErrorIs(t, s.DeleteGroup(ctx, engAcme), org.ErrGroupNotFound)
	})
}

// TestOrgStore_ProjectQueriesStayInsideTheirProjectAndOrg runs
// ListProjects, ListProjectMembers, RemoveProjectMember and
// DeleteProject. Each org holds a project named web and one member
// belongs to both, so the org filter and the project filter each have a
// row to exclude.
func TestOrgStore_ProjectQueriesStayInsideTheirProjectAndOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := org.NewPostgresStore(p)

	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	apiAcme, webAcme := scopedGRN("acme", "project", "api"), scopedGRN("acme", "project", "web")
	webGlobex := scopedGRN("globex", "project", "web")
	require.NoError(t, s.CreateProject(ctx, org.NewProject(apiAcme, "api", acme, string(org.RoleMember))))
	require.NoError(t, s.CreateProject(ctx, org.NewProject(webAcme, "web", acme, string(org.RoleMember))))
	require.NoError(t, s.CreateProject(ctx, org.NewProject(webGlobex, "web", globex, string(org.RoleMember))))

	users := user.NewPostgresStore(p)
	ada, bob := scopedGRN("acme", "user", "ada"), scopedGRN("acme", "user", "bob")
	require.NoError(t, users.Create(ctx, user.New(ada, "ada")))
	require.NoError(t, users.Create(ctx, user.New(bob, "bob")))

	require.NoError(t, s.AddProjectMember(ctx, webAcme, ada))
	require.NoError(t, s.AddProjectMember(ctx, webAcme, bob))
	require.NoError(t, s.AddProjectMember(ctx, apiAcme, bob))
	require.NoError(t, s.AddProjectMember(ctx, webGlobex, ada))

	t.Run("an org's projects are its own, a page at a time", func(t *testing.T) {
		list, err := s.ListProjects(ctx, acme, org.ListFilter{})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{apiAcme, webAcme}, grnsOf(list), "the org's own projects, ordered by GRN")

		// The other org's project of the same name is reached by asking
		// about that org, so the org filter is what left it out above.
		other, err := s.ListProjects(ctx, globex, org.ListFilter{})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{webGlobex}, grnsOf(other))

		require.Len(t, list, 2)
		assert.Equal(t, "web", list[1].Name())
		assert.Equal(t, acme, list[1].Organization())
		assert.Equal(t, []string{string(org.RoleMember)}, list[1].Roles())

		page, err := s.ListProjects(ctx, acme, org.ListFilter{Limit: 1})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{apiAcme}, grnsOf(page), "the limit takes the first page and stops")

		page, err = s.ListProjects(ctx, acme, org.ListFilter{Offset: 1})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{webAcme}, grnsOf(page), "the offset skips the first project")
	})

	t.Run("a project's members are the ones inside the project asked about", func(t *testing.T) {
		members, err := s.ListProjectMembers(ctx, webAcme)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{ada, bob}, members, "ordered by member GRN")

		members, err = s.ListProjectMembers(ctx, apiAcme)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{bob}, members, "ada is absent from the org's other project")

		members, err = s.ListProjectMembers(ctx, webGlobex)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{ada}, members, "bob is absent from the other org's project of the same name")
	})

	t.Run("removing a member leaves the project's other member and the member's other project", func(t *testing.T) {
		require.NoError(t, s.RemoveProjectMember(ctx, webAcme, ada))

		members, err := s.ListProjectMembers(ctx, webAcme)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{bob}, members, "the removal took ada out of web and left bob in it")

		members, err = s.ListProjectMembers(ctx, webGlobex)
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{ada}, members, "ada's membership of the other org's project is untouched")

		// The two refusals, against the removal above as their
		// control: it matched a row, and neither of these matches
		// one.
		assert.ErrorIs(t, s.RemoveProjectMember(ctx, webAcme, ada), org.ErrProjectMemberNotFound,
			"ada is no longer a member of web")
		assert.ErrorIs(t, s.RemoveProjectMember(ctx, scopedGRN("acme", "project", "nope"), bob), org.ErrProjectMemberNotFound,
			"the project named does not exist")
	})

	t.Run("deleting a project takes its members and leaves its neighbours", func(t *testing.T) {
		require.NoError(t, s.DeleteProject(ctx, webAcme))

		_, err := s.GetProject(ctx, webAcme)
		assert.ErrorIs(t, err, org.ErrProjectNotFound)

		members, err := s.ListProjectMembers(ctx, webAcme)
		require.NoError(t, err)
		assert.Empty(t, members, "bob's membership row went with the project")

		_, err = s.GetProject(ctx, webGlobex)
		assert.NoError(t, err, "the other org's project of the same name survives")
		_, err = s.GetOrg(ctx, acme)
		assert.NoError(t, err, "the org survives its project")

		remaining, err := s.ListProjects(ctx, acme, org.ListFilter{})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{apiAcme}, grnsOf(remaining), "the org's other project survives")

		// The refusal, against the delete above as its control: the
		// first call matched a row and this one matches none.
		assert.ErrorIs(t, s.DeleteProject(ctx, webAcme), org.ErrProjectNotFound)
	})
}

// grnsOf names what a list returned, so a scoped list is compared whole
// instead of by a length guard and an index — the guard aborts before
// the claim it guards, and a list is one claim.
func grnsOf[T interface{ GRN() grn.GRN }](items []T) []grn.GRN {
	out := make([]grn.GRN, 0, len(items))
	for _, item := range items {
		out = append(out, item.GRN())
	}
	return out
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

// TestSessionStore_CreatePrunesExpired holds that an expired session's
// row is removed rather than merely read as absent, which is what bounds
// the table across every sign-in ever made. The row is read from the
// table, because Get declines an expired session either way.
//
// The standing session is the control, and it is created before the
// sweeping call rather than by it: a session the same statement inserts
// is out of reach of that statement's delete, so it would survive a
// sweep that took the whole table with it and prove nothing.
func TestSessionStore_CreatePrunesExpired(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	users := user.NewPostgresStore(p)
	s := authn.NewPostgresSessionStore(p)
	userGRN := grnOf("user", "ada")
	require.NoError(t, users.Create(ctx, user.New(userGRN, "ada")))

	standing, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, standing, authn.NewSession(userGRN, time.Hour)))

	stale, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, stale, authn.NewSession(userGRN, -time.Hour)))
	// The row survives its own create; the next one is what removes it.
	require.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", authn.HashToken(stale)))

	fresh, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, fresh, authn.NewSession(userGRN, time.Hour)))

	assert.Equal(t, 0, countRows(t, p,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", authn.HashToken(stale)),
		"the expired session was pruned")
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", authn.HashToken(standing)),
		"a session that is still live outlives the sweep")
	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM sessions WHERE token_hash = $1", authn.HashToken(fresh)))
}

// TestOAuthStateStore_RoundTrip drives a login state through the shape
// the callback rests on: live once, spent by the callback that
// completes, and gone when its flow has run out. The first consume is
// the control for the second — the refusals below are the state having
// been spent or lapsed, not a store that refuses everything.
func TestOAuthStateStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authn.NewPostgresOAuthStateStore(p)

	state, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, state, time.Minute))
	require.NoError(t, s.Consume(ctx, state))

	assert.ErrorIs(t, s.Consume(ctx, state), authn.ErrOAuthStateNotLive)
	assert.ErrorIs(t, s.Consume(ctx, "never-issued"), authn.ErrOAuthStateNotLive)

	// A state past its lifetime is not live, whether or not the row
	// naming it is still there.
	lapsed, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, lapsed, -time.Minute))
	assert.ErrorIs(t, s.Consume(ctx, lapsed), authn.ErrOAuthStateNotLive)

	// Issuing prunes what has lapsed, which is what bounds the table:
	// the row above survives its own issue and not the next one.
	fresh, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, fresh, time.Minute))
	var rows int
	require.NoError(t, p.QueryRow(ctx,
		"SELECT count(*) FROM oauth_states WHERE state_hash = $1", authn.HashToken(lapsed),
	).Scan(&rows))
	assert.Equal(t, 0, rows, "the lapsed state was pruned")
}

// TestOAuthStateStore_IssueBoundsTheLiveStates holds that issuing
// refuses past MaxLiveOAuthStates live states, which is what bounds
// the write rate an unauthenticated caller can drive on
// /auth/login/{provider}: each state lives at most one flow lifetime,
// so the table — and the writes that fill it — stop at the bound
// however fast the requests arrive.
func TestOAuthStateStore_IssueBoundsTheLiveStates(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authn.NewPostgresOAuthStateStore(p)

	// One row short of the bound, seeded in bulk so the test pays no
	// round trip per row.
	_, err := p.Exec(ctx, `INSERT INTO oauth_states (state_hash, expires_at)
		SELECT 'seed-' || g, now() + interval '10 minutes'
		FROM generate_series(1, $1) g`, authn.MaxLiveOAuthStates-1)
	require.NoError(t, err)

	// The write that reaches the bound is admitted, and the one past
	// it is refused: the table holds exactly the bound, which is the
	// property the rate limit is read out of.
	seeded, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, s.Issue(ctx, seeded, time.Minute))
	fresh, err := authn.NewToken()
	require.NoError(t, err)
	assert.ErrorIs(t, s.Issue(ctx, fresh, time.Minute), authn.ErrOAuthStateRateLimited)
	assert.Equal(t, authn.MaxLiveOAuthStates, countRows(t, p,
		"SELECT count(*) FROM oauth_states"))

	// The control: spending a state admits one more, so the refusal
	// above is the bound on live states and not a store that has
	// stopped writing.
	require.NoError(t, s.Consume(ctx, seeded))
	require.NoError(t, s.Issue(ctx, fresh, time.Minute))

	// A refused issue still prunes: an expired row lying on a full
	// table is swept by the issue that refuses, so shedding load
	// cannot wedge the table.
	_, err = p.Exec(ctx, `INSERT INTO oauth_states (state_hash, expires_at)
		VALUES ('lapsed', now() - interval '1 minute')`)
	require.NoError(t, err)
	another, err := authn.NewToken()
	require.NoError(t, err)
	assert.ErrorIs(t, s.Issue(ctx, another, time.Minute), authn.ErrOAuthStateRateLimited)
	assert.Equal(t, 0, countRows(t, p,
		"SELECT count(*) FROM oauth_states WHERE state_hash = 'lapsed'"),
		"the refused issue still swept the lapsed row")
}

// TestAgentStore_RoundTrip drives an agent through the shape ADR-0012
// decided: garam mints its GRN, an operator is assigned to it, a
// reassignment raises the epoch, and the operator that was replaced
// writes nothing afterwards.
func TestAgentStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, a))

	got, err := s.Get(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, orgGRN, got.Organization())
	assert.Equal(t, agent.ProvisioningStatePending, got.ProvisioningState())
	assert.Equal(t, agent.RuntimeStateUnknown, got.RuntimeState())
	// The stamps read back are the ones New set, to the microsecond
	// Postgres keeps them at.
	assert.WithinDuration(t, a.CreatedAt(), got.CreatedAt(), time.Microsecond)
	assert.WithinDuration(t, a.UpdatedAt(), got.UpdatedAt(), time.Microsecond)

	_, err = s.Get(ctx, grnOf("agent", "missing"))
	assert.ErrorIs(t, err, agent.ErrAgentNotFound)

	// Assignment is a relation, so an unassigned agent is a stored
	// agent with no operator.
	_, err = s.GetAssignment(ctx, g)
	assert.ErrorIs(t, err, agent.ErrAssignmentNotFound)

	// An agent with no assignment has no current epoch, so every report
	// about it is stale.
	_, err = s.ReportProvisioningState(ctx, g, 1, agent.ProvisioningStateReady)
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)
	_, err = s.ReportRuntimeState(ctx, g, 1, agent.RuntimeStateConnected)
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)

	first := grnOf(agent.ServiceTypeOperator, "one")
	assigned, err := s.Assign(ctx, g, first)
	require.NoError(t, err)
	assert.Equal(t, first, assigned.Operator())
	assert.Equal(t, 1, assigned.Epoch())
	assert.True(t, assigned.CreatedAt().After(a.CreatedAt()), "the assignment is stamped when it is written, after the agent it binds")

	// The operator reports the pod; the agent reports itself. Neither
	// report moves the other's state.
	provisioned, err := s.ReportProvisioningState(ctx, g, assigned.Epoch(), agent.ProvisioningStateReady)
	require.NoError(t, err)
	assert.Equal(t, agent.ProvisioningStateReady, provisioned.ProvisioningState())
	assert.Equal(t, agent.RuntimeStateUnknown, provisioned.RuntimeState())
	// A report moves the update stamp and leaves the create stamp, so
	// each is read from its own column.
	assert.WithinDuration(t, a.CreatedAt(), provisioned.CreatedAt(), time.Microsecond)
	assert.True(t, provisioned.UpdatedAt().After(a.UpdatedAt()), "a report moves the update stamp")

	running, err := s.ReportRuntimeState(ctx, g, assigned.Epoch(), agent.RuntimeStateConnected)
	require.NoError(t, err)
	assert.Equal(t, agent.RuntimeStateConnected, running.RuntimeState())
	assert.Equal(t, agent.ProvisioningStateReady, running.ProvisioningState())

	second := grnOf(agent.ServiceTypeOperator, "two")
	reassigned, err := s.Assign(ctx, g, second)
	require.NoError(t, err)
	assert.Equal(t, second, reassigned.Operator())
	assert.Equal(t, 2, reassigned.Epoch(), "a reassignment raises the epoch")
	// A reassignment writes onto the row the first one left, so the
	// create stamp is still the first one's.
	assert.Equal(t, assigned.CreatedAt(), reassigned.CreatedAt())
	assert.True(t, reassigned.UpdatedAt().After(assigned.UpdatedAt()), "and moves the update stamp")

	stored, err := s.GetAssignment(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, second, stored.Operator())
	assert.Equal(t, 2, stored.Epoch())

	// A pod left running by the replaced operator still holds epoch 1.
	_, err = s.ReportProvisioningState(ctx, g, assigned.Epoch(), agent.ProvisioningStateFailed)
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)
	_, err = s.ReportRuntimeState(ctx, g, assigned.Epoch(), agent.RuntimeStateDisconnected)
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)

	// An epoch too wide for the column shares its low 32 bits with the
	// live one and is stale all the same.
	_, err = s.ReportRuntimeState(ctx, g, stored.Epoch()+1<<32, agent.RuntimeStateDisconnected)
	assert.ErrorIs(t, err, agent.ErrStaleEpoch)

	after, err := s.Get(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, agent.ProvisioningStateReady, after.ProvisioningState(), "the fenced write left the row alone")
	assert.Equal(t, agent.RuntimeStateConnected, after.RuntimeState())

	// The current epoch still writes.
	current, err := s.ReportRuntimeState(ctx, g, stored.Epoch(), agent.RuntimeStateDisconnected)
	require.NoError(t, err)
	assert.Equal(t, agent.RuntimeStateDisconnected, current.RuntimeState())

	_, err = s.ReportRuntimeState(ctx, grnOf("agent", "missing"), 1, agent.RuntimeStateConnected)
	assert.ErrorIs(t, err, agent.ErrAgentNotFound)

	// An agent belongs to its org and disappears with it, assignment
	// included.
	require.NoError(t, org.NewPostgresStore(p).DeleteOrg(ctx, orgGRN))
	_, err = s.Get(ctx, g)
	assert.ErrorIs(t, err, agent.ErrAgentNotFound)
	var assignments int
	require.NoError(t, p.QueryRow(ctx, `SELECT count(*) FROM agent_assignments`).Scan(&assignments))
	assert.Zero(t, assignments)
}

// TestAgentStore_ListsTheOrgsOwnAgents covers what the browser read
// path is served from: an organization's agents and the assignments
// holding them, each ordered and each scoped to that organization.
// Every state moves before the read, so a list that answered from the
// wrong column or the wrong row is visible.
func TestAgentStore_ListsTheOrgsOwnAgents(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgs := org.NewPostgresStore(p)
	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	held := seedAgent(t, s, acme)
	loose := seedAgent(t, s, acme)
	elsewhere := seedAgent(t, s, globex)

	operator := scopedGRN(acme.Identifier, agent.ServiceTypeOperator, "field-one")
	assigned, err := s.Assign(ctx, held, operator)
	require.NoError(t, err)
	_, err = s.ReportProvisioningState(ctx, held, assigned.Epoch(), agent.ProvisioningStateReady)
	require.NoError(t, err)
	_, err = s.ReportRuntimeState(ctx, held, assigned.Epoch(), agent.RuntimeStateConnected)
	require.NoError(t, err)
	_, err = s.Assign(ctx, elsewhere, scopedGRN(globex.Identifier, agent.ServiceTypeOperator, "field-two"))
	require.NoError(t, err)

	listed, err := s.ListByOrg(ctx, acme)
	require.NoError(t, err)
	require.Len(t, listed, 2, "the org's own agents and no other org's")
	assert.ElementsMatch(t, []grn.GRN{held, loose}, []grn.GRN{listed[0].GRN(), listed[1].GRN()})
	assert.Less(t, listed[0].GRN().String(), listed[1].GRN().String(), "ordered by GRN")

	byGRN := map[grn.GRN]*agent.Agent{}
	for _, a := range listed {
		byGRN[a.GRN()] = a
	}
	// Each reported state comes back in its own field, and the agent
	// nobody reported on keeps the initial pair.
	assert.Equal(t, agent.ProvisioningStateReady, byGRN[held].ProvisioningState())
	assert.Equal(t, agent.RuntimeStateConnected, byGRN[held].RuntimeState())
	assert.Equal(t, agent.ProvisioningStatePending, byGRN[loose].ProvisioningState())
	assert.Equal(t, agent.RuntimeStateUnknown, byGRN[loose].RuntimeState())

	// An unassigned agent contributes no assignment, so the list is not
	// one per agent.
	assignments, err := s.ListAssignmentsByOrg(ctx, acme)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	assert.Equal(t, held, assignments[0].Agent())
	assert.Equal(t, operator, assignments[0].Operator())
	assert.Equal(t, 1, assignments[0].Epoch())

	// The other org holds its own agent and its own assignment, so the
	// scoping refuses a pairing rather than every pairing.
	listed, err = s.ListByOrg(ctx, globex)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, elsewhere, listed[0].GRN())
	assignments, err = s.ListAssignmentsByOrg(ctx, globex)
	require.NoError(t, err)
	require.Len(t, assignments, 1)
	assert.Equal(t, elsewhere, assignments[0].Agent())

	// An org that holds none reads as empty rather than as everyone's.
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("initech"), "initech")))
	listed, err = s.ListByOrg(ctx, orgRootGRN("initech"))
	require.NoError(t, err)
	assert.Empty(t, listed)
	assignments, err = s.ListAssignmentsByOrg(ctx, orgRootGRN("initech"))
	require.NoError(t, err)
	assert.Empty(t, assignments)
}

// seedAgent mints an agent of the org and stores it.
func seedAgent(t *testing.T, s *agent.PostgresStore, orgGRN grn.GRN) grn.GRN {
	t.Helper()
	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(context.Background(), a))
	return g
}

// TestAgent_GRNNamesAnAgentOfItsOrg covers the schema's copy of the
// ownership rule the agent domain enforces: a row whose GRN names
// another organization, or names something that is not an agent, is
// refused even by a statement that never passes through the domain. A
// row the database accepted and the domain refused to read would be
// neither writable nor readable.
func TestAgent_GRNNamesAnAgentOfItsOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, a))

	// agent.New refuses both pairings, so only a direct statement reaches
	// the constraint.
	rejected := []struct {
		name string
		grn  grn.GRN
	}{
		{"another org's agent", scopedGRN("globex", agent.ServiceType, "deadbeef")},
		{"an identity that is not an agent", grnOf("user", "ada")},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Exec(ctx, `INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
				VALUES ($1, $2, 'pending', 'unknown', now(), now())`, tt.grn.String(), orgGRN.String())
			assert.Error(t, err)
		})
	}
}

// TestAgentAssignment_StaysInsideItsOrg covers the schema's rules for an
// assignment (ADR-0015): the GRN it carries names an operator, and that
// operator belongs to the agent's org. An operator is addressed as an
// agent is (ADR-0012), so a root-owned one has no place here and is not
// admitted the way a policy attachment admits a root-owned subject.
func TestAgentAssignment_StaysInsideItsOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, a))

	rejected := []struct {
		name     string
		operator grn.GRN
	}{
		{"another org's operator", scopedGRN("globex", agent.ServiceTypeOperator, "one")},
		{"a root-owned operator", grn.New(grn.RootOwner, grn.DefaultProject, agent.ServiceTypeOperator, "one")},
		{"an identity that is not an operator", grnOf("user", "ada")},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Assign(ctx, g, tt.operator)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
			_, err = s.GetAssignment(ctx, g)
			assert.ErrorIs(t, err, agent.ErrAssignmentNotFound, "the rejected assignment was not stored")
		})
	}

	// The operator of the agent's own org is assigned, so the rules
	// reject a pairing rather than every pairing.
	operator := grnOf(agent.ServiceTypeOperator, "one")
	assigned, err := s.Assign(ctx, g, operator)
	require.NoError(t, err)
	assert.Equal(t, operator, assigned.Operator())

	// A reassignment writes onto the row the agent already has, and it
	// leaves the org the same way an assignment does.
	_, err = s.Assign(ctx, g, scopedGRN("globex", agent.ServiceTypeOperator, "two"))
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	stored, err := s.GetAssignment(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, operator, stored.Operator(), "the rejected reassignment left the row alone")
	assert.Equal(t, 1, stored.Epoch())
}

// TestAgentDefinitionStore_RoundTrip covers what a definition is
// (ADR-0026): an organization's record of what an agent should be,
// written once, holding no agent. The agent GRN it names answers
// nothing among the agents, because garam learns an agent exists from
// the operator that reports it.
func TestAgentDefinitionStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	orgs := org.NewPostgresStore(p)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	otherOrgGRN := grn.New(grn.RootOwner, grn.DefaultProject, "org", "globex")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(otherOrgGRN, "globex")))

	values, err := agent.ParseValues(map[string]string{"model": "haiku", "workspace": "frontend"})
	require.NoError(t, err)
	definedBy := grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada")
	d, err := agent.NewDefinition(orgGRN, grnOf(agent.ServiceTypeOperator, "one"), values, definedBy)
	require.NoError(t, err)
	require.NoError(t, s.Define(ctx, d))

	stored, err := s.ListDefinitionsByOrg(ctx, orgGRN)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, d.Agent(), stored[0].Agent())
	assert.Equal(t, d.Operator(), stored[0].Operator())
	assert.Equal(t, definedBy, stored[0].DefinedBy())
	assert.Equal(t, values, stored[0].Values(), "every key and value survives the column")
	assert.False(t, stored[0].CreatedAt().IsZero())

	// The definition put no agent anywhere: reading the GRN it names
	// answers as it did before the definition was written, while an
	// agent an operator reported is found.
	_, err = s.Get(ctx, d.Agent())
	assert.ErrorIs(t, err, agent.ErrAgentNotFound)
	_, err = s.GetAssignment(ctx, d.Agent())
	assert.ErrorIs(t, err, agent.ErrAssignmentNotFound)
	reported, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := agent.New(reported, orgGRN)
	require.NoError(t, err)
	require.NoError(t, s.Create(ctx, a))
	_, err = s.Get(ctx, reported)
	assert.NoError(t, err, "an agent that was reported is found, so the miss above is the definition")
	agents, err := s.ListByOrg(ctx, orgGRN)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, reported, agents[0].GRN())

	// A definition belongs to the org that wrote it.
	elsewhere, err := agent.NewDefinition(otherOrgGRN, scopedGRN("globex", agent.ServiceTypeOperator, "one"), agent.Values{}, definedBy)
	require.NoError(t, err)
	require.NoError(t, s.Define(ctx, elsewhere))
	stored, err = s.ListDefinitionsByOrg(ctx, orgGRN)
	require.NoError(t, err)
	require.Len(t, stored, 1, "this org's definitions and no other org's")
	assert.Equal(t, d.Agent(), stored[0].Agent())

	// An empty payload is stored as one and read back as one: an
	// operator reads no values rather than a missing object.
	storedElsewhere, err := s.ListDefinitionsByOrg(ctx, otherOrgGRN)
	require.NoError(t, err)
	require.Len(t, storedElsewhere, 1)
	assert.Equal(t, agent.Values{}, storedElsewhere[0].Values())
}

// TestAgentDefinition_StaysInsideItsOrgAndHoldsAnObject covers the
// schema's rules for a definition. internal/agent refuses the same
// pairings, so only a direct statement reaches the constraints — and a
// row the database accepted and the domain refused to read would be
// neither writable nor readable.
func TestAgentDefinition_StaysInsideItsOrgAndHoldsAnObject(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	g, err := agent.NewGRN(orgGRN)
	require.NoError(t, err)

	rejected := []struct {
		name     string
		agentGRN grn.GRN
		operator grn.GRN
		values   string
	}{
		{"another org's agent", scopedGRN("globex", agent.ServiceType, "deadbeef"), grnOf(agent.ServiceTypeOperator, "one"), `{}`},
		{"an identity that is not an agent", grnOf("user", "ada"), grnOf(agent.ServiceTypeOperator, "one"), `{}`},
		{"another org's operator", g, scopedGRN("globex", agent.ServiceTypeOperator, "one"), `{}`},
		{"an identity that is not an operator", g, grnOf(agent.ServiceType, "deadbeef"), `{}`},
		{"values that are not an object", g, grnOf(agent.ServiceTypeOperator, "one"), `[]`},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Exec(ctx, `INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, agent_values, defined_by, created_at)
				VALUES ($1, $2, $3, $4::jsonb, $5, now())`,
				tt.agentGRN.String(), orgGRN.String(), tt.operator.String(), tt.values, grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada").String())
			assert.Error(t, err)
		})
	}

	// The same statement naming this org's agent and operator is
	// accepted, so the constraints reject a row rather than every row.
	_, err = p.Exec(ctx, `INSERT INTO agent_definitions (agent_grn, org_grn, operator_grn, agent_values, defined_by, created_at)
		VALUES ($1, $2, $3, $4::jsonb, $5, now())`,
		g.String(), orgGRN.String(), grnOf(agent.ServiceTypeOperator, "one").String(), `{"model":"haiku"}`,
		grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada").String())
	require.NoError(t, err)
}

func ptr[T any](v T) *T { return &v }

// policyDoc builds a one-statement document whose Sid identifies the
// version it belongs to.
func policyDoc(sid string, effect wire.Effect) authz.PolicyDocument {
	return statementDoc(sid, effect, "org:read", "grn:acme:*")
}

// statementDoc builds a one-statement document over an explicit action
// and resource pattern.
func statementDoc(sid string, effect wire.Effect, action, resource string) authz.PolicyDocument {
	return authz.PolicyDocument{
		Version: "2026-07-25",
		Statement: []authz.Statement{{
			Sid:      sid,
			Effect:   effect,
			Action:   []string{action},
			Resource: []string{resource},
		}},
	}
}

// newPolicyOrg creates the org a policy hangs off and returns its GRN.
func newPolicyOrg(t *testing.T, p *pgxpool.Pool) grn.GRN {
	t.Helper()
	orgGRN := grnOf("org", "acme")
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(context.Background(), org.NewOrganization(orgGRN, "acme")))
	return orgGRN
}

func TestPolicyStore_LifecycleRoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)
	author := grnOf("user", "ada")

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	created, err := s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), author)
	require.NoError(t, err)
	assert.Equal(t, authz.PolicyStatusDraft, created.Status())
	assert.Equal(t, 1, created.DefaultVersion())

	policies, err := s.ListPolicies(ctx, orgGRN)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, "read-only", policies[0].Name())

	// A draft contributes nothing until it is activated.
	active, err := s.ActivePolicies(ctx, orgGRN)
	require.NoError(t, err)
	assert.Empty(t, active)

	activated, err := s.SetPolicyStatus(ctx, g, authz.PolicyStatusActive)
	require.NoError(t, err)
	assert.Equal(t, authz.PolicyStatusActive, activated.Status())
	active, err = s.ActivePolicies(ctx, orgGRN)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "v1", active[0].Document.Statement[0].Sid)
	assert.Empty(t, active[0].Subjects, "an unattached policy applies org-wide")

	// Appending a version leaves the default pointer alone.
	v2, err := s.AddPolicyVersion(ctx, g, policyDoc("v2", wire.EffectDeny), author)
	require.NoError(t, err)
	assert.Equal(t, 2, v2.Version())
	active, err = s.ActivePolicies(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, "v1", active[0].Document.Statement[0].Sid)

	rolled, err := s.SetPolicyDefaultVersion(ctx, g, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, rolled.DefaultVersion())
	active, err = s.ActivePolicies(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, "v2", active[0].Document.Statement[0].Sid)
	assert.Equal(t, wire.EffectDeny, active[0].Document.Statement[0].Effect)

	versions, err := s.ListPolicyVersions(ctx, g)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	assert.Equal(t, author, versions[0].CreatedBy())

	_, err = s.SetPolicyDefaultVersion(ctx, g, 99)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	_, err = s.GetPolicy(ctx, grn.New("acme", "default", authz.ServiceTypePolicy, "missing"))
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	_, err = s.AddPolicyVersion(ctx, grn.New("acme", "default", authz.ServiceTypePolicy, "missing"), policyDoc("x", wire.EffectAllow), author)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
}

// TestPolicyService_ArchiveRoundTrip drives the lifecycle service over
// the real store: activate, then archive, and the org keeps nothing to
// evaluate.
func TestPolicyService_ArchiveRoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(s)
	srv := authz.NewPolicyGRPCServer(s, cache)
	orgGRN := newPolicyOrg(t, p)

	created, err := srv.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      "read-only",
		Document:  `{"Version":"2026-07-25","Statement":[{"Sid":"v1","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`,
		CreatedBy: grnOf("user", "ada").String(),
	})
	require.NoError(t, err)
	policyGRN := created.GetPolicy().GetGrn()

	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	statements, err := cache.Statements(ctx, orgGRN, nil)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	archived, err := srv.ArchivePolicy(ctx, &authzv1.ArchivePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	assert.Equal(t, authzv1.PolicyStatus_POLICY_STATUS_ARCHIVED, archived.GetPolicy().GetStatus())

	statements, err = cache.Statements(ctx, orgGRN, nil)
	require.NoError(t, err)
	assert.Empty(t, statements)

	// The policy and its versions survive the transition.
	versions, err := srv.ListPolicyVersions(ctx, &authzv1.ListPolicyVersionsRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	assert.Len(t, versions.GetVersions(), 1)
}

func TestPolicyVersions_AppendOnly(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), grnOf("user", "ada"))
	require.NoError(t, err)

	_, err = p.Exec(ctx, `UPDATE policy_versions SET document = '{}'::jsonb WHERE policy_grn = $1`, g.String())
	assert.Error(t, err, "a stored version must not be rewritten")
	_, err = p.Exec(ctx, `DELETE FROM policy_versions WHERE policy_grn = $1`, g.String())
	assert.Error(t, err, "a stored version must not be deleted on its own")

	// Versions do disappear with the policy that owns them.
	require.NoError(t, org.NewPostgresStore(p).DeleteOrg(ctx, orgGRN))
	var count int
	require.NoError(t, p.QueryRow(ctx, `SELECT count(*) FROM policy_versions`).Scan(&count))
	assert.Zero(t, count)
}

func TestPolicyCache_InvalidatedByNotify(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)

	cache := authz.NewPolicyCache(s)
	go func() { _ = authz.ListenPolicyChanges(ctx, p, cache) }()

	before, err := cache.Statements(ctx, orgGRN, nil)
	require.NoError(t, err)
	require.Empty(t, before)

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), grnOf("user", "ada"))
	require.NoError(t, err)
	_, err = s.SetPolicyStatus(ctx, g, authz.PolicyStatusActive)
	require.NoError(t, err)

	// The listener registers its LISTEN concurrently with the writes
	// above, so the announcement is repeated until it has one to
	// receive. TestPolicyWrite_Notifies covers the write's own
	// announcement.
	assert.Eventually(t, func() bool {
		_, err := p.Exec(ctx, `SELECT pg_notify($1, $2)`, authz.PolicyChannel, orgGRN.String())
		require.NoError(t, err)
		got, err := cache.Statements(ctx, orgGRN, nil)
		return err == nil && len(got) == 1
	}, 5*time.Second, 20*time.Millisecond, "the notify listener should drop the org's cached statements")
}

func TestPolicyWrite_Notifies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)

	conn, err := p.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(ctx, "LISTEN "+authz.PolicyChannel)
	require.NoError(t, err)

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), grnOf("user", "ada"))
	require.NoError(t, err)

	n, err := conn.Conn().WaitForNotification(ctx)
	require.NoError(t, err)
	assert.Equal(t, authz.PolicyChannel, n.Channel)
	assert.Equal(t, orgGRN.String(), n.Payload)

	// An attachment write changes what the policy applies to, so it
	// announces the same org.
	_, err = s.AttachPolicy(ctx, g, grnOf("user", "ada"))
	require.NoError(t, err)
	n, err = conn.Conn().WaitForNotification(ctx)
	require.NoError(t, err)
	assert.Equal(t, orgGRN.String(), n.Payload)
}

// TestPolicyAttachments_RoundTrip drives attachment over the real
// store: an attached policy reaches the subject it names and the
// group it names, an archived one reaches nobody, and detaching an
// active policy's last attachment is refused rather than widening the
// policy back to an org-wide grant.
func TestPolicyAttachments_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(s)
	srv := authz.NewPolicyGRPCServer(s, cache)
	orgGRN := newPolicyOrg(t, p)
	ada, bob, eng := grnOf("user", "ada"), grnOf("user", "bob"), grnOf("group", "eng")

	created, err := srv.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      "read-only",
		Document:  `{"Version":"2026-07-25","Statement":[{"Sid":"v1","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`,
		CreatedBy: ada.String(),
	})
	require.NoError(t, err)
	policyGRN := created.GetPolicy().GetGrn()
	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)

	// Unattached, the policy applies to every subject of the org.
	statements, err := cache.Statements(ctx, orgGRN, []grn.GRN{bob})
	require.NoError(t, err)
	require.Len(t, statements, 1)

	_, err = srv.AttachPolicy(ctx, &authzv1.AttachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	require.NoError(t, err)
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{ada})
	require.NoError(t, err)
	require.Len(t, statements, 1)
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{bob})
	require.NoError(t, err)
	assert.Empty(t, statements, "an attached policy stops applying org-wide")

	// A group attachment reaches the members the caller reports.
	_, err = srv.AttachPolicy(ctx, &authzv1.AttachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: eng.String()})
	require.NoError(t, err)
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{bob, eng})
	require.NoError(t, err)
	require.Len(t, statements, 1)

	attachments, err := srv.ListPolicyAttachments(ctx, &authzv1.ListPolicyAttachmentsRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	assert.Len(t, attachments.GetAttachments(), 2)

	// An archived policy is not evaluated even where it is attached.
	_, err = srv.ArchivePolicy(ctx, &authzv1.ArchivePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{ada, eng})
	require.NoError(t, err)
	assert.Empty(t, statements)
	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)

	// Detaching down to one attachment narrows as before, but the last
	// one is refused: removing it would widen the active policy
	// org-wide, so the policy keeps applying to eng and to eng only.
	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	require.NoError(t, err)
	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: eng.String()})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{bob})
	require.NoError(t, err)
	assert.Empty(t, statements, "a refused detach leaves the policy where it was")
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{eng})
	require.NoError(t, err)
	require.Len(t, statements, 1)

	// Archiving is what retires the policy, and what nothing evaluates
	// detaches freely.
	_, err = srv.ArchivePolicy(ctx, &authzv1.ArchivePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: eng.String()})
	require.NoError(t, err)

	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestPolicyAttachment_StaysInsideItsOrg covers the schema's
// org-consistency rule: an org-owned subject from another org cannot
// carry the policy out of its org.
func TestPolicyAttachment_StaysInsideItsOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), grnOf("user", "ada"))
	require.NoError(t, err)

	_, err = s.AttachPolicy(ctx, g, grn.New("other", "default", "group", "eng"))
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))

	// A root-owned identity exists above every org and is admitted.
	_, err = s.AttachPolicy(ctx, g, grn.New(grn.RootOwner, "default", "user", "ada"))
	require.NoError(t, err)

	_, err = s.AttachPolicy(ctx, grn.New("acme", "default", authz.ServiceTypePolicy, "missing"), grnOf("user", "ada"))
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
}

// TestPolicyAttachment_DenyOverridesAcrossSources covers the union the
// engine evaluates: a policy attached to the subject's group denies
// what the caller's role statements allow.
func TestPolicyAttachment_DenyOverridesAcrossSources(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(s)
	orgGRN := newPolicyOrg(t, p)
	ada, eng := grnOf("user", "ada"), grnOf("group", "eng")

	denyRead := authz.PolicyDocument{
		Version: "2026-07-25",
		Statement: []authz.Statement{{
			Sid:      "deny-read",
			Effect:   wire.EffectDeny,
			Action:   []string{"project:read"},
			Resource: []string{"grn:acme:*"},
		}},
	}
	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "no-read"), denyRead, ada)
	require.NoError(t, err)
	_, err = s.SetPolicyStatus(ctx, g, authz.PolicyStatusActive)
	require.NoError(t, err)
	_, err = s.AttachPolicy(ctx, g, eng)
	require.NoError(t, err)

	req := &authzv1.CheckRequest{
		SubjectGrn: ada.String(),
		ObjectGrn:  grnOf("project", "web").String(),
		Action:     "project:read",
		Statements: []*authzv1.Statement{{
			Sid:      "org-role-owner",
			Effect:   authzv1.Effect_EFFECT_ALLOW,
			Action:   []string{"project:*"},
			Resource: []string{"grn:acme:*"},
		}},
	}
	engine := authz.NewGRPCServer(cache)
	resp, err := engine.Check(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed(), "the attachment does not reach a subject outside the group")

	req.SubjectGroups = []string{eng.String()}
	resp, err = engine.Check(ctx, req)
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed(), "a group-attached deny overrides the caller's allow")
}

// TestPolicyAttachment_ADenyDoesNotWidenOnDetach covers the transition
// ADR-0008 left symmetric: a Deny written for one subject, detached
// from it, must not reach the subjects it was never attached to. The
// detach of the last attachment is refused, so the Deny goes on
// deciding for its subject — the passing control beside the refusal —
// and bob, whom a widening would have caught, is never reached.
func TestPolicyAttachment_ADenyDoesNotWidenOnDetach(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(s)
	srv := authz.NewPolicyGRPCServer(s, cache)
	orgGRN := newPolicyOrg(t, p)
	ada, bob := grnOf("user", "ada"), grnOf("user", "bob")

	created, err := srv.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      "no-read",
		Document:  `{"Version":"2026-07-25","Statement":[{"Sid":"deny-read","Effect":"Deny","Action":["project:read"],"Resource":["grn:acme:*"]}]}`,
		CreatedBy: ada.String(),
	})
	require.NoError(t, err)
	policyGRN := created.GetPolicy().GetGrn()
	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	_, err = srv.AttachPolicy(ctx, &authzv1.AttachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	require.NoError(t, err)

	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))

	statements, err := cache.Statements(ctx, orgGRN, []grn.GRN{bob})
	require.NoError(t, err)
	assert.Empty(t, statements, "a detached Deny reaches no subject it was never attached to")
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{ada})
	require.NoError(t, err)
	require.Len(t, statements, 1, "the refused detach leaves the Deny deciding for its subject")
}

// TestOrgIsolation_OnlyAttachmentKeepsGrantsInsideAnOrg asserts what
// separates two orgs' grants. The attached policies here match every
// org, so what holds each off the other org's subjects is its
// attachment, and what holds a role statement off is the org in its
// resource pattern. The engine scopes its lookup by the object's org
// and never by the subject's, so an unattached policy reaches whatever
// subject it is handed — the boundary ADR-0025 leaves to the caller,
// recorded here as the behaviour the engine has.
func TestOrgIsolation_OnlyAttachmentKeepsGrantsInsideAnOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	store := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(store)
	policies := authz.NewPolicyGRPCServer(store, cache)
	engine := authz.NewGRPCServer(cache)

	orgs := org.NewPostgresStore(p)
	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	adaA, webA := scopedGRN("acme", "user", "ada"), scopedGRN("acme", "project", "web")
	adaB, engB := scopedGRN("globex", "user", "ada"), scopedGRN("globex", "group", "eng")
	webB, openB := scopedGRN("globex", "project", "web"), scopedGRN("globex", "project", "open")

	allowA := activePolicy(t, policies, acme, "allow-read", statementDoc("allow-read", wire.EffectAllow, "project:read", "grn:*:*"))
	attachPolicy(t, policies, allowA, adaA)
	denyB := activePolicy(t, policies, globex, "deny-read", statementDoc("deny-read", wire.EffectDeny, "project:read", "grn:*:*"))
	attachPolicy(t, policies, denyB, engB)

	// Unattached, so no subject set stands between this allow and a
	// subject of another org. It names one project, leaving webB an
	// object only an attachment reaches.
	activePolicy(t, policies, globex, "open-door", statementDoc("open-door", wire.EffectAllow, "project:read", openB.String()))

	t.Run("persisted policies stay in their org", func(t *testing.T) {
		active, err := store.ActivePolicies(ctx, acme)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"allow-read"}, sidsOf(active))

		active, err = store.ActivePolicies(ctx, globex)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"deny-read", "open-door"}, sidsOf(active))

		// The lookup is scoped by the org it is asked about and not by
		// the subject: org A's subject collects org B's unattached
		// statement, and none of org A's own.
		statements, err := cache.Statements(ctx, globex, []grn.GRN{adaA})
		require.NoError(t, err)
		require.Len(t, statements, 1)
		assert.Equal(t, "open-door", statements[0].Sid)
	})

	t.Run("a subject attachment decides nothing across the boundary", func(t *testing.T) {
		assert.True(t, checkRead(t, engine, adaA, webA, nil), "org A's attached allow reaches org A's resource")
		assert.False(t, checkRead(t, engine, adaA, webB, nil), "org A's attached allow does not reach org B's resource")
	})

	t.Run("an unattached policy reaches a subject of another org", func(t *testing.T) {
		assert.True(t, checkRead(t, engine, adaA, openB, nil),
			"org B's unattached allow reaches org A's subject; the caller holds this boundary (ADR-0025)")
		assert.False(t, checkRead(t, engine, adaA, webB, nil),
			"that same subject reaches nothing org B's unattached allow does not name")
	})

	t.Run("a group attachment decides nothing across the boundary", func(t *testing.T) {
		assert.False(t, checkRead(t, engine, adaB, webB, []grn.GRN{engB}, roleAllow("globex")),
			"org B's group-attached deny overrides a role allow inside org B")
		assert.True(t, checkRead(t, engine, adaB, webA, []grn.GRN{engB}, roleAllow("acme")),
			"org B's group-attached deny does not reach org A's resource")
	})

	t.Run("role statements stay inside the org that granted them", func(t *testing.T) {
		assert.False(t, checkRead(t, engine, adaB, webB, nil, roleAllow("acme")),
			"a role allow scoped to org A grants nothing on org B's resource")
	})

	t.Run("a write in one org leaves the other org's cache intact", func(t *testing.T) {
		// Both orgs are cached by now: the checks above filled them.
		require.True(t, checkRead(t, engine, adaA, webB, nil, roleAllow("globex")))

		// Unattached, this policy denies every subject of org A.
		activePolicy(t, policies, acme, "deny-all", statementDoc("deny-all", wire.EffectDeny, "project:read", "grn:*:*"))

		assert.False(t, checkRead(t, engine, adaA, webA, nil), "org A's new deny drops org A's cached allow")
		assert.True(t, checkRead(t, engine, adaA, webB, nil, roleAllow("globex")),
			"org A's new deny changes no decision about org B's resource")
	})

	t.Run("a cross-org attachment is rejected", func(t *testing.T) {
		_, err := policies.AttachPolicy(ctx, &authzv1.AttachPolicyRequest{PolicyGrn: allowA, SubjectGrn: engB.String()})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))

		attachments, err := policies.ListPolicyAttachments(ctx, &authzv1.ListPolicyAttachmentsRequest{PolicyGrn: allowA})
		require.NoError(t, err)
		assert.Len(t, attachments.GetAttachments(), 1, "the rejected attachment was not stored")
	})
}

// orgRootGRN returns the root-owned GRN of the named organization.
func orgRootGRN(name string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", name)
}

// scopedGRN names a resource owned by an arbitrary org, where grnOf is
// fixed to "acme".
func scopedGRN(orgName, serviceType, id string) grn.GRN {
	return grn.New(orgName, grn.DefaultProject, serviceType, id)
}

// activePolicy creates and activates a policy in orgGRN through the
// lifecycle service, so the cache is dropped the way a production write
// drops it. It returns the policy GRN in wire form.
func activePolicy(t *testing.T, srv *authz.PolicyGRPCServer, orgGRN grn.GRN, name string, doc authz.PolicyDocument) string {
	t.Helper()
	ctx := context.Background()
	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	created, err := srv.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      name,
		Document:  string(raw),
		CreatedBy: scopedGRN(orgGRN.Identifier, "user", "ada").String(),
	})
	require.NoError(t, err)
	policyGRN := created.GetPolicy().GetGrn()
	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	return policyGRN
}

// attachPolicy attaches policyGRN to subject through the lifecycle
// service.
func attachPolicy(t *testing.T, srv *authz.PolicyGRPCServer, policyGRN string, subject grn.GRN) {
	t.Helper()
	_, err := srv.AttachPolicy(context.Background(), &authzv1.AttachPolicyRequest{
		PolicyGrn:  policyGRN,
		SubjectGrn: subject.String(),
	})
	require.NoError(t, err)
}

// sidsOf names the policies an org lookup returned, by the Sid of each
// one's single statement. ActivePolicies orders by policy GRN, which
// the fixture does not choose, so an org's policies are compared as a
// set.
func sidsOf(policies []authz.ActivePolicy) []string {
	sids := make([]string, 0, len(policies))
	for _, p := range policies {
		sids = append(sids, p.Document.Statement[0].Sid)
	}
	return sids
}

// roleAllow is the role-derived allow the api server passes for a
// caller, scoped to the org whose roles granted it (ADR-0007).
func roleAllow(orgName string) *authzv1.Statement {
	return &authzv1.Statement{
		Sid:      "role-" + orgName,
		Effect:   authzv1.Effect_EFFECT_ALLOW,
		Action:   []string{"project:*"},
		Resource: []string{"grn:" + orgName + ":*"},
	}
}

// checkRead asks the engine whether subject may read object, carrying
// the groups the caller belongs to and its role-derived statements.
// Every isolation case turns on the org segments, so the action is
// fixed to project:read.
func checkRead(t *testing.T, engine *authz.GRPCServer, subject, object grn.GRN, groups []grn.GRN, roles ...*authzv1.Statement) bool {
	t.Helper()
	req := &authzv1.CheckRequest{
		SubjectGrn: subject.String(),
		ObjectGrn:  object.String(),
		Action:     "project:read",
		Statements: roles,
	}
	for _, g := range groups {
		req.SubjectGroups = append(req.SubjectGroups, g.String())
	}
	resp, err := engine.Check(context.Background(), req)
	require.NoError(t, err)
	return resp.GetAllowed()
}

// TestCertificateAuthority_RoundTrip issues against real Postgres: an
// organization's CA is created by the first certificate that needs it,
// the row holds the private key only in sealed form, and a certificate
// signed by one organization does not chain against another's.
func TestCertificateAuthority_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	acme := orgRootGRN("acme")
	other := orgRootGRN("other")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(other, "other")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	store := ca.NewPostgresStore(p)
	svc, err := ca.NewService(store, sealer, ca.Config{})
	require.NoError(t, err)

	_, err = store.Get(ctx, acme)
	assert.ErrorIs(t, err, ca.ErrAuthorityNotFound, "a CA exists only once something needs one")

	subject := scopedGRN("acme", "agent", "bot-1")
	cert, err := svc.Issue(ctx, subject)
	require.NoError(t, err)

	authority, err := store.Get(ctx, acme)
	require.NoError(t, err)
	assert.Equal(t, acme, authority.Organization())
	assert.Equal(t, subject.String(), leafOf(t, cert).URIs[0].String())
	require.NoError(t, chainsTo(t, cert, authority))

	// What a database dump yields is the sealed form: readable through
	// the sealer that produced it, and unusable otherwise — it is not a
	// key, and it does not open under a key that never sealed it.
	var sealed []byte
	require.NoError(t, p.QueryRow(ctx,
		`SELECT sealed_key FROM certificate_authorities WHERE org_grn = $1`, acme.String()).Scan(&sealed))
	opened, err := sealer.Open(ctx, []byte(acme.String()), sealed)
	require.NoError(t, err)
	_, err = x509.ParsePKCS8PrivateKey(opened)
	require.NoError(t, err)
	_, err = x509.ParsePKCS8PrivateKey(sealed)
	assert.Error(t, err, "the stored bytes parse as a private key")
	strangerKey, err := ca.NewAESGCMSealer(make([]byte, ca.KeySize))
	require.NoError(t, err)
	_, err = strangerKey.Open(ctx, []byte(acme.String()), sealed)
	assert.Error(t, err, "the stored bytes open under a key-encryption key that never sealed them")

	// The organization's second agent takes the CA the first created.
	sibling, err := svc.Issue(ctx, scopedGRN("acme", "agent", "bot-2"))
	require.NoError(t, err)
	assert.Equal(t, cert.IssuerPEM(), sibling.IssuerPEM())

	// Another organization gets a CA of its own, and neither trust root
	// accepts the other's certificate (ADR-0014).
	foreign, err := svc.Issue(ctx, scopedGRN("other", "agent", "bot-1"))
	require.NoError(t, err)
	foreignAuthority, err := store.Get(ctx, other)
	require.NoError(t, err)
	require.NoError(t, chainsTo(t, foreign, foreignAuthority))
	assert.Error(t, chainsTo(t, cert, foreignAuthority))
	assert.Error(t, chainsTo(t, foreign, authority))

	// A CA belongs to an organization that exists, and no CA is written
	// for one that does not.
	_, err = svc.Issue(ctx, scopedGRN("ghost", "agent", "bot-1"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	var authorities int
	require.NoError(t, p.QueryRow(ctx, `SELECT count(*) FROM certificate_authorities`).Scan(&authorities))
	assert.Equal(t, 2, authorities)

	// Write access to the database is not enough to move a trust root:
	// an authority row copied onto another organization carries a
	// sealed key that does not open there, so the copy signs nothing.
	copycat := orgRootGRN("copycat")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(copycat, "copycat")))
	_, err = p.Exec(ctx, `INSERT INTO certificate_authorities (org_grn, certificate, sealed_key, not_after, created_at)
		SELECT $1, certificate, sealed_key, not_after, created_at
		FROM certificate_authorities WHERE org_grn = $2`, copycat.String(), acme.String())
	require.NoError(t, err)

	_, err = svc.Issue(ctx, scopedGRN("copycat", "agent", "bot-1"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindDataLoss))

	assert.ErrorIs(t, store.Create(ctx, authority), ca.ErrAuthorityAlreadyExists)
}

// leafOf parses the issued certificate.
func leafOf(t *testing.T, cert *ca.Certificate) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(cert.CertificatePEM())
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	require.Len(t, leaf.URIs, 1)
	return leaf
}

// chainsTo verifies the certificate against one organization's CA, the
// way a listener requiring client auth would.
func chainsTo(t *testing.T, cert *ca.Certificate, authority *ca.Authority) error {
	t.Helper()
	root, err := x509.ParseCertificate(authority.Certificate())
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(root)
	_, err = leafOf(t, cert).Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	return err
}

// TestMessageStore_RoundTrip drives the queue ADR-0013 rests on: a
// message is stored once per sender key, handed to one reader, and
// stays out of the queue once that reader reports it delivered. No
// agent row exists here — garam stores a message before it can route
// one, so the queue references none.
func TestMessageStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := message.NewPostgresStore(p)
	bot, elsewhere := grnOf("agent", "bot"), grnOf("agent", "elsewhere")
	ada := grnOf("user", "ada")
	// Wide enough that nothing here is refused for want of attempts;
	// what the bound does is TestMessageStore_StopsRedeliveringAnExhaustedMessage.
	const attempts = 8

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	stored, err := s.Enqueue(ctx, m)
	require.NoError(t, err)
	assert.Equal(t, m.ID(), stored.ID())
	assert.Equal(t, "key-1", stored.IdempotencyKey(), "the key the sender suppressed duplicates with comes back")
	assert.WithinDuration(t, m.CreatedAt(), stored.CreatedAt(), time.Microsecond)

	retried, err := message.New(bot, ada, "key-1", "hello again")
	require.NoError(t, err)
	again, err := s.Enqueue(ctx, retried)
	require.NoError(t, err)
	assert.Equal(t, m.ID(), again.ID(), "a repeated sender key returns the stored message")
	assert.Equal(t, "hello", again.Body(), "and stores nothing new")

	// The queue is a table, not a connection: a store sharing no state
	// with the one that enqueued claims the message, and dropping every
	// pooled connection afterwards leaves the claim standing.
	reader := message.NewPostgresStore(p)
	claim, err := reader.Claim(ctx, bot, time.Minute, attempts)
	require.NoError(t, err)
	assert.Equal(t, m.ID(), claim.Message().ID())
	assert.Equal(t, "hello", claim.Message().Body())
	assert.Equal(t, ada, claim.Message().Sender())
	assert.Equal(t, 1, claim.Message().Attempts(), "handing the message out is the first attempt")
	p.Reset()

	_, err = s.Claim(ctx, bot, time.Minute, attempts)
	assert.ErrorIs(t, err, message.ErrNoMessageReady, "a held message is nobody else's")
	_, err = s.Claim(ctx, elsewhere, time.Minute, attempts)
	assert.ErrorIs(t, err, message.ErrNoMessageReady, "and belongs to the recipient it is addressed to")

	// The claim is a fence rather than a secret: another recipient's
	// queue does not complete under it even where its token is known.
	assert.ErrorIs(t, s.Complete(ctx, elsewhere, claim.ID()), message.ErrStaleClaim)

	require.NoError(t, reader.Complete(ctx, bot, claim.ID()))
	// A reader retries what it could not confirm, so completing twice
	// reports the same delivery rather than failing.
	require.NoError(t, reader.Complete(ctx, bot, claim.ID()))
	_, err = s.Claim(ctx, bot, time.Minute, attempts)
	assert.ErrorIs(t, err, message.ErrNoMessageReady, "a delivered message is not handed out again")

	// The key outlives delivery, so a retry arriving after it neither
	// stores a message nor requeues the delivered one.
	late, err := message.New(bot, ada, "key-1", "hello again")
	require.NoError(t, err)
	afterDelivery, err := s.Enqueue(ctx, late)
	require.NoError(t, err)
	assert.Equal(t, m.ID(), afterDelivery.ID())
	_, err = s.Claim(ctx, bot, time.Minute, attempts)
	assert.ErrorIs(t, err, message.ErrNoMessageReady)
}

// TestMessageStore_ClaimIsExclusive puts several readers on one
// recipient's queue at once: every message is handed out, and none is
// handed to two readers.
func TestMessageStore_ClaimIsExclusive(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	const queued, readers = 8, 8
	writer := message.NewPostgresStore(p)
	for i := range queued {
		m, err := message.New(bot, ada, fmt.Sprintf("key-%d", i), "hello")
		require.NoError(t, err)
		_, err = writer.Enqueue(ctx, m)
		require.NoError(t, err)
	}

	// Every reader waits for the last of them to be ready, so the claims
	// contend rather than run one after another.
	var (
		start   = make(chan struct{})
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed []message.ID
	)
	wg.Add(readers)
	for range readers {
		go func() {
			defer wg.Done()
			s := message.NewPostgresStore(p)
			<-start
			for {
				// The attempt bound is wide enough to hand every message
				// to every reader, so what keeps a message to one of them
				// is the claim and not the count.
				c, err := s.Claim(ctx, bot, time.Minute, readers)
				if errors.Is(err, message.ErrNoMessageReady) {
					return
				}
				if !assert.NoError(t, err) {
					return
				}
				mu.Lock()
				claimed = append(claimed, c.Message().ID())
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Len(t, claimed, queued, "every queued message was handed out")
	held := make(map[message.ID]struct{}, len(claimed))
	for _, id := range claimed {
		held[id] = struct{}{}
	}
	assert.Len(t, held, queued, "no message was handed to two readers")
}

// TestMessageStore_LapsedClaimIsHandedOn covers the reader that claims
// a message and then dies: the lease runs out, the next reader is given
// the message, and the claim it replaced completes nothing.
func TestMessageStore_LapsedClaimIsHandedOn(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := message.NewPostgresStore(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, m)
	require.NoError(t, err)

	lapsing, err := s.Claim(ctx, bot, 100*time.Millisecond, 4)
	require.NoError(t, err)

	// What is waited for is what is asserted: the claim that succeeds,
	// not the lease slept through. Only one attempt can succeed, because
	// it takes the message for a minute.
	var handedOn *message.Claim
	require.Eventually(t, func() bool {
		c, err := s.Claim(ctx, bot, time.Minute, 4)
		if err != nil {
			return false
		}
		handedOn = c
		return true
	}, 5*time.Second, 10*time.Millisecond, "a lapsed claim leaves the message claimable")

	require.NotNil(t, handedOn)
	assert.Equal(t, m.ID(), handedOn.Message().ID())
	assert.NotEqual(t, lapsing.ID(), handedOn.ID(), "the message is handed on under a claim of its own")
	assert.Equal(t, 2, handedOn.Message().Attempts(), "the hand-on is the second attempt")

	_, err = s.Renew(ctx, bot, lapsing.ID(), time.Minute, time.Hour)
	assert.ErrorIs(t, err, message.ErrStaleClaim,
		"the reader the message was taken from renews nothing")
	assert.ErrorIs(t, s.Complete(ctx, bot, lapsing.ID()), message.ErrStaleClaim,
		"the reader the message was taken from completes nothing")
	require.NoError(t, s.Complete(ctx, bot, handedOn.ID()))
}

// TestMessageStore_RenewalHoldsAClaimPastItsLeaseButNotPastTheCeiling
// covers the turn that outruns its lease. Renewing keeps the message
// out of every other reader's reach past the lease it was claimed
// under; the ceiling is measured from the moment the claim was taken
// and is not renewable away, so a holder that renews forever loses the
// claim all the same (ADR-0018) rather than leaving a message neither
// completed nor redelivered.
func TestMessageStore_RenewalHoldsAClaimPastItsLeaseButNotPastTheCeiling(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := message.NewPostgresStore(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")
	// The lease is what the claim would lapse under; the ceiling is how
	// far renewals may carry it. Both are wide enough that a stalled
	// machine renews late rather than failing.
	const (
		lease   = 2 * time.Second
		ceiling = 5 * time.Second
	)

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, m)
	require.NoError(t, err)

	held, err := s.Claim(ctx, bot, lease, 4)
	require.NoError(t, err)

	// Renew until the lease it was claimed under has run out, which is
	// the condition the assertion below turns on: what holds the message
	// from here is renewal and nothing else.
	renewedUntil := time.Now().Add(lease + 500*time.Millisecond)
	var renewed *message.Claim
	for time.Now().Before(renewedUntil) {
		renewed, err = s.Renew(ctx, bot, held.ID(), lease, ceiling)
		require.NoError(t, err, "a claim renewed inside its lease is still the holder's")
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, renewed)
	assert.True(t, renewed.ExpiresAt().After(held.ExpiresAt()), "renewal carried the claim past where it started")
	assert.Equal(t, held.ID(), renewed.ID(), "renewal keeps the hold rather than taking a new one")

	_, err = s.Claim(ctx, bot, time.Minute, 4)
	assert.ErrorIs(t, err, message.ErrNoMessageReady,
		"a renewed claim is nobody else's, past the lease it was claimed under")

	// The ceiling is the backstop: renewals keep being asked for and the
	// claim lapses on schedule anyway.
	var handedOn *message.Claim
	require.Eventually(t, func() bool {
		_, _ = s.Renew(ctx, bot, held.ID(), lease, ceiling)
		c, err := s.Claim(ctx, bot, time.Minute, 4)
		if err != nil {
			return false
		}
		handedOn = c
		return true
	}, 10*time.Second, 200*time.Millisecond, "the ceiling holds a renewal that would carry the claim forever")

	require.NotNil(t, handedOn)
	assert.Equal(t, m.ID(), handedOn.Message().ID())
	_, err = s.Renew(ctx, bot, held.ID(), lease, ceiling)
	assert.ErrorIs(t, err, message.ErrStaleClaim, "a claim carried past its ceiling renews nothing")
}

// TestMessageStore_StopsRedeliveringAnExhaustedMessage covers the
// message that fails the same way every time. Handing it out is the
// attempt, since a turn that failed and a turn that never started are
// indistinguishable from here (ADR-0018), so a message that has been
// handed out its bounded number of times is handed out no more.
func TestMessageStore_StopsRedeliveringAnExhaustedMessage(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := message.NewPostgresStore(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")
	const attempts = 2
	const lease = 200 * time.Millisecond

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, m)
	require.NoError(t, err)

	first, err := s.Claim(ctx, bot, lease, attempts)
	require.NoError(t, err)
	assert.Equal(t, 1, first.Message().Attempts())

	// Nobody completes it, so the lease is what ends each attempt.
	var second *message.Claim
	require.Eventually(t, func() bool {
		c, err := s.Claim(ctx, bot, lease, attempts)
		if err != nil {
			return false
		}
		second = c
		return true
	}, 5*time.Second, 20*time.Millisecond, "a lapsed claim leaves the message claimable")
	require.NotNil(t, second)
	assert.Equal(t, 2, second.Message().Attempts())

	// Its attempts spent, the message is handed to nobody however long
	// anyone waits — and the second claim's lease runs out inside this
	// window, so what withholds it is the count rather than a lease.
	require.Never(t, func() bool {
		_, err := s.Claim(ctx, bot, time.Minute, attempts)
		return err == nil
	}, 2*time.Second, 50*time.Millisecond, "an exhausted message was handed out again")

	// The same message under a wider bound is claimable, so what stopped
	// it was the count and not anything else about the row.
	third, err := s.Claim(ctx, bot, time.Minute, attempts+1)
	require.NoError(t, err)
	assert.Equal(t, m.ID(), third.Message().ID())
	assert.Equal(t, 3, third.Message().Attempts())
}

// TestMessageService_ClaimWaitsForTheMessageThatArrives drives the long
// poll over the real queue: the claim is made before there is anything
// to claim, and it answers with the message a sender stores while it
// waits. What it waits on is a message becoming claimable, which is the
// condition the answer is read from — a poll returning early answers
// that nothing is ready, and this one does not.
func TestMessageService_ClaimWaitsForTheMessageThatArrives(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	store := message.NewPostgresStore(p)
	cfg := briefDelivery()
	s, err := message.NewService(store, cfg)
	require.NoError(t, err)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	type answer struct {
		claim *message.Claim
		err   error
	}
	answered := make(chan answer, 1)
	start := time.Now()
	go func() {
		c, err := s.Claim(ctx, bot, bot)
		answered <- answer{claim: c, err: err}
	}()

	// Long enough that a poll answering an empty queue at once has
	// already answered, and short enough to leave the wait running.
	time.Sleep(10 * cfg.PollInterval)
	select {
	case got := <-answered:
		require.Fail(t, "the poll answered before there was a message", "%+v", got)
	default:
	}

	sent, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = store.Enqueue(ctx, sent)
	require.NoError(t, err)

	select {
	case got := <-answered:
		require.NoError(t, got.err)
		require.NotNil(t, got.claim)
		assert.Equal(t, sent.ID(), got.claim.Message().ID())
		assert.Equal(t, "hello", got.claim.Message().Body())
		assert.Less(t, time.Since(start), cfg.Wait,
			"the poll waited its whole wait out rather than answering with the message that arrived")
	case <-time.After(cfg.Wait + 5*time.Second):
		require.Fail(t, "the poll never answered")
	}
}

// TestMessageStore_CompletionDropsTheBodyAndKeepsTheKey is ADR-0034's
// first half in one round trip: completing a message drops its body and
// nothing else, so the row that stays still suppresses the sender's
// retry. The body is read from the table rather than through the store,
// because nothing reads a completed message back — a getter that
// rendered a dropped body as empty would answer the same for a body
// dropped and a body never read.
func TestMessageStore_CompletionDropsTheBodyAndKeepsTheKey(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := message.NewPostgresStore(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, m)
	require.NoError(t, err)

	claim, err := s.Claim(ctx, bot, time.Minute, 4)
	require.NoError(t, err)
	assert.Equal(t, "hello", claim.Message().Body(), "the body is there to be dropped")
	require.NoError(t, s.Complete(ctx, bot, claim.ID()))

	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM messages WHERE id = $1 AND body IS NULL", string(m.ID())),
		"the row stays and its body is gone")

	// The key outlives the body, so a retry after delivery neither
	// stores a message nor requeues the delivered one.
	late, err := message.New(bot, ada, "key-1", "hello again")
	require.NoError(t, err)
	afterDelivery, err := s.Enqueue(ctx, late)
	require.NoError(t, err)
	assert.Equal(t, m.ID(), afterDelivery.ID(), "a retry after completion is still suppressed")
	_, err = s.Claim(ctx, bot, time.Minute, 4)
	assert.ErrorIs(t, err, message.ErrNoMessageReady)
}

// TestMessageStore_EnqueueSweepsWhatOutlivedTheWindow is ADR-0034's
// second half: storing a message is the queue's one write, so the sweep
// rides it and deletes an undelivered message older than the retention
// window whole. Two controls stand beside the expired one, written and
// aged before the sweeping call: a message still inside the window,
// which a sweep reading age alone would take, and a completed one
// equally old, which a sweep reading delivery alone would take.
func TestMessageStore_EnqueueSweepsWhatOutlivedTheWindow(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := message.NewPostgresStore(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	post := func(key string) message.ID {
		m, err := message.New(bot, ada, key, "hello")
		require.NoError(t, err)
		stored, err := s.Enqueue(ctx, m)
		require.NoError(t, err)
		return stored.ID()
	}
	// age backdates the row, because the window is thirty days and the
	// test is not.
	age := func(id message.ID, by time.Duration) {
		_, err := p.Exec(ctx,
			"UPDATE messages SET created_at = now() - make_interval(secs => $1) WHERE id = $2",
			by.Seconds(), string(id))
		require.NoError(t, err)
	}
	present := func(id message.ID) int {
		return countRows(t, p, "SELECT count(*) FROM messages WHERE id = $1", string(id))
	}

	// The delivered one is posted first so the claim below takes it: a
	// claim hands out the oldest message on the queue.
	delivered := post("key-delivered")
	claim, err := s.Claim(ctx, bot, time.Minute, 4)
	require.NoError(t, err)
	require.Equal(t, delivered, claim.Message().ID(), "the oldest message is the one claimed")
	require.NoError(t, s.Complete(ctx, bot, claim.ID()))

	expired := post("key-expired")
	standing := post("key-standing")

	age(expired, message.RetentionWindow+time.Hour)
	age(delivered, message.RetentionWindow+time.Hour)
	age(standing, message.RetentionWindow-time.Hour)
	// The aged row survives its own age: nothing has swept since it
	// crossed the window, so what removes it below is the sweep and not
	// the ageing.
	require.Equal(t, 1, present(expired))

	fresh := post("key-fresh")

	assert.Equal(t, 0, present(expired), "an undelivered message past the window was swept")
	assert.Equal(t, 1, present(standing), "a message inside the window outlives the sweep")
	assert.Equal(t, 1, present(delivered), "a completed message is kept, age or not")
	assert.Equal(t, 1, present(fresh))
}

// TestMessage_BodyAndCompletionAreOneState covers the schema's copy of
// the lifecycle (ADR-0034): a body lives exactly while the message is
// undelivered, so the two half-states — an undelivered row without a
// body, and a completed one still holding it — are unrepresentable.
// internal/message never writes either, so only a direct statement
// reaches the constraint.
func TestMessage_BodyAndCompletionAreOneState(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	rejected := []struct {
		name string
		stmt string
	}{
		{
			name: "an undelivered message without a body",
			stmt: `INSERT INTO messages (id, recipient_grn, sender_grn, idempotency_key, body, created_at)
				VALUES ('m-nobody', $1, $2, 'key-nobody', NULL, now())`,
		},
		{
			name: "a completed message still holding its body",
			stmt: `INSERT INTO messages (id, recipient_grn, sender_grn, idempotency_key, body, created_at, claim_id, claim_expires_at, claimed_at, completed_at)
				VALUES ('m-kept', $1, $2, 'key-kept', 'hello', now(), 'c-1', now(), now(), now())`,
		},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Exec(ctx, tt.stmt, bot.String(), ada.String())
			assert.Error(t, err)
		})
	}

	// An undelivered message carrying its body is admitted, so the
	// constraint rejects a half-state rather than every row.
	m, err := message.New(bot, ada, "key-whole", "hello")
	require.NoError(t, err)
	_, err = message.NewPostgresStore(p).Enqueue(ctx, m)
	require.NoError(t, err)
}

// briefDelivery is the queue's delivery configuration with the waits
// shortened to what a test can sit through. What each parameter governs
// is unchanged; `garam serve machine` runs on the defaults.
func briefDelivery() message.Config {
	return message.Config{
		Lease:        2 * time.Second,
		LeaseCeiling: 5 * time.Second,
		Attempts:     2,
		Wait:         2 * time.Second,
		PollInterval: 20 * time.Millisecond,
	}
}

// memberGroups reads the groups a machine identity belongs to
// (machineapi.Groups) — the adapter the composition root builds, in the
// form this suite can hold.
type memberGroups struct{ store org.Store }

func (m memberGroups) List(ctx context.Context, subject grn.GRN) ([]grn.GRN, error) {
	return m.store.ListGroupsByMember(ctx, orgRootGRN(subject.Organization), subject)
}

// certificates is the certificate authority as the agent domain signs
// through it (agent.Issuer) — the adapter the composition root builds,
// in the form this suite can hold.
type certificates struct{ issuer ca.Service }

func (c certificates) Issue(ctx context.Context, subject grn.GRN) (*agent.Certificate, error) {
	issued, err := c.issuer.Issue(ctx, subject)
	if err != nil {
		return nil, err
	}
	return &agent.Certificate{
		CertificatePEM: issued.CertificatePEM(),
		PrivateKeyPEM:  issued.PrivateKeyPEM(),
		IssuerPEM:      issued.IssuerPEM(),
		NotAfter:       issued.NotAfter(),
	}, nil
}

// machineTrustRoots reads the authority a presented certificate has to
// chain to out of the ones Postgres holds.
type machineTrustRoots struct{ store ca.Store }

func (r machineTrustRoots) Get(ctx context.Context, subject grn.GRN) ([]byte, error) {
	authority, err := r.store.Get(ctx, orgRootGRN(subject.Organization))
	if err != nil {
		return nil, err
	}
	return authority.Certificate(), nil
}

// loopbackCertificate is what the listener terminates TLS with. What
// garam presents is not what this test is about, so it is self-signed
// and the client below does not verify it.
func loopbackCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "garam machine listener"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// policyEngine serves the authz engine over gRPC against the policies
// Postgres holds, and returns the lifecycle service an organization
// writes them through beside the seam the machine listener asks
// through. It is the pair of processes `garam serve authz` and `garam
// serve machine` are, over one database.
func policyEngine(t *testing.T, p *pgxpool.Pool) (*authz.PolicyGRPCServer, machineapi.Evaluator) {
	t.Helper()
	store := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(store)

	srv := grpc.NewServer()
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer(cache))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	// Writes made through this service drop the org's cache entry
	// directly, as they do in the engine's own process.
	return authz.NewPolicyGRPCServer(store, cache), machineapi.NewGRPCEvaluator(conn)
}

// machineListener serves the machine surface over the stores Postgres
// holds and returns its URL. The parts are the ones `garam serve
// machine` assembles: the same generated router, the same listener,
// authorities sealed under the same key, and the engine reached over
// gRPC.
func machineListener(t *testing.T, p *pgxpool.Pool, sealer ca.Sealer, policies machineapi.Evaluator) string {
	t.Helper()
	authorities := ca.NewPostgresStore(p)
	issuer, err := ca.NewService(authorities, sealer, ca.Config{})
	require.NoError(t, err)
	agents := agent.NewService(agent.NewPostgresStore(p), certificates{issuer: issuer})
	messages, err := message.NewService(message.NewPostgresStore(p), briefDelivery())
	require.NoError(t, err)
	// The watches this surface counts are opened by the process serving
	// browsers, so what it holds is the store they are written to and the
	// relay it announces forwarded events on (ADR-0017, ADR-0022).
	watches, err := watch.NewService(watch.NewPostgresStore(p), watch.NewPostgresRelay(p), watch.Config{})
	require.NoError(t, err)

	srv := machine.NewServer(
		machine.NewService(machineTrustRoots{store: authorities}),
		loopbackCertificate(t),
		machinegen.Handler(machineapi.NewServer(agents, messages, watches, memberGroups{store: org.NewPostgresStore(p)}, policies)),
	)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	return "https://" + lis.Addr().String()
}

// installed returns the certificate garam signs for subject as the
// process holding it presents one. An operator's own first certificate
// arrives out of band (ADR-0015); an agent's comes from the surface
// under test.
func installed(t *testing.T, issuer ca.Service, subject grn.GRN) tls.Certificate {
	t.Helper()
	issued, err := issuer.Issue(context.Background(), subject)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair(issued.CertificatePEM(), issued.PrivateKeyPEM())
	require.NoError(t, err)
	return pair
}

// machineClient presents pair on every request it makes, the way a
// process holding one certificate reaches this listener.
func machineClient(t *testing.T, pair tls.Certificate) *http.Client {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{pair},
	}}}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// machineRequest performs one request and returns the status and body.
func machineRequest(t *testing.T, client *http.Client, method, url string) (int, []byte) {
	t.Helper()
	return machineSend(t, client, method, url, "")
}

// machineSend performs one request carrying body and returns the status
// and what came back.
func machineSend(t *testing.T, client *http.Client, method, url, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, answer
}

// submissionTo is the body a sender posts to address recipient.
func submissionTo(recipient grn.GRN, key string) string {
	return `{"recipient":"` + recipient.String() + `","idempotencyKey":"` + key + `","body":"hello"}`
}

// detachPolicy removes policyGRN's attachment to subject through the
// lifecycle service.
func detachPolicy(t *testing.T, srv *authz.PolicyGRPCServer, policyGRN string, subject grn.GRN) {
	t.Helper()
	_, err := srv.DetachPolicy(context.Background(), &authzv1.DetachPolicyRequest{
		PolicyGrn:  policyGRN,
		SubjectGrn: subject.String(),
	})
	require.NoError(t, err)
}

// detachRefused asserts the lifecycle refuses to remove policyGRN's
// attachment to subject: it is an active policy's last.
func detachRefused(t *testing.T, srv *authz.PolicyGRPCServer, policyGRN string, subject grn.GRN) {
	t.Helper()
	_, err := srv.DetachPolicy(context.Background(), &authzv1.DetachPolicyRequest{
		PolicyGrn:  policyGRN,
		SubjectGrn: subject.String(),
	})
	require.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// archivePolicy retires policyGRN through the lifecycle service, which
// takes it out of every check.
func archivePolicy(t *testing.T, srv *authz.PolicyGRPCServer, policyGRN string) {
	t.Helper()
	_, err := srv.ArchivePolicy(context.Background(), &authzv1.ArchivePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
}

// pairOf is the certificate the surface issued, as the process it was
// issued for presents it.
func pairOf(t *testing.T, issued machinegen.AgentCertificate) tls.Certificate {
	t.Helper()
	pair, err := tls.X509KeyPair([]byte(issued.CertificatePem), []byte(issued.PrivateKeyPem))
	require.NoError(t, err)
	return pair
}

// TestMachineSurface_OperatorCreatesAnAgentThatThenAuthenticates
// carries ADR-0015's operator path end to end against real Postgres:
// an operator authenticates with its own certificate, creates an
// agent, obtains that agent's certificate, and the agent authenticates
// with it on the same listener. The certificate is carried back in
// because a surface that issues and a listener that verifies can each
// be right while the pair is not.
//
// Every refusal sits beside an answer, and each operator refused one
// certificate holds another: what refuses is the assignment rather
// than a certificate that authenticated nobody.
func TestMachineSurface_OperatorCreatesAnAgentThatThenAuthenticates(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	for _, name := range []string{"acme", "globex"} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN(name), name)))
	}

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(ca.NewPostgresStore(p), sealer, ca.Config{})
	require.NoError(t, err)

	_, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)
	assignments := agent.NewPostgresStore(p)

	operatorGRN := scopedGRN("acme", agent.ServiceTypeOperator, "one")
	operator := machineClient(t, installed(t, outOfBand, operatorGRN))

	status, body := machineRequest(t, operator, http.MethodPost, url+"/agents")
	require.Equal(t, http.StatusCreated, status, string(body))
	var created machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal(body, &created))
	assert.Equal(t, operatorGRN.String(), created.Operator)
	assert.Equal(t, 1, created.Epoch)

	agentGRN, err := grn.Parse(created.Grn)
	require.NoError(t, err)
	assert.Equal(t, "acme", agentGRN.Organization, "garam minted it in the operator's organization")
	assert.Equal(t, agent.ServiceType, agentGRN.ServiceType)

	stored, err := assignments.GetAssignment(ctx, agentGRN)
	require.NoError(t, err)
	assert.Equal(t, operatorGRN, stored.Operator())
	assert.Equal(t, created.Epoch, stored.Epoch())

	status, body = machineRequest(t, operator, http.MethodPost, url+"/agents/"+created.Grn+"/certificate")
	require.Equal(t, http.StatusCreated, status, string(body))
	var issued machinegen.AgentCertificate
	require.NoError(t, json.Unmarshal(body, &issued))
	assert.Equal(t, created.Grn, issued.Grn)
	assert.True(t, issued.NotAfter.After(time.Now()), "a certificate that is already expired")

	// The two halves join here: what this surface issued authenticates
	// on the listener that verifies it, as the agent it names.
	provisioned := machineClient(t, pairOf(t, issued))
	status, body = machineRequest(t, provisioned, http.MethodGet, url+"/identity")
	require.Equal(t, http.StatusOK, status, string(body))
	assert.JSONEq(t, `{"grn":"`+created.Grn+`"}`, string(body))

	// That certificate authenticates and still creates nothing: an
	// agent is not an operator.
	status, _ = machineRequest(t, provisioned, http.MethodPost, url+"/agents")
	assert.Equal(t, http.StatusForbidden, status)

	// Another organization's operator authenticates against its own
	// authority, holds the agent it created there, and is refused the
	// one it does not hold.
	strangerGRN := scopedGRN("globex", agent.ServiceTypeOperator, "one")
	stranger := machineClient(t, installed(t, outOfBand, strangerGRN))
	status, body = machineRequest(t, stranger, http.MethodPost, url+"/agents")
	require.Equal(t, http.StatusCreated, status, string(body))
	var strangers machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal(body, &strangers))
	status, _ = machineRequest(t, stranger, http.MethodPost, url+"/agents/"+strangers.Grn+"/certificate")
	assert.Equal(t, http.StatusCreated, status)
	status, _ = machineRequest(t, stranger, http.MethodPost, url+"/agents/"+created.Grn+"/certificate")
	assert.Equal(t, http.StatusForbidden, status, "another organization's agent")

	// An operator of the agent's own organization that holds nothing is
	// refused the same way.
	siblingGRN := scopedGRN("acme", agent.ServiceTypeOperator, "two")
	sibling := machineClient(t, installed(t, outOfBand, siblingGRN))
	status, _ = machineRequest(t, sibling, http.MethodPost, url+"/agents/"+created.Grn+"/certificate")
	assert.Equal(t, http.StatusForbidden, status, "an agent of its organization it was not assigned")

	// A reassignment raises the epoch, and what an operator may obtain
	// follows the row: the one it replaced is refused from then on, and
	// the one holding the agent now is answered.
	reassigned, err := assignments.Assign(ctx, agentGRN, siblingGRN)
	require.NoError(t, err)
	require.Equal(t, 2, reassigned.Epoch())
	status, _ = machineRequest(t, operator, http.MethodPost, url+"/agents/"+created.Grn+"/certificate")
	assert.Equal(t, http.StatusForbidden, status, "the operator the reassignment replaced")
	status, _ = machineRequest(t, sibling, http.MethodPost, url+"/agents/"+created.Grn+"/certificate")
	assert.Equal(t, http.StatusCreated, status)
}

// provisioned creates an agent through the surface as the calling
// operator and returns its GRN with a client presenting the certificate
// the surface issued for it. It is the two calls
// TestMachineSurface_OperatorCreatesAnAgentThatThenAuthenticates covers
// in full, as the tests that need an authenticated agent make them.
func provisioned(t *testing.T, url string, operator *http.Client) (grn.GRN, *http.Client) {
	t.Helper()
	status, body := machineRequest(t, operator, http.MethodPost, url+"/agents")
	require.Equal(t, http.StatusCreated, status, string(body))
	var created machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal(body, &created))

	status, body = machineRequest(t, operator, http.MethodPost, url+"/agents/"+created.Grn+"/certificate")
	require.Equal(t, http.StatusCreated, status, string(body))
	var issued machinegen.AgentCertificate
	require.NoError(t, json.Unmarshal(body, &issued))

	agentGRN, err := grn.Parse(created.Grn)
	require.NoError(t, err)
	return agentGRN, machineClient(t, pairOf(t, issued))
}

// TestMachineSurface_AnAgentActsOnItsOwnQueueAndNoOther carries a
// message end to end over the listener an agent authenticates on: one
// agent posts, the addressed agent claims it, holds it while it works,
// and reports it delivered, after which the message leaves the queue.
//
// Every refusal sits beside the same call made by the agent the message
// is addressed to. Both callers authenticate, both hold certificates
// this listener verifies, and only one of them is the recipient — so
// what refuses is the one comparison rather than a surface that refuses
// whoever asks.
func TestMachineSurface_AnAgentActsOnItsOwnQueueAndNoOther(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(ca.NewPostgresStore(p), sealer, ca.Config{})
	require.NoError(t, err)

	_, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)
	operator := machineClient(t, installed(t, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	queue := url + "/agents/" + recipientGRN.String() + "/claims"

	submission := `{"recipient":"` + recipientGRN.String() + `","idempotencyKey":"key-1","body":"hello"}`
	status, body := machineSend(t, sender, http.MethodPost, url+"/messages", submission)
	require.Equal(t, http.StatusCreated, status, string(body))
	var posted machinegen.Message
	require.NoError(t, json.Unmarshal(body, &posted))
	assert.Equal(t, senderGRN.String(), posted.Sender, "the sender is what the certificate named")
	assert.Equal(t, recipientGRN.String(), posted.Recipient)

	// A sender that could not read the answer sends it again, and one
	// message is stored.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submission)
	require.Equal(t, http.StatusCreated, status, string(body))
	var repeated machinegen.Message
	require.NoError(t, json.Unmarshal(body, &repeated))
	assert.Equal(t, posted.Id, repeated.Id, "a repeated key answers with the message already stored")

	status, body = machineSend(t, sender, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusForbidden, status, string(body))
	assert.Contains(t, string(body), `"kind":"permission_denied"`)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, posted.Id, claimed.Message.Id)
	assert.Equal(t, "hello", claimed.Message.Body)
	assert.Equal(t, 1, claimed.Attempt)
	assert.True(t, claimed.ExpiresAt.After(time.Now()), "a hold that had lapsed before it was answered")

	held := queue + "/" + claimed.Id
	status, body = machineSend(t, sender, http.MethodPost, held+"/renewal", "")
	assert.Equal(t, http.StatusForbidden, status, string(body))
	status, body = machineSend(t, sender, http.MethodPost, held+"/completion", "")
	assert.Equal(t, http.StatusForbidden, status, string(body))

	// The holder keeps its claim while the turn runs.
	status, body = machineSend(t, recipient, http.MethodPost, held+"/renewal", "")
	require.Equal(t, http.StatusOK, status, string(body))
	var renewed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &renewed))
	assert.Equal(t, claimed.Id, renewed.Id, "renewal keeps the hold rather than taking a new one")
	assert.True(t, renewed.ExpiresAt.After(claimed.ExpiresAt), "renewal left the hold where it started")

	status, body = machineSend(t, recipient, http.MethodPost, held+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))
	// A holder that could not read the answer reports it again, and the
	// same delivery comes back rather than a refusal.
	status, body = machineSend(t, recipient, http.MethodPost, held+"/completion", "")
	assert.Equal(t, http.StatusNoContent, status, string(body))

	// The delivered message is handed out no more, so the next poll
	// waits its wait out and answers that there is nothing.
	start := time.Now()
	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body))
	assert.GreaterOrEqual(t, time.Since(start), briefDelivery().Wait,
		"the poll answered an empty queue before its wait ran out")

	// A message addressed to an agent garam has never provisioned is
	// stored and answered with success: rejecting it would destroy the
	// message and tell the sender nothing (ADR-0013). It is in the queue
	// rather than merely answered for, and storing it provisioned
	// nothing.
	ghost := scopedGRN("acme", agent.ServiceType, "0000000000000000")
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages",
		`{"recipient":"`+ghost.String()+`","idempotencyKey":"key-2","body":"nobody is home"}`)
	require.Equal(t, http.StatusCreated, status, string(body))

	stranded, err := message.NewPostgresStore(p).Claim(ctx, ghost, time.Minute, 1)
	require.NoError(t, err)
	assert.Equal(t, "nobody is home", stranded.Message().Body())
	_, err = agent.NewPostgresStore(p).Get(ctx, ghost)
	assert.ErrorIs(t, err, agent.ErrAgentNotFound, "storing the message provisioned an agent")
}

// TestMachineSurface_AnotherOrganizationsAgentReachesNothingOnThisQueue
// carries the same message path across an organization boundary. A
// third agent is provisioned by another organization's operator and
// authenticates on the same listener against its own authority, then
// makes the calls the recipient makes. Each refusal sits beside the
// recipient's own call to that endpoint: the stranger's certificate
// authenticates here, so what refuses is the recipient comparison
// rather than a certificate this listener rejected.
//
// Its post crossed until ADR-0024. What an identity may send is now
// decided by policy, and what its own GRN grants it is its own
// organization, so a message to another organization's agent is
// refused instead of stored — the one call this test used to watch
// cross.
func TestMachineSurface_AnotherOrganizationsAgentReachesNothingOnThisQueue(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	for _, name := range []string{"acme", "globex"} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN(name), name)))
	}

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(ca.NewPostgresStore(p), sealer, ca.Config{})
	require.NoError(t, err)

	policies, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)
	acmeOperator := machineClient(t, installed(t, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	globexOperator := machineClient(t, installed(t, outOfBand, scopedGRN("globex", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, acmeOperator)
	recipientGRN, recipient := provisioned(t, url, acmeOperator)
	strangerGRN, stranger := provisioned(t, url, globexOperator)
	require.Equal(t, "globex", strangerGRN.Organization, "the stranger was minted in the operator's organization")
	queue := url + "/agents/" + recipientGRN.String() + "/claims"

	status, body := machineSend(t, sender, http.MethodPost, url+"/messages",
		`{"recipient":"`+recipientGRN.String()+`","idempotencyKey":"key-1","body":"hello"}`)
	require.Equal(t, http.StatusCreated, status, string(body))
	var posted machinegen.Message
	require.NoError(t, json.Unmarshal(body, &posted))

	status, body = machineSend(t, stranger, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusForbidden, status, string(body))
	assert.Contains(t, string(body), `"kind":"permission_denied"`)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var claimed machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, posted.Id, claimed.Message.Id)
	assert.Equal(t, senderGRN.String(), claimed.Message.Sender, "the sender the certificate named survived the trip")
	assert.Equal(t, "hello", claimed.Message.Body)

	held := queue + "/" + claimed.Id
	status, body = machineSend(t, stranger, http.MethodPost, held+"/renewal", "")
	assert.Equal(t, http.StatusForbidden, status, string(body))
	status, body = machineSend(t, stranger, http.MethodPost, held+"/completion", "")
	assert.Equal(t, http.StatusForbidden, status, string(body))

	status, body = machineSend(t, recipient, http.MethodPost, held+"/renewal", "")
	require.Equal(t, http.StatusOK, status, string(body))

	// Completing the first message empties the queue, so a message the
	// stranger managed to store would be the next one handed out.
	status, body = machineSend(t, recipient, http.MethodPost, held+"/completion", "")
	require.Equal(t, http.StatusNoContent, status, string(body))

	// The post crosses no further than the rest: the recipient is in
	// another organization, which is refused before a policy is read
	// (ADR-0024).
	status, body = machineSend(t, stranger, http.MethodPost, url+"/messages",
		submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusForbidden, status, string(body))
	assert.JSONEq(t, `{"kind":"permission_denied","message":"recipient is in another organization"}`, string(body))

	// No policy the sender's own organization writes carries it across
	// either, because the boundary is decided before a policy is
	// consulted (ADR-0024). The Allow below matches every resource there
	// is and buys nothing.
	reachAll := activePolicy(t, policies, orgRootGRN("acme"), "reach-everything",
		statementDoc("reach-everything", wire.EffectAllow, message.ActionSend, "grn:*:*"))
	attachPolicy(t, policies, reachAll, senderGRN)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages",
		submissionTo(strangerGRN, "key-3"))
	require.Equal(t, http.StatusForbidden, status, string(body))

	// That same Allow is active, attached and matching inside acme, so
	// what refused above is the recipient's organization and not a
	// policy that reaches nothing.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages",
		submissionTo(recipientGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// Nothing refused reached the queue, so the one message waiting is
	// the one that was accepted.
	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	require.NoError(t, json.Unmarshal(body, &claimed))
	assert.Equal(t, senderGRN.String(), claimed.Message.Sender)
	assert.Equal(t, "key-4", claimed.Message.IdempotencyKey)
}

// TestMachineSurface_AnAttachedPolicyDecidesWhatAnAgentSends carries
// ADR-0024 end to end. An organization writes a policy through the
// lifecycle service, attaches it to one of its agents, and the machine
// listener asks the engine over gRPC before it stores anything — the
// two processes `garam serve authz` and `garam serve machine` are, over
// one database.
//
// Three controls sit against the refusals, because a 403 holds as
// readily for a surface that refuses everything: the same sender
// reaching a recipient the Deny does not name, the agent beside it that
// holds no policy reaching the one it does, and the same call once the
// policy is archived. The queue is drained at the end, so what was
// refused is shown to be absent rather than merely unanswered.
//
// The detach step in the middle is the one an operator runs to stop
// restricting the sender. It is refused — the sender's attachment is
// the policy's last, and removing it would apply the Deny to the whole
// organization — so the policy goes on deciding for its subject alone,
// and the agent it was never attached to is never reached.
func TestMachineSurface_AnAttachedPolicyDecidesWhatAnAgentSends(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(ca.NewPostgresStore(p), sealer, ca.Config{})
	require.NoError(t, err)

	policies, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)
	operator := machineClient(t, installed(t, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, operator)
	_, other := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	siblingGRN, _ := provisioned(t, url, operator)

	// An agent nobody has written a policy for reaches its own
	// organization, so the surface starts by accepting the call the
	// policy below refuses.
	status, body := machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusCreated, status, string(body))

	denied := activePolicy(t, policies, acme, "no-talking",
		statementDoc("no-talking", wire.EffectDeny, message.ActionSend, recipientGRN.String()))
	attachPolicy(t, policies, denied, senderGRN)

	// The organization's own rule refused this one, which is a different
	// answer from the boundary refusal above and says so in the body.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusForbidden, status, string(body))
	assert.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, string(body))

	// The Deny names one recipient, so the sender still reaches the
	// agent beside it.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(siblingGRN, "key-3"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// The attachment names one subject, so the agent beside the sender
	// reaches the recipient the sender may not.
	status, body = machineSend(t, other, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// Detaching the policy's last attachment is refused rather than
	// widening the Deny to the whole org: the agent beside the sender
	// goes on reaching the recipient, and the sender goes on being
	// refused, so the refusal changed nothing the policy decides.
	detachRefused(t, policies, denied, senderGRN)
	status, body = machineSend(t, other, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-5"))
	require.Equal(t, http.StatusCreated, status, string(body))
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-6"))
	require.Equal(t, http.StatusForbidden, status, string(body))

	// Archiving is what takes the policy out of every check, so the
	// refusals above were this policy and nothing beside it.
	archivePolicy(t, policies, denied)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-7"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// What the recipient's queue holds is every accepted message and no
	// refused one: a policy refusal stores nothing, where a message
	// garam merely cannot route is stored (ADR-0013).
	queue := url + "/agents/" + recipientGRN.String() + "/claims"
	var keys []string
	for range 4 {
		status, body = machineSend(t, recipient, http.MethodPost, queue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var claimed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &claimed))
		keys = append(keys, claimed.Message.IdempotencyKey)
		status, body = machineSend(t, recipient, http.MethodPost, queue+"/"+claimed.Id+"/completion", "")
		require.Equal(t, http.StatusNoContent, status, string(body))
	}
	assert.Equal(t, []string{"key-1", "key-4", "key-5", "key-7"}, keys)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body), "a refused message reached the queue")
}

// TestMachineSurface_AGroupAttachedPolicyDecidesWhatItsMembersSend
// carries the control the fix turns on against real Postgres and the
// real engine: one policy document, one sender, one recipient, and only
// the attachment target moving. A group is a target an administrator
// can pick and an agent is a member of one the way a user is
// (ADR-0008, ADR-0014), so attached to the sender's group and attached
// to the sender itself the call has to end the same way. It did not:
// the group attachment answered 201 and the direct one 403, which is an
// attachment that reads as a control that is not there.
//
// The agent in no group makes the same call at every step, so what
// refuses is the membership rather than a surface that refuses whoever
// asks.
func TestMachineSurface_AGroupAttachedPolicyDecidesWhatItsMembersSend(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(ca.NewPostgresStore(p), sealer, ca.Config{})
	require.NoError(t, err)

	policies, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)
	operator := machineClient(t, installed(t, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, operator)
	_, outsider := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	siblingGRN, _ := provisioned(t, url, operator)

	engineering := scopedGRN("acme", org.ServiceTypeGroup, "engineering")
	require.NoError(t, orgs.CreateGroup(ctx, org.NewGroup(engineering, "engineering", acme)))
	require.NoError(t, orgs.AddGroupMember(ctx, engineering, senderGRN))
	members, err := orgs.ListGroupMembers(ctx, engineering)
	require.NoError(t, err)
	require.Equal(t, []grn.GRN{senderGRN}, members, "the group holds one of the two senders")

	// An agent nobody has written a policy for reaches its own
	// organization, so the surface starts by accepting the call the
	// group attachment below refuses.
	status, body := machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusCreated, status, string(body))

	denied := activePolicy(t, policies, acme, "no-talking",
		statementDoc("no-talking", wire.EffectDeny, message.ActionSend, recipientGRN.String()))
	attachPolicy(t, policies, denied, engineering)

	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusForbidden, status, string(body))
	assert.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, string(body))

	// The Deny names one recipient, so the member reaches the agent
	// beside it.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(siblingGRN, "key-3"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// The agent that is in no group is answered under the same policy.
	status, body = machineSend(t, outsider, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// The same document attached to the sender itself refuses the same
	// call. Attaching before detaching keeps the policy attached to
	// something throughout: an active policy's last attachment refuses
	// to detach rather than widening the policy to the whole
	// organization.
	attachPolicy(t, policies, denied, senderGRN)
	detachPolicy(t, policies, denied, engineering)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-5"))
	require.Equal(t, http.StatusForbidden, status, string(body))

	// The agent outside the group is answered here too, so what each
	// attachment refused is the subject it named.
	status, body = machineSend(t, outsider, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-6"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// Archiving is what takes the policy out of every check, so both
	// refusals were this document and nothing beside it.
	archivePolicy(t, policies, denied)
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-7"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// What the recipient's queue holds is every accepted message and no
	// refused one.
	queue := url + "/agents/" + recipientGRN.String() + "/claims"
	var keys []string
	for range 4 {
		status, body = machineSend(t, recipient, http.MethodPost, queue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var claimed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &claimed))
		keys = append(keys, claimed.Message.IdempotencyKey)
		status, body = machineSend(t, recipient, http.MethodPost, queue+"/"+claimed.Id+"/completion", "")
		require.Equal(t, http.StatusNoContent, status, string(body))
	}
	assert.Equal(t, []string{"key-1", "key-4", "key-6", "key-7"}, keys)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body), "a refused message reached the queue")
}

// TestMachineSurface_ARecipientsOwnPolicyAdmitsNoForeignSender covers
// the boundary a resource pattern cannot hold (#265). The engine
// resolves the policies of the organization owning the object, and an
// active policy with no attachment applies to every subject of that
// check — so one unattached Allow in the recipient's organization
// grants every sender anywhere, whatever the sender's own statements
// say.
//
// The policy here is `Action: ["*"]` rather than `message:send`,
// because that is the case that needs no new document: an organization
// holding a wildcard policy written before this action existed is
// exposed the day it ships.
//
// The engine is asked the same question directly, twice: it denies the
// foreign sender before the policy exists and allows it after. That
// pair is what makes this a test of the precondition rather than of a
// policy that reached nothing, and an engine stuck open fails the first
// half rather than passing both. The engine says yes and the surface
// still refuses.
func TestMachineSurface_ARecipientsOwnPolicyAdmitsNoForeignSender(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	for _, name := range []string{"alpha", "beta"} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN(name), name)))
	}

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(ca.NewPostgresStore(p), sealer, ca.Config{})
	require.NoError(t, err)

	policies, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)
	alphaOperator := machineClient(t, installed(t, outOfBand, scopedGRN("alpha", agent.ServiceTypeOperator, "one")))
	betaOperator := machineClient(t, installed(t, outOfBand, scopedGRN("beta", agent.ServiceTypeOperator, "one")))
	foreignGRN, foreign := provisioned(t, url, alphaOperator)
	recipientGRN, recipient := provisioned(t, url, betaOperator)
	neighbourGRN, neighbour := provisioned(t, url, betaOperator)
	require.Equal(t, "alpha", foreignGRN.Organization)
	require.Equal(t, "beta", recipientGRN.Organization)

	// With nothing written anywhere, the foreign sender is refused and
	// the recipient's own neighbour is not.
	status, body := machineSend(t, foreign, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusForbidden, status, string(body))
	status, body = machineSend(t, neighbour, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// The engine denies the foreign subject before the policy exists.
	// It is asked again below and has to answer differently, which is
	// what makes the pair evidence: an engine stuck open answers the
	// same both times and fails here.
	crossing := machineapi.EvalInput{Subject: foreignGRN, Object: recipientGRN, Action: message.ActionSend}
	allowed, err := evaluator.Evaluate(ctx, crossing)
	require.NoError(t, err)
	require.False(t, allowed, "the engine allowed a subject no policy had granted")

	// The recipient's organization activates one wildcard policy and
	// attaches it to nobody, which is what an administrator does by
	// detaching the last attachment from one they had aimed at a subject.
	openDoor := activePolicy(t, policies, orgRootGRN("beta"), "everything",
		statementDoc("everything", wire.EffectAllow, "*", "grn:beta:*"))

	// That policy does reach the foreign subject: the same question,
	// answered the other way now that beta has written one document.
	allowed, err = evaluator.Evaluate(ctx, crossing)
	require.NoError(t, err)
	require.True(t, allowed, "the unattached wildcard policy did not reach the foreign subject")

	// The surface refuses it anyway, which is the whole of the fix. The
	// body names the organization rather than a policy: this refusal is
	// not one beta wrote and not one it can lift.
	status, body = machineSend(t, foreign, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-3"))
	require.Equal(t, http.StatusForbidden, status, string(body))
	assert.JSONEq(t, `{"kind":"permission_denied","message":"recipient is in another organization"}`, string(body))

	// The same call from inside the organization is accepted while that
	// policy is active, so what refuses is the sender's organization and
	// not a surface that stopped accepting messages.
	status, body = machineSend(t, neighbour, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-4"))
	require.Equal(t, http.StatusCreated, status, string(body))

	// Archiving the policy changes nothing about the foreign sender: it
	// was refused before the policy existed and is refused after, so the
	// refusal never depended on what beta wrote.
	archivePolicy(t, policies, openDoor)
	status, body = machineSend(t, foreign, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-5"))
	require.Equal(t, http.StatusForbidden, status, string(body))

	// Nothing the foreign sender posted is in the queue.
	queue := url + "/agents/" + recipientGRN.String() + "/claims"
	var senders []string
	for range 2 {
		status, body = machineSend(t, recipient, http.MethodPost, queue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var claimed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &claimed))
		senders = append(senders, claimed.Message.Sender)
		status, body = machineSend(t, recipient, http.MethodPost, queue+"/"+claimed.Id+"/completion", "")
		require.Equal(t, http.StatusNoContent, status, string(body))
	}
	assert.Equal(t, []string{neighbourGRN.String(), neighbourGRN.String()}, senders)

	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	assert.Equal(t, http.StatusNoContent, status, string(body), "a foreign sender's message reached the queue")
}
