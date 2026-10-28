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

// TestParseControlEndpoint_RefusesWhatAURLParserRewritesOrCannotReach
// holds the spellings a console could not build a stable request under:
// no host, a port outside the range, and a dot segment, plain or
// escaped. Each case differs from the valid prefix, the control, in
// that one part.
func TestParseControlEndpoint_RefusesWhatAURLParserRewritesOrCannotReach(t *testing.T) {
	const control = "https://a.example:8443/garam/control"
	got, err := operator.ParseControlEndpoint(control)
	require.NoError(t, err)
	assert.Equal(t, operator.ControlEndpoint(control), got)

	tests := []struct {
		name     string
		endpoint string
	}{
		{"empty host", "https://:443"},
		{"empty host and port", "https://:"},
		{"port above the range", "https://a.example:99999/garam/control"},
		{"port zero", "https://a.example:0/garam/control"},
		{"port with a leading zero", "https://a.example:08443/garam/control"},
		{"parent segment", "https://a.example/../garam/control"},
		{"parent segment inside the prefix", "https://a.example:8443/garam/../control"},
		{"current segment", "https://a.example:8443/garam/./control"},
		{"escaped parent segment", "https://a.example:8443/garam/%2e%2E/control"},
		{"half-escaped parent segment", "https://a.example:8443/garam/.%2e/control"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := operator.ParseControlEndpoint(tt.endpoint)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%q: %v", tt.endpoint, err)
		})
	}
}
