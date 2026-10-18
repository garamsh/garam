package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
)

func TestUserStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	s := userpg.New(pool(t))

	g := grnOf("user", "ada")
	u := user.New(g, "ada")
	require.NoError(t, s.Create(ctx, u))

	got, err := s.Get(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, g, got.GRN)
	assert.Equal(t, "ada", got.Nickname)

	_, err = s.Get(ctx, grnOf("user", "nobody"))
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))

	// Many GRNs, one query. GRNs without a user are absent, not an error.
	byGRN, err := s.ListByGRNs(ctx, []grn.GRN{g, grnOf("user", "nobody")})
	require.NoError(t, err)
	require.Len(t, byGRN, 1)
	assert.Equal(t, "ada", byGRN[0].Nickname)

	byGRN, err = s.ListByGRNs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, byGRN)

	updated, err := s.Update(ctx, g, user.Patch{
		Nickname: ptr("ada2"),
		Roles:    []string{"admin"},
	})
	require.NoError(t, err)
	assert.Equal(t, "ada2", updated.Nickname)
	assert.Equal(t, []string{"admin"}, updated.Roles)

	require.NoError(t, s.Delete(ctx, g))
	_, err = s.Get(ctx, g)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	assert.True(t, errorx.IsKind(s.Delete(ctx, g), errorx.KindNotFound))
}

// TestUserStore_DeleteRefusedByAReferenceIsNotAMissingRow drives the
// one delete in this schema a foreign key can refuse.
// `invitations.created_by` references `users (grn)` with no
// `ON DELETE CASCADE`, so the row is kept rather than followed, and
// postgres answers 23503 — the code an insert would mean "the row you
// named is missing" by, and this statement means the opposite by.
//
// Two controls stand beside the refusal, one per answer it has to stay
// apart from. The unreferenced user deletes, so the refusal tracks the
// reference rather than a delete that never works; the absent user is
// still not found, so the two answers a caller has to tell apart are
// asserted together. The read at the end is the point of the issue: the
// row the caller was refused about is still there.
func TestUserStore_DeleteRefusedByAReferenceIsNotAMissingRow(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	users := userpg.New(p)
	orgs := orgpg.New(p)

	referenced, free := grnOf("user", "ada"), grnOf("user", "bob")
	orgGRN := grnOf("org", "acme")
	require.NoError(t, users.Create(ctx, user.New(referenced, "ada")))
	require.NoError(t, users.Create(ctx, user.New(free, "bob")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgGRN, "acme")))
	require.NoError(t, orgs.CreateInvitation(ctx, org.NewInvitation(
		"tok-1", orgGRN, org.RoleAdmin, referenced, time.Now().Add(time.Hour).UTC())))

	err := users.Delete(ctx, referenced)

	require.Error(t, err)
	assert.ErrorIs(t, err, user.ErrUserReferenced)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))
	assert.False(t, errorx.IsKind(err, errorx.KindNotFound),
		"a delete another row blocks is not a missing row")

	require.NoError(t, users.Delete(ctx, free),
		"control: an unreferenced user deletes")
	assert.True(t, errorx.IsKind(users.Delete(ctx, grnOf("user", "nobody")), errorx.KindNotFound),
		"control: a user that is not there is still not found")

	got, err := users.Get(ctx, referenced)
	require.NoError(t, err, "the refused delete removed nothing")
	assert.Equal(t, "ada", got.Nickname)
}

// TestUserStore_SurrendersItsTimestamps holds that the two instants a
// User persists are reachable from the aggregate the store returns
// (ADR-0051). Both columns are written at the insert and read back by
// the same store, so the assertion goes through the aggregate rather
// than through the row: what is under test is whether a caller holding
// the aggregate can reach the value at all.
func TestUserStore_SurrendersItsTimestamps(t *testing.T) {
	ctx := context.Background()
	s := userpg.New(pool(t))

	g := grnOf("user", "ada")
	before := time.Now().UTC()
	require.NoError(t, s.Create(ctx, user.New(g, "ada")))
	after := time.Now().UTC()

	stored, err := s.Get(ctx, g)
	require.NoError(t, err)
	assertInserted(t, stamps{created: stored.CreatedAt, updated: stored.UpdatedAt}, before, after)

	updated, err := s.Update(ctx, g, user.Patch{Nickname: ptr("ada2")})
	require.NoError(t, err)
	assertUpdated(t,
		stamps{created: stored.CreatedAt, updated: stored.UpdatedAt},
		stamps{created: updated.CreatedAt, updated: updated.UpdatedAt})
}
