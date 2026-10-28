package pgxtype_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// TestContains escapes each character LIKE reads as other than itself.
// The plain text is the control: it passes through as it is.
func TestContains(t *testing.T) {
	assert.Equal(t, "%ops%", pgxtype.Contains("ops"))
	assert.Equal(t, `%50\%%`, pgxtype.Contains("50%"))
	assert.Equal(t, `%a\_b%`, pgxtype.Contains("a_b"))
	assert.Equal(t, `%a\\b%`, pgxtype.Contains(`a\b`))
}
