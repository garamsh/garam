package agent_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/platform/internal/agent"
)

// TestEnrollmentSummary_State reads each state off the times an
// approval records. A spent approval stays spent past its expiry and a
// withdrawn one stays withdrawn, so expiry decides only an approval
// nothing else ended; the instant of expiry is already expired.
func TestEnrollmentSummary_State(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	before, after := now.Add(-time.Minute), now.Add(time.Minute)
	tests := []struct {
		name string
		e    agent.EnrollmentSummary
		want agent.EnrollmentState
	}{
		{"pending", agent.EnrollmentSummary{ExpiresAt: after}, agent.EnrollmentPending},
		{"expired", agent.EnrollmentSummary{ExpiresAt: before}, agent.EnrollmentExpired},
		{"expired at the instant", agent.EnrollmentSummary{ExpiresAt: now}, agent.EnrollmentExpired},
		{"spent past expiry", agent.EnrollmentSummary{ExpiresAt: before, SpentAt: &before}, agent.EnrollmentSpent},
		{"withdrawn past expiry", agent.EnrollmentSummary{ExpiresAt: before, WithdrawnAt: &before}, agent.EnrollmentWithdrawn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.e.State(now))
		})
	}
}
