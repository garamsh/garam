package policyfeed_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/policyfeed"
)

func TestGate_AwaitMarksAnswersAtOnceWithNoMarks(t *testing.T) {
	// The control is the test below: the same gate reads the store once
	// there is a mark to read.
	store := &pendingStore{}
	store.pending.Store(1 << 40)
	svc := &wakeCounter{}

	err := policyfeed.NewGate(store, svc, time.Hour).AwaitMarks(context.Background(), nil)

	require.NoError(t, err)
	assert.Zero(t, store.reads.Load(), "nothing was read")
	assert.Zero(t, svc.wakes.Load(), "the loop was not woken")
}

func TestGate_AwaitMarksReadsEachKindByItsOwnRead(t *testing.T) {
	cases := []struct {
		name  string
		mark  policyfeed.Entity
		check func(t *testing.T, s *pendingStore)
	}{
		{"role link", policyfeed.Entity{Type: policyfeed.RoleLinkType, ID: "grn:acme grn:root/iam/user/u"},
			func(t *testing.T, s *pendingStore) {
				assert.Equal(t, "grn:acme", s.org)
				assert.Equal(t, "grn:root/iam/user/u", s.user)
			}},
		{"scoped link", policyfeed.Entity{Type: policyfeed.ScopedRoleLinkType, ID: "grn:acme/iam/group/g"},
			func(t *testing.T, s *pendingStore) { assert.Equal(t, "grn:acme/iam/group/g", s.owner) }},
		{"entity", policyfeed.Entity{Type: policyfeed.AgentType, ID: "grn:acme/iam/agent/a"},
			func(t *testing.T, s *pendingStore) {
				assert.Equal(t, policyfeed.Entity{Type: policyfeed.AgentType, ID: "grn:acme/iam/agent/a"}, s.entity)
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &pendingStore{}
			store.pending.Store(1)

			err := policyfeed.NewGate(store, &wakeCounter{}, time.Second).
				AwaitMarks(context.Background(), []policyfeed.Entity{c.mark})

			require.NoError(t, err)
			assert.EqualValues(t, 2, store.reads.Load(), "one read with the mark, one without")
			c.check(t, store)
		})
	}
}

func TestGate_AwaitMarksRefusesAtTheBoundWhileOneMarkStays(t *testing.T) {
	store := &pendingStore{}
	store.pending.Store(1 << 40)

	err := policyfeed.NewGate(store, &wakeCounter{}, 120*time.Millisecond).
		AwaitMarks(context.Background(), []policyfeed.Entity{
			{Type: policyfeed.OrgType, ID: "grn:acme"}, {Type: policyfeed.UserType, ID: "grn:root/iam/user/u"}})

	require.ErrorIs(t, err, policyfeed.ErrNotApplied)
}

func TestGate_AwaitMarksAnswersASetAsideLinkAtOnce(t *testing.T) {
	// The bound test above is the control: a pending mark is held to the
	// bound, a set-aside link among the marks is not.
	store := &pendingStore{setAside: true}

	start := time.Now()
	err := policyfeed.NewGate(store, &wakeCounter{}, time.Hour).
		AwaitMarks(context.Background(), []policyfeed.Entity{
			{Type: policyfeed.RoleLinkType, ID: "grn:acme grn:root/iam/user/u"}})

	require.ErrorIs(t, err, policyfeed.ErrLinkSetAside)
	assert.Less(t, time.Since(start), time.Second, "no wait for the bound")
}

func TestObservingGate_ReadsTheOutboxWithoutALoop(t *testing.T) {
	store := &pendingStore{}
	store.pending.Store(1)

	err := policyfeed.NewObservingGate(store, time.Second).
		AwaitMarks(context.Background(), []policyfeed.Entity{{Type: policyfeed.OrgType, ID: "grn:acme"}})

	require.NoError(t, err, "it reads the outbox until the mark is gone")
	assert.EqualValues(t, 2, store.reads.Load())
}
