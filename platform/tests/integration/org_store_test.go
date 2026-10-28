package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

func TestOrgStore_RoundTripWithCascade(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := orgpg.New(p)
	orgGRN := grnOf("org", "acme")

	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	got, err := s.GetOrg(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, "acme", got.Name)

	renamed, err := s.UpdateOrg(ctx, orgGRN, org.Patch{Name: ptr("acme2")})
	require.NoError(t, err)
	assert.Equal(t, "acme2", renamed.Name)

	groupGRN := grnOf("group", "eng")
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(groupGRN, "eng", orgGRN, string(org.RoleMember))))
	projectGRN := grnOf("project", "web")
	require.NoError(t, s.CreateProject(ctx, org.NewProject(projectGRN, "web", orgGRN, string(org.RoleMember))))

	// The roles column survives the round-trip and still resolves to
	// the actions the scoped check reads.
	gotGroup, err := s.GetGroup(ctx, groupGRN)
	require.NoError(t, err)
	assert.Equal(t, []string{string(org.RoleMember)}, gotGroup.Roles)
	assert.Contains(t, org.ActionsFor(gotGroup.Roles), org.ActionGroupRead)

	gotProject, err := s.GetProject(ctx, projectGRN)
	require.NoError(t, err)
	assert.Equal(t, []string{string(org.RoleMember)}, gotProject.Roles)
	assert.Contains(t, org.ActionsFor(gotProject.Roles), org.ActionProjectRead)

	userGRN := grnOf("user", "ada")
	require.NoError(t, userpg.New(p).Create(ctx, user.New(userGRN, "ada")))
	require.NoError(t, s.AddGroupMember(ctx, groupGRN, userGRN))
	require.NoError(t, s.AddProjectMember(ctx, projectGRN, userGRN))

	members, err := s.ListGroupMembers(ctx, groupGRN)
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{userGRN}, members)

	groups, err := s.ListGroups(ctx, orgGRN, listfilter.Filter{})
	require.NoError(t, err)
	assert.Len(t, groups, 1)
	assert.Equal(t, orgGRN, groups[0].Organization)
	assert.Equal(t, []string{string(org.RoleMember)}, groups[0].Roles)

	// A patch without roles keeps the stored ones (the coalesce
	// branch); one carrying roles replaces the whole set, and an empty
	// slice clears it rather than reading as "unchanged".
	patchedGroup, err := s.UpdateGroup(ctx, groupGRN, org.GroupPatch{Name: ptr("eng2")})
	require.NoError(t, err)
	assert.Equal(t, "eng2", patchedGroup.Name)
	assert.Equal(t, []string{string(org.RoleMember)}, patchedGroup.Roles)

	patchedGroup, err = s.UpdateGroup(ctx, groupGRN, org.GroupPatch{Roles: []string{string(org.RoleAdmin)}})
	require.NoError(t, err)
	assert.Equal(t, []string{string(org.RoleAdmin)}, patchedGroup.Roles)
	assert.Contains(t, org.ActionsFor(patchedGroup.Roles), org.ActionOrgUpdate)

	patchedProject, err := s.UpdateProject(ctx, projectGRN, org.ProjectPatch{Name: ptr("web2")})
	require.NoError(t, err)
	assert.Equal(t, "web2", patchedProject.Name)
	assert.Equal(t, []string{string(org.RoleMember)}, patchedProject.Roles)

	patchedProject, err = s.UpdateProject(ctx, projectGRN, org.ProjectPatch{Roles: []string{}})
	require.NoError(t, err)
	assert.Empty(t, patchedProject.Roles)
	assert.Empty(t, org.ActionsFor(patchedProject.Roles))

	// Deleting the org cascades to groups and projects.
	require.NoError(t, s.DeleteOrg(ctx, orgGRN))
	_, err = s.GetGroup(ctx, groupGRN)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	_, err = s.GetProject(ctx, projectGRN)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
}

