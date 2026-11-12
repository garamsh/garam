package transit

import "time"

// SetClock exposes, to the external test package, the clock a login's
// lease is read against, for a test that moves time rather than waits.
// It reports false for a custodian that does not log in.
func SetClock(c *Custodian, now func() time.Time) bool {
	l, ok := c.tokens.(*loginTokens)
	if ok {
		l.now = now
	}
	return ok
}
