package operator_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/operator"
)

// TestParseControlEndpoint_AcceptsOnlyABaseURLTheConsoleBuildsUnder
// holds what an administrator may register. Each refusal differs from
// an accepted endpoint in the one property it breaks.
func TestParseControlEndpoint_AcceptsOnlyABaseURLTheConsoleBuildsUnder(t *testing.T) {
	long := "https://control.example.com/" + strings.Repeat("a", 2048-len("https://control.example.com/"))
	for _, ok := range []string{
		"https://control.example.com",
		"https://control.example.com:8443",
		"https://control.example.com/garam/control",
		long,
	} {
		got, err := operator.ParseControlEndpoint(ok)
		require.NoError(t, err, ok)
		assert.Equal(t, operator.ControlEndpoint(ok), got)
	}
	for _, bad := range []string{
		"",
		"http://control.example.com",
		"https://",
		"https://user:pass@control.example.com",
		"https://control.example.com?x=1",
		"https://control.example.com?",
		"https://control.example.com#top",
		"https://control.example.com/",
		"https://control.example.com/garam/",
		"HTTPS://control.example.com",
		"https://control.example.com/a b",
		"control.example.com",
		long + "a",
	} {
		_, err := operator.ParseControlEndpoint(bad)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%q: %v", bad, err)
	}
}