// TestOrgStore_GroupAndProjectSurrenderTheirPolicies holds that the
// policies a Group and a Project persist are reachable from the
// aggregate the store returns (ADR-0051). The column is written by the
// patch and read back by the store, so the assertion goes through the
// aggregate rather than through the row: what is under test is whether
// a caller holding the aggregate can reach the value at all.
func TestOrgStore_GroupAndProjectSurrenderTheirPolicies(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := orgpg.New(p)
	orgGRN := grnOf("org", "acme")
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	attached := []string{"read-only", "billing"}

	groupGRN := grnOf("group", "eng")
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(groupGRN, "eng", orgGRN)))
	freshGroup, err := s.GetGroup(ctx, groupGRN)
	require.NoError(t, err)
	assert.Empty(t, freshGroup.Policies,
		"a group stored with none reads back none, so the assertion below reads the patch")

	_, err = s.UpdateGroup(ctx, groupGRN, org.GroupPatch{Policies: attached})
	require.NoError(t, err)
	storedGroup, err := s.GetGroup(ctx, groupGRN)
	require.NoError(t, err)
	assert.Equal(t, attached, storedGroup.Policies)

	projectGRN := grnOf("project", "web")
	require.NoError(t, s.CreateProject(ctx, org.NewProject(projectGRN, "web", orgGRN)))
	freshProject, err := s.GetProject(ctx, projectGRN)
	require.NoError(t, err)
	assert.Empty(t, freshProject.Policies,
		"a project stored with none reads back none, so the assertion below reads the patch")

	_, err = s.UpdateProject(ctx, projectGRN, org.ProjectPatch{Policies: attached})
	require.NoError(t, err)
	storedProject, err := s.GetProject(ctx, projectGRN)
	require.NoError(t, err)
	assert.Equal(t, attached, storedProject.Policies)
}

// TestOrgStore_OrgGroupAndProjectSurrenderTheirTimestamps holds the
// same for the three aggregates this package persists (ADR-0051). All
// three carry the pair, and all three are written at the insert and
// read back by the same store.
func TestOrgStore_OrgGroupAndProjectSurrenderTheirTimestamps(t *testing.T) {
	ctx := context.Background()
	s := orgpg.New(pool(t))

	orgGRN := grnOf("org", "acme")
	groupGRN := grnOf("group", "eng")
	projectGRN := grnOf("project", "web")

	before := time.Now().UTC()
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(groupGRN, "eng", orgGRN)))
	require.NoError(t, s.CreateProject(ctx, org.NewProject(projectGRN, "web", orgGRN)))
	after := time.Now().UTC()

	storedOrg, err := s.GetOrg(ctx, orgGRN)
	require.NoError(t, err)
	assertInserted(t, stamps{created: storedOrg.CreatedAt, updated: storedOrg.UpdatedAt}, before, after)
	updatedOrg, err := s.UpdateOrg(ctx, orgGRN, org.Patch{Name: ptr("acme2")})
	require.NoError(t, err)
	assertUpdated(t,
		stamps{created: storedOrg.CreatedAt, updated: storedOrg.UpdatedAt},
		stamps{created: updatedOrg.CreatedAt, updated: updatedOrg.UpdatedAt})

	storedGroup, err := s.GetGroup(ctx, groupGRN)
	require.NoError(t, err)
	assertInserted(t, stamps{created: storedGroup.CreatedAt, updated: storedGroup.UpdatedAt}, before, after)
	updatedGroup, err := s.UpdateGroup(ctx, groupGRN, org.GroupPatch{Name: ptr("eng2")})
	require.NoError(t, err)
	assertUpdated(t,
		stamps{created: storedGroup.CreatedAt, updated: storedGroup.UpdatedAt},
		stamps{created: updatedGroup.CreatedAt, updated: updatedGroup.UpdatedAt})

	storedProject, err := s.GetProject(ctx, projectGRN)
	require.NoError(t, err)
	assertInserted(t, stamps{created: storedProject.CreatedAt, updated: storedProject.UpdatedAt}, before, after)
	updatedProject, err := s.UpdateProject(ctx, projectGRN, org.ProjectPatch{Name: ptr("web2")})
	require.NoError(t, err)
	assertUpdated(t,
		stamps{created: storedProject.CreatedAt, updated: storedProject.UpdatedAt},
		stamps{created: updatedProject.CreatedAt, updated: updatedProject.UpdatedAt})
}

