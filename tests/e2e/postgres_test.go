// Package e2e_test exercises the Postgres store implementations, and
// the services layered over them, against a real database. Set GARAM_TEST_DATABASE_URL (e.g. via
// `make e2e`, which starts the docker-compose Postgres) to run them;
// without it every test skips.
package e2e_test

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
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

// TestMigrationsDir_Resolves guards the migration source path: it runs
// without a database, so `make ci` catches a broken path before e2e does.
func TestMigrationsDir_Resolves(t *testing.T) {
	entries, err := os.ReadDir(migrationsDir(t))
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
}

// pool connects to the test database or skips the test.
func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("GARAM_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("GARAM_TEST_DATABASE_URL not set; skipping e2e")
	}
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
		policies, policy_versions, policy_attachments, agents, agent_assignments,
		certificate_authorities CASCADE`)
	require.NoError(t, err)
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

	// The operator reports the pod; the agent reports itself. Neither
	// report moves the other's state.
	provisioned, err := s.ReportProvisioningState(ctx, g, assigned.Epoch(), agent.ProvisioningStateReady)
	require.NoError(t, err)
	assert.Equal(t, agent.ProvisioningStateReady, provisioned.ProvisioningState())
	assert.Equal(t, agent.RuntimeStateUnknown, provisioned.RuntimeState())

	running, err := s.ReportRuntimeState(ctx, g, assigned.Epoch(), agent.RuntimeStateConnected)
	require.NoError(t, err)
	assert.Equal(t, agent.RuntimeStateConnected, running.RuntimeState())
	assert.Equal(t, agent.ProvisioningStateReady, running.ProvisioningState())

	second := grnOf(agent.ServiceTypeOperator, "two")
	reassigned, err := s.Assign(ctx, g, second)
	require.NoError(t, err)
	assert.Equal(t, second, reassigned.Operator())
	assert.Equal(t, 2, reassigned.Epoch(), "a reassignment raises the epoch")

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

func ptr[T any](v T) *T { return &v }

// policyDoc builds a one-statement document whose Sid identifies the
// version it belongs to.
func policyDoc(sid string, effect authz.Effect) authz.PolicyDocument {
	return statementDoc(sid, effect, "org:read", "grn:acme:*")
}

// statementDoc builds a one-statement document over an explicit action
// and resource pattern.
func statementDoc(sid string, effect authz.Effect, action, resource string) authz.PolicyDocument {
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
	created, err := s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", authz.EffectAllow), author)
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
	v2, err := s.AddPolicyVersion(ctx, g, policyDoc("v2", authz.EffectDeny), author)
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
	assert.Equal(t, authz.EffectDeny, active[0].Document.Statement[0].Effect)

	versions, err := s.ListPolicyVersions(ctx, g)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	assert.Equal(t, author, versions[0].CreatedBy())

	_, err = s.SetPolicyDefaultVersion(ctx, g, 99)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	_, err = s.GetPolicy(ctx, grn.New("acme", "default", authz.ServiceTypePolicy, "missing"))
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	_, err = s.AddPolicyVersion(ctx, grn.New("acme", "default", authz.ServiceTypePolicy, "missing"), policyDoc("x", authz.EffectAllow), author)
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
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", authz.EffectAllow), grnOf("user", "ada"))
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
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", authz.EffectAllow), grnOf("user", "ada"))
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
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", authz.EffectAllow), grnOf("user", "ada"))
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
// group it names, an archived one reaches nobody, and detaching the
// last attachment returns the policy to an org-wide grant.
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

	for _, subject := range []grn.GRN{ada, eng} {
		_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: subject.String()})
		require.NoError(t, err)
	}
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{bob})
	require.NoError(t, err)
	require.Len(t, statements, 1)

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
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", authz.EffectAllow), grnOf("user", "ada"))
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
			Effect:   authz.EffectDeny,
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

// TestOrgIsolation_GrantsNeverCrossOrgs asserts that nothing one org
// persists — policy, attachment, cached statement — changes a decision
// about another's. Both policies match every org, so only the lookup's
// org scoping keeps them apart.
func TestOrgIsolation_GrantsNeverCrossOrgs(t *testing.T) {
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
	webB := scopedGRN("globex", "project", "web")

	allowA := activePolicy(t, policies, acme, "allow-read", statementDoc("allow-read", authz.EffectAllow, "project:read", "grn:*:*"))
	attachPolicy(t, policies, allowA, adaA)
	denyB := activePolicy(t, policies, globex, "deny-read", statementDoc("deny-read", authz.EffectDeny, "project:read", "grn:*:*"))
	attachPolicy(t, policies, denyB, engB)

	t.Run("persisted policies stay in their org", func(t *testing.T) {
		active, err := store.ActivePolicies(ctx, acme)
		require.NoError(t, err)
		require.Len(t, active, 1)
		assert.Equal(t, "allow-read", active[0].Document.Statement[0].Sid)

		active, err = store.ActivePolicies(ctx, globex)
		require.NoError(t, err)
		require.Len(t, active, 1)
		assert.Equal(t, "deny-read", active[0].Document.Statement[0].Sid)

		// Org A's subject is unknown to org B's statement lookup.
		statements, err := cache.Statements(ctx, globex, []grn.GRN{adaA})
		require.NoError(t, err)
		assert.Empty(t, statements)
	})

	t.Run("a subject attachment decides nothing across the boundary", func(t *testing.T) {
		assert.True(t, checkRead(t, engine, adaA, webA, nil), "org A's attached allow reaches org A's resource")
		assert.False(t, checkRead(t, engine, adaA, webB, nil), "org A's attached allow does not reach org B's resource")
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
		activePolicy(t, policies, acme, "deny-all", statementDoc("deny-all", authz.EffectDeny, "project:read", "grn:*:*"))

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
