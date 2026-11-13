package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
)

// TestPolicyRaised_ACascadingDeleteRecordsEveryMarkItRaised holds, for
// each of the three deletes that take rows with them, that the marks
// the store records for the request are exactly the marks the outbox
// holds after it: the outbox is empty before (seedDelivered), so
// whatever it holds is what the delete raised. A route that waits on
// the recording then waits on all of it (ADR-0165 item 3).
//
// The recorded marks are then put to the real gate over the real
// outbox: it refuses at its bound while they are there, and answers
// once a delivery pass has delivered them.
func TestPolicyRaised_ACascadingDeleteRecordsEveryMarkItRaised(t *testing.T) {
	tests := []struct {
		name   string
		delete func(ctx context.Context, s *orgpg.Store) error
		// has is a mark the cascade must have recorded, so the case
		// cannot pass on an empty recording.
		has string
	}{
		{"an organization", func(ctx context.Context, s *orgpg.Store) error {
			return s.DeleteOrg(ctx, mustGRN(t, outboxOrg))
		}, "Iam::Agent " + outboxAgent},
		{"a group", func(ctx context.Context, s *orgpg.Store) error {
			return s.DeleteGroup(ctx, mustGRN(t, outboxTeam))
		}, "Iam::Team " + outboxTeam},
		{"a project", func(ctx context.Context, s *orgpg.Store) error {
			return s.DeleteProject(ctx, mustGRN(t, outboxProject))
		}, "Iam::Project " + outboxProject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, d := seedDelivered(t)
			ctx, raised := policyfeed.WithRaised(context.Background())

			require.NoError(t, tt.delete(ctx, orgpg.New(p)))

			var recorded []string
			for _, e := range raised.Entities() {
				recorded = append(recorded, e.Type+" "+e.ID)
			}
			held := markKeys(outboxMarks(t, p))
			assert.ElementsMatch(t, held, recorded, "the recording is what the outbox holds")
			assert.Contains(t, recorded, tt.has)

			gate := policyfeed.NewObservingGate(policyfeedpg.NewStore(p), 200*time.Millisecond)
			assert.ErrorIs(t, gate.AwaitMarks(context.Background(), raised.Entities()),
				policyfeed.ErrNotApplied, "held, so refused at the bound")

			_, err := d.pass(t)
			require.NoError(t, err)
			assert.NoError(t, gate.AwaitMarks(context.Background(), raised.Entities()), "delivered")
		})
	}
}

// TestPolicyRaised_ADeleteNobodyAsksAboutRecordsNothing holds that the
// stores answer as before for a caller whose context carries no
// collection: the delete commits and still raises its marks.
func TestPolicyRaised_ADeleteNobodyAsksAboutRecordsNothing(t *testing.T) {
	p, _, _ := seedDelivered(t)
	require.NoError(t, orgpg.New(p).DeleteGroup(context.Background(), mustGRN(t, outboxTeam)))
	assert.Contains(t, markKeys(outboxMarks(t, p)), "Iam::Team "+outboxTeam)
}

func mustGRN(t *testing.T, s string) grn.GRN {
	t.Helper()
	g, err := grn.Parse(s)
	require.NoError(t, err)
	return g
}