func TestOrgStore_Membership(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := orgpg.New(p)
	orgGRN := grnOf("org", "acme")
	userGRN := grnOf("user", "ada")

	require.NoError(t, userpg.New(p).Create(ctx, user.New(userGRN, "ada")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	m := org.NewMembership(orgGRN, userGRN, org.RoleOwner)
	require.NoError(t, s.AddMember(ctx, m))
	assert.True(t, errorx.IsKind(s.AddMember(ctx, m), errorx.KindAlreadyExists))

	got, err := s.GetMember(ctx, orgGRN, userGRN)
	require.NoError(t, err)
	assert.Equal(t, org.RoleOwner, got.Role)

	_, err = s.GetMember(ctx, orgGRN, grnOf("user", "nobody"))
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))

	members, err := s.ListMembers(ctx, orgGRN)
	require.NoError(t, err)
	assert.Len(t, members, 1)

	orgs, err := s.ListOrgsByMember(ctx, userGRN, listfilter.Filter{})
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, orgGRN, orgs[0].GRN)

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
	s := orgpg.New(p)
	orgGRN := grnOf("org", "acme")
	owner := grnOf("user", "ada")
	invitee := grnOf("user", "bob")

	users := userpg.New(p)
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
	assert.Equal(t, org.RoleAdmin, got.Role)
	assert.True(t, expires.Equal(got.ExpiresAt))

	_, err = s.GetInvitation(ctx, "nope")
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))

	m, err := s.AcceptInvitation(ctx, "tok-1", invitee)
	require.NoError(t, err)
	assert.Equal(t, orgGRN, m.Organization)
	assert.Equal(t, invitee, m.User)
	assert.Equal(t, org.RoleAdmin, m.Role)
	held, err := s.GetMember(ctx, orgGRN, invitee)
	require.NoError(t, err)
	assert.Equal(t, org.RoleAdmin, held.Role)
	accepted, err := s.GetInvitation(ctx, "tok-1")
	require.NoError(t, err)
	assert.True(t, accepted.Accepted())
	assert.Equal(t, invitee, accepted.AcceptedBy)

	_, err = s.AcceptInvitation(ctx, "tok-1", invitee)
	assert.ErrorIs(t, err, org.ErrInvitationAccepted)
	_, err = s.AcceptInvitation(ctx, "nope", invitee)
	assert.ErrorIs(t, err, org.ErrInvitationNotFound)
}

// TestOrgStore_AcceptInvitationSpendsNothingItCannotJoin holds that the
// accept's claim and membership are one transaction: a user already a
// member is refused and the invitation stays pending, and an expired
// invitation is refused and adds no member (#1381). A second user's
// accept of the same invitation afterwards is the control that the
// refusal left it spendable.
func TestOrgStore_AcceptInvitationSpendsNothingItCannotJoin(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := orgpg.New(p)
	orgGRN := grnOf("org", "acme")
	owner := grnOf("user", "ada")
	member := grnOf("user", "bob")
	newcomer := grnOf("user", "carol")

	users := userpg.New(p)
	for _, u := range []grn.GRN{owner, member, newcomer} {
		require.NoError(t, users.Create(ctx, user.New(u, u.Identifier)))
	}
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	require.NoError(t, s.AddMember(ctx, org.NewMembership(orgGRN, member, org.RoleMember)))
	require.NoError(t, s.CreateInvitation(ctx,
		org.NewInvitation("tok-live", orgGRN, org.RoleAdmin, owner, time.Now().UTC().Add(time.Hour))))
	require.NoError(t, s.CreateInvitation(ctx,
		org.NewInvitation("tok-lapsed", orgGRN, org.RoleAdmin, owner, time.Now().UTC().Add(-time.Minute))))

	_, err := s.AcceptInvitation(ctx, "tok-live", member)
	require.ErrorIs(t, err, org.ErrMemberAlreadyExists)
	pending, err := s.GetInvitation(ctx, "tok-live")
	require.NoError(t, err)
	assert.False(t, pending.Accepted(), "the refused membership rolled the claim back")

	_, err = s.AcceptInvitation(ctx, "tok-lapsed", newcomer)
	require.ErrorIs(t, err, org.ErrInvitationExpired)
	_, err = s.GetMember(ctx, orgGRN, newcomer)
	require.ErrorIs(t, err, org.ErrMemberNotFound, "the expired invitation joined nobody")

	_, err = s.AcceptInvitation(ctx, "tok-live", newcomer)
	require.NoError(t, err)
	joined, err := s.GetMember(ctx, orgGRN, newcomer)
	require.NoError(t, err)
	assert.Equal(t, org.RoleAdmin, joined.Role)
}

