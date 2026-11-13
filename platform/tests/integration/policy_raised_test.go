package integration_test

import (
	"context"
	"slices"
	"strings"
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
// the store records for the request are exactly the marks its own
// transaction raised (ADR-0165 item 3). The outbox is empty before
// (seedDelivered), and then holds two committed pending marks the
// delete did not write: one of an entity the delete does not touch,
// which the recording must not hold, and one of an entity the delete
// raises again, which it must. A route that waits on the recording
// then waits on all of it and on nothing older.
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
		// again is the mark committed pending before the delete that
		// the delete raises again.
		again string
	}{
		{"an organization", func(ctx context.Context, s *orgpg.Store) error {
			return s.DeleteOrg(ctx, mustGRN(t, outboxOrg))
		}, "Iam::Agent " + outboxAgent, "Iam::Org " + outboxOrg},
		{"a group", func(ctx context.Context, s *orgpg.Store) error {
			return s.DeleteGroup(ctx, mustGRN(t, outboxTeam))
		}, "Iam::Team " + outboxTeam, "Iam::Team " + outboxTeam},
		{"a project", func(ctx context.Context, s *orgpg.Store) error {
			return s.DeleteProject(ctx, mustGRN(t, outboxProject))
		}, "Iam::Project " + outboxProject, "Iam::Project " + outboxProject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _, d := seedDelivered(t)
			execSQL(t, p, `SELECT policy_outbox_mark('Iam::Team', $1)`, earlier)
			typ, id, _ := strings.Cut(tt.again, " ")
			execSQL(t, p, `SELECT policy_outbox_mark($1, $2)`, typ, id)
			require.Contains(t, markKeys(outboxMarks(t, p)), "Iam::Team "+earlier, "the case needs the earlier mark")
			ctx, raised := policyfeed.WithRaised(context.Background())

			require.NoError(t, tt.delete(ctx, orgpg.New(p)))

			var recorded []string
			for _, e := range raised.Entities() {
				recorded = append(recorded, e.Type+" "+e.ID)
			}
			held := markKeys(outboxMarks(t, p))
			require.Contains(t, held, "Iam::Team "+earlier, "the delete left the earlier mark alone")
			assert.NotContains(t, recorded, "Iam::Team "+earlier, "an earlier mark is not the delete's")
			assert.ElementsMatch(t, slices.DeleteFunc(slices.Clone(held), func(k string) bool {
				return k == "Iam::Team "+earlier
			}), recorded, "the recording is what the delete raised")
			assert.Contains(t, recorded, tt.has)
			assert.Contains(t, recorded, tt.again, "a mark pending before, raised again, is the delete's")

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

// earlier is an entity no delete here touches, whose mark is committed
// pending before the delete runs.
const earlier = "grn:zzzzzzzzzzzzzzzz/iam/group/00000000000000aa"

func mustGRN(t *testing.T, s string) grn.GRN {
	t.Helper()
	g, err := grn.Parse(s)
	require.NoError(t, err)
	return g
}
