package operator_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/operator"
)

// TestLimiter_AdmitsTheLimitPerKeyPerWindow holds the bound: a key's
// calls past the limit inside one window are refused with the time
// until it closes, another key in the same window is admitted, and the
// first key is admitted again once its window closes.
func TestLimiter_AdmitsTheLimitPerKeyPerWindow(t *testing.T) {
	c := &clock{at: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
	l := operator.NewLimiter(operator.LimiterConfig{Limit: 2, Window: time.Minute, MaxKeys: 10, Now: c.now})

	require.NoError(t, l.Check("a"))
	require.NoError(t, l.Check("a"))
	c.at = c.at.Add(20 * time.Second)
	err := l.Check("a")
	var exceeded *operator.Exceeded
	require.ErrorAs(t, err, &exceeded)
	assert.Equal(t, 40*time.Second, exceeded.RetryAfter)
	assert.Equal(t, 40, exceeded.RetryAfterSeconds())
	assert.True(t, errorx.IsKind(err, errorx.KindResourceExhausted))

	assert.NoError(t, l.Check("b"), "another key has its own count")

	c.at = c.at.Add(40 * time.Second)
	assert.NoError(t, l.Check("a"), "a closed window admits again")
}

// TestLimiter_RefusesANewKeyWhileTheTrackedSetIsFull holds the memory
// bound: with MaxKeys open windows, a new key is refused until one
// closes, while a tracked key goes on being counted.
func TestLimiter_RefusesANewKeyWhileTheTrackedSetIsFull(t *testing.T) {
	c := &clock{at: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)}
	l := operator.NewLimiter(operator.LimiterConfig{Limit: 5, Window: time.Minute, MaxKeys: 2, Now: c.now})
	require.NoError(t, l.Check("a"))
	require.NoError(t, l.Check("b"))

	var exceeded *operator.Exceeded
	require.ErrorAs(t, l.Check("c"), &exceeded, "a third key while two windows are open")
	assert.NoError(t, l.Check("a"), "a tracked key is still admitted")

	c.at = c.at.Add(time.Minute)
	assert.NoError(t, l.Check("c"), "once the windows close the set has room")
}

func TestExceeded_RoundsRetryAfterUpToAWholeSecond(t *testing.T) {
	assert.Equal(t, 1, (&operator.Exceeded{RetryAfter: 10 * time.Millisecond}).RetryAfterSeconds())
	assert.Equal(t, 2, (&operator.Exceeded{RetryAfter: 1001 * time.Millisecond}).RetryAfterSeconds())
	assert.Equal(t, 1, (&operator.Exceeded{}).RetryAfterSeconds(), "never zero, which would ask for an immediate retry")
}