// TestOrgStore_InvitationClaimIsReleasedOrCompletedOnlyByItsHolder
// walks the claim a registration takes before the account exists: it
// spends the invitation with no acceptor named, a second claim is
// refused, and the completion and the release each match only the
// claim whose mark they carry, so a stale holder undoes nobody's
// claim and nobody's acceptance (#1381). A claimed invitation past its
// expiry is not pruned: it is spent, not pending.
func TestOrgStore_InvitationClaimIsReleasedOrCompletedOnlyByItsHolder(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := orgpg.New(p)
	orgGRN := grnOf("org", "acme")
	owner := grnOf("user", "ada")
	invitee := grnOf("user", "bob")

	users := userpg.New(p)
	require.NoError(t, users.Create(ctx, user.New(owner, "ada")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	require.NoError(t, s.CreateInvitation(ctx,
		org.NewInvitation("tok-1", orgGRN, org.RoleMember, owner, time.Now().UTC().Add(time.Hour))))

	first, err := s.ClaimInvitation(ctx, "tok-1")
	require.NoError(t, err)
	assert.Equal(t, orgGRN, first.Organization)
	claimed, err := s.GetInvitation(ctx, "tok-1")
	require.NoError(t, err)
	assert.True(t, claimed.Accepted(), "a claimed invitation reads as spent")
	assert.True(t, claimed.AcceptedBy.IsZero(), "with nobody named yet")

	_, err = s.ClaimInvitation(ctx, "tok-1")
	require.ErrorIs(t, err, org.ErrInvitationAccepted)

	require.NoError(t, s.ReleaseInvitationClaim(ctx, first))
	released, err := s.GetInvitation(ctx, "tok-1")
	require.NoError(t, err)
	assert.False(t, released.Accepted())

	second, err := s.ClaimInvitation(ctx, "tok-1")
	require.NoError(t, err)
	require.False(t, second.AcceptedAt.Equal(first.AcceptedAt),
		"the two claims carry different marks")
	assert.Error(t, s.ReleaseInvitationClaim(ctx, first), "a stale mark releases nothing")
	assert.Error(t, s.CompleteInvitationClaim(ctx, first, invitee), "nor completes anything")
	still, err := s.GetInvitation(ctx, "tok-1")
	require.NoError(t, err)
	assert.True(t, still.Accepted(), "the second claim still holds")

	require.NoError(t, users.Create(ctx, user.New(invitee, "bob")))
	require.NoError(t, s.CompleteInvitationClaim(ctx, second, invitee))
	assert.Error(t, s.ReleaseInvitationClaim(ctx, second), "an accepted invitation is not released")
	accepted, err := s.GetInvitation(ctx, "tok-1")
	require.NoError(t, err)
	assert.Equal(t, invitee, accepted.AcceptedBy)

	// A claim whose registration died, past its expiry, survives the
	// sweep the next create runs; the expired pending one beside it is
	// the control that the sweep ran.
	require.NoError(t, s.CreateInvitation(ctx,
		org.NewInvitation("tok-crashed", orgGRN, org.RoleMember, owner, time.Now().UTC().Add(time.Hour))))
	_, err = s.ClaimInvitation(ctx, "tok-crashed")
	require.NoError(t, err)
	require.NoError(t, s.CreateInvitation(ctx,
		org.NewInvitation("tok-lapsed", orgGRN, org.RoleMember, owner, time.Now().UTC().Add(-time.Minute))))
	_, err = s.ClaimInvitation(ctx, "tok-lapsed")
	require.ErrorIs(t, err, org.ErrInvitationExpired, "an expired invitation is not claimed")
	_, err = p.Exec(ctx, `UPDATE invitations SET expires_at = now() - interval '1 minute'
		WHERE token = 'tok-crashed'`)
	require.NoError(t, err)
	require.NoError(t, s.CreateInvitation(ctx,
		org.NewInvitation("tok-next", orgGRN, org.RoleMember, owner, time.Now().UTC().Add(time.Hour))))
	assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM invitations WHERE token = 'tok-crashed'"))
	assert.Equal(t, 0, countRows(t, p, "SELECT count(*) FROM invitations WHERE token = 'tok-lapsed'"))
}

// TestOrgStore_AClaimThatLosesARaceIsRefusedAsSpent holds that a claim
// which matched nothing is answered as spent even when the invitation
// is live again by the time the store reads why: the holder that beat
// it released after its own request failed. That is a lost race, an
// expected conflict, not an internal fault (#1381).
//
// The interleaving is forced with locks, each wait read off
// pg_stat_activity before the next step: a holder's uncommitted claim
// blocks the store's; a table lock queued behind both makes the
// store's read wait once its claim has missed; the release commits
// under that lock, so the read finds the row live. The read is shown
// to be waiting before the release, so the state it meets is the one
// the test asked for.
func TestOrgStore_AClaimThatLosesARaceIsRefusedAsSpent(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := orgpg.New(p)
	orgGRN := grnOf("org", "acme")
	owner := grnOf("user", "ada")
	require.NoError(t, userpg.New(p).Create(ctx, user.New(owner, "ada")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	require.NoError(t, s.CreateInvitation(ctx,
		org.NewInvitation("tok-1", orgGRN, org.RoleMember, owner, time.Now().UTC().Add(time.Hour))))

	waiting := func(like string) func() bool {
		return func() bool {
			return countRows(t, p, `SELECT count(*) FROM pg_stat_activity
				WHERE datname = current_database() AND wait_event_type = 'Lock'
				AND query LIKE $1`, like) == 1
		}
	}

	holder, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = holder.Exec(ctx, `UPDATE invitations SET accepted_at = now() WHERE token = 'tok-1'`)
	require.NoError(t, err)

	lost := make(chan error, 1)
	go func() {
		_, err := s.ClaimInvitation(ctx, "tok-1")
		lost <- err
	}()
	require.Eventually(t, waiting("%name: ClaimInvitation%"), 10*time.Second, 10*time.Millisecond,
		"the store's claim waits on the holder's")

	gate, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = gate.Rollback(ctx) }()
	locked := make(chan error, 1)
	go func() {
		_, err := gate.Exec(ctx, `LOCK TABLE invitations IN ACCESS EXCLUSIVE MODE`)
		locked <- err
	}()
	require.Eventually(t, waiting("LOCK TABLE invitations%"), 10*time.Second, 10*time.Millisecond,
		"the table lock queues behind both claims")

	require.NoError(t, holder.Commit(ctx))
	require.NoError(t, <-locked)
	require.Eventually(t, waiting("%name: GetInvitation%"), 10*time.Second, 10*time.Millisecond,
		"the store's claim missed and its read waits on the table lock")

	_, err = gate.Exec(ctx, `UPDATE invitations SET accepted_at = NULL WHERE token = 'tok-1'`)
	require.NoError(t, err)
	require.NoError(t, gate.Commit(ctx))

	assert.ErrorIs(t, <-lost, org.ErrInvitationAccepted)
	live, err := s.GetInvitation(ctx, "tok-1")
	require.NoError(t, err)
	assert.False(t, live.Accepted(), "the read met a live invitation")
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
	s := orgpg.New(p)
	orgGRN := grnOf("org", "acme")
	owner := grnOf("user", "ada")
	invitee := grnOf("user", "bob")

	users := userpg.New(p)
	require.NoError(t, users.Create(ctx, user.New(owner, "ada")))
	require.NoError(t, users.Create(ctx, user.New(invitee, "bob")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))

	lapsed := time.Now().UTC().Add(-time.Hour)
	ahead := time.Now().UTC().Add(time.Hour)

	// Accepted while it was live, then lapsed: written by hand, since
	// the store refuses to accept an invitation already past expiry.
	spent := org.NewInvitation("tok-accepted", orgGRN, org.RoleMember, owner, lapsed)
	require.NoError(t, s.CreateInvitation(ctx, spent))
	_, err := p.Exec(ctx, `UPDATE invitations SET accepted_by = $2,
		accepted_at = expires_at - interval '1 minute' WHERE token = $1`,
		"tok-accepted", invitee.String())
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
	s := orgpg.New(pool(t))
	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")

	acmeOrg := org.NewOrganization(acme, "acme")
	require.NoError(t, s.CreateOrg(ctx, acmeOrg))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	all, err := s.ListOrgs(ctx, listfilter.Filter{})
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{acme, globex}, grnsOf(all), "every org, ordered by GRN")
	require.Len(t, all, 2)
	assert.Equal(t, "acme", all[0].Name)
	assert.Equal(t, "globex", all[1].Name)

	// The stamp CreateOrg wrote, to the microsecond a timestamptz keeps.
	// A row read back zeroed, or stamped by a clock read at read time,
	// fails here.
	assert.WithinDuration(t, acmeOrg.CreatedAt, all[0].CreatedAt, time.Microsecond)

	page, err := s.ListOrgs(ctx, listfilter.Filter{Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{acme}, grnsOf(page), "the limit takes the first page and stops")

	page, err = s.ListOrgs(ctx, listfilter.Filter{Offset: 1})
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
	s := orgpg.New(p)

	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	engAcme, opsAcme := scopedGRN("acme", "group", "eng"), scopedGRN("acme", "group", "ops")
	engGlobex := scopedGRN("globex", "group", "eng")
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(engAcme, "eng", acme, string(org.RoleMember))))
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(opsAcme, "ops", acme, string(org.RoleMember))))
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(engGlobex, "eng", globex, string(org.RoleMember))))

	// A user is root-owned, which is what lets one join a group or
	// project in each org.
	users := userpg.New(p)
	ada, bob := rootUserGRN("ada"), rootUserGRN("bob")
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

		remaining, err := s.ListGroups(ctx, acme, listfilter.Filter{})
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
	s := orgpg.New(p)

	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(globex, "globex")))

	apiAcme, webAcme := scopedGRN("acme", "project", "api"), scopedGRN("acme", "project", "web")
	webGlobex := scopedGRN("globex", "project", "web")
	require.NoError(t, s.CreateProject(ctx, org.NewProject(apiAcme, "api", acme, string(org.RoleMember))))
	require.NoError(t, s.CreateProject(ctx, org.NewProject(webAcme, "web", acme, string(org.RoleMember))))
	require.NoError(t, s.CreateProject(ctx, org.NewProject(webGlobex, "web", globex, string(org.RoleMember))))

	// A user is root-owned, which is what lets one join a group or
	// project in each org.
	users := userpg.New(p)
	ada, bob := rootUserGRN("ada"), rootUserGRN("bob")
	require.NoError(t, users.Create(ctx, user.New(ada, "ada")))
	require.NoError(t, users.Create(ctx, user.New(bob, "bob")))

	require.NoError(t, s.AddProjectMember(ctx, webAcme, ada))
	require.NoError(t, s.AddProjectMember(ctx, webAcme, bob))
	require.NoError(t, s.AddProjectMember(ctx, apiAcme, bob))
	require.NoError(t, s.AddProjectMember(ctx, webGlobex, ada))

	t.Run("an org's projects are its own, a page at a time", func(t *testing.T) {
		list, err := s.ListProjects(ctx, acme, listfilter.Filter{})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{apiAcme, webAcme}, grnsOf(list), "the org's own projects, ordered by GRN")

		// The other org's project of the same name is reached by asking
		// about that org, so the org filter is what left it out above.
		other, err := s.ListProjects(ctx, globex, listfilter.Filter{})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{webGlobex}, grnsOf(other))

		require.Len(t, list, 2)
		assert.Equal(t, "web", list[1].Name)
		assert.Equal(t, acme, list[1].Organization)
		assert.Equal(t, []string{string(org.RoleMember)}, list[1].Roles)

		page, err := s.ListProjects(ctx, acme, listfilter.Filter{Limit: 1})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{apiAcme}, grnsOf(page), "the limit takes the first page and stops")

		page, err = s.ListProjects(ctx, acme, listfilter.Filter{Offset: 1})
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

		remaining, err := s.ListProjects(ctx, acme, listfilter.Filter{})
		require.NoError(t, err)
		assert.Equal(t, []grn.GRN{apiAcme}, grnsOf(remaining), "the org's other project survives")

		// The refusal, against the delete above as its control: the
		// first call matched a row and this one matches none.
		assert.ErrorIs(t, s.DeleteProject(ctx, webAcme), org.ErrProjectNotFound)
	})
}

// grnsOf names what a list returned, so a scoped list is compared whole
// instead of by a length guard and an index — the guard aborts before
// the claim it guards, and a list is one claim. A constraint cannot
// name a field, so the aggregates are listed and told apart here.
func grnsOf[T *org.Organization | *org.Group | *org.Project](items []T) []grn.GRN {
	out := make([]grn.GRN, 0, len(items))
	for _, item := range items {
		switch v := any(item).(type) {
		case *org.Organization:
			out = append(out, v.GRN)
		case *org.Group:
			out = append(out, v.GRN)
		case *org.Project:
			out = append(out, v.GRN)
		}
	}
	return out
}

// TestOrgStore_MembersStayInsideTheirOrg covers the refusal of a group
// or project member another org owns. Each refused GRN differs from the
// admitted control in its organization segment alone, so that segment
// is what the refusal reads.
func TestOrgStore_MembersStayInsideTheirOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := orgpg.New(p)

	acme, globex := orgRootGRN("acme"), orgRootGRN("globex")
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, s.CreateOrg(ctx, org.NewOrganization(globex, "globex")))
	eng := scopedGRN("acme", org.ServiceTypeGroup, "eng")
	require.NoError(t, s.CreateGroup(ctx, org.NewGroup(eng, "eng", acme)))
	web := scopedGRN("acme", org.ServiceTypeProject, "web")
	require.NoError(t, s.CreateProject(ctx, org.NewProject(web, "web", acme)))

	containers := []struct {
		name     string
		grn      grn.GRN
		table    string
		add      func(context.Context, grn.GRN, grn.GRN) error
		remove   func(context.Context, grn.GRN, grn.GRN) error
		list     func(context.Context, grn.GRN) ([]grn.GRN, error)
		notFound error
	}{
		{"group", eng, "group_members", s.AddGroupMember, s.RemoveGroupMember, s.ListGroupMembers, org.ErrGroupMemberNotFound},
		{"project", web, "project_members", s.AddProjectMember, s.RemoveProjectMember, s.ListProjectMembers, org.ErrProjectMemberNotFound},
	}
	kinds := []string{agent.ServiceType, agent.ServiceTypeOperator, agent.ServiceTypeSupervisor, org.ServiceTypeGroup}
	for _, c := range containers {
		t.Run(c.name, func(t *testing.T) {
			for _, kind := range kinds {
				own := scopedGRN("acme", kind, "one")
				other := scopedGRN("globex", kind, "one")

				err := c.add(ctx, c.grn, other)
				assert.ErrorIs(t, err, org.ErrMemberOfAnotherOrg, "globex's %s is refused", kind)
				assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))
				require.NoError(t, c.add(ctx, c.grn, own), "acme's own %s is admitted", kind)

				// No such row can exist, so the remove is answered as
				// not-a-member rather than refused.
				assert.ErrorIs(t, c.remove(ctx, c.grn, other), c.notFound)
			}

			ada := rootUserGRN("ada")
			require.NoError(t, c.add(ctx, c.grn, ada), "a root-owned user is admitted")

			members, err := c.list(ctx, c.grn)
			require.NoError(t, err)
			want := []grn.GRN{ada}
			for _, kind := range kinds {
				want = append(want, scopedGRN("acme", kind, "one"))
			}
			assert.ElementsMatch(t, want, members, "only the admitted members are stored")

			// A row stored before the refusal existed is still
			// removable, so an org can clear one out.
			legacy := scopedGRN("globex", agent.ServiceType, "legacy")
			_, err = p.Exec(ctx, "INSERT INTO "+c.table+" VALUES ($1, $2)", c.grn.String(), legacy.String())
			require.NoError(t, err)
			require.NoError(t, c.remove(ctx, c.grn, legacy))
			assert.ErrorIs(t, c.remove(ctx, c.grn, legacy), c.notFound)
		})
	}
}

// rootUserGRN returns the root-owned GRN of the named user, the shape
// sign-in mints.
func rootUserGRN(id string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
}
