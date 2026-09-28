package agent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
)

// TestParseRevision_RefusesBelowOne sets the first revision beside the
// one below it, so what refuses is the bound and not the parse.
func TestParseRevision_RefusesBelowOne(t *testing.T) {
	_, err := agent.ParseRevision(0)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))

	r, err := agent.ParseRevision(1)
	require.NoError(t, err)
	assert.Equal(t, agent.Revision(1), r)
}

// TestNewConfigurationApplied_BoundsTheIgnoredKeys puts each refused
// list beside the list one step from it that is accepted, so the bound
// under test is what refuses and not the list being unreadable.
func TestNewConfigurationApplied_BoundsTheIgnoredKeys(t *testing.T) {
	keys := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "key-" + strings.Repeat("a", i+1)
		}
		return out
	}
	cases := map[string]struct {
		refused  []string
		accepted []string
	}{
		"more keys than a definition holds": {keys(33), keys(32)},
		"a key no definition could carry":   {[]string{"tools.pins.git"}, []string{"tools-pins-git"}},
		"a key longer than 64 characters":   {[]string{strings.Repeat("a", 65)}, []string{strings.Repeat("a", 64)}},
		"a key named twice":                 {[]string{"model", "model"}, []string{"model", "models"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := agent.NewConfigurationApplied(c.refused)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "refused: %v", err)

			applied, err := agent.NewConfigurationApplied(c.accepted)
			require.NoError(t, err)
			assert.Len(t, applied.IgnoredKeys, len(c.accepted))
		})
	}
}

// TestNewConfigurationApplied_NoKeysIsAnEmptyListAndKeysAreSorted
// pins the two shapes a reader tells an application by: an operator
// naming no ignored key gives an empty list and not an absent one, and
// the keys it names read in one order whatever order they arrived in.
func TestNewConfigurationApplied_NoKeysIsAnEmptyListAndKeysAreSorted(t *testing.T) {
	none, err := agent.NewConfigurationApplied(nil)
	require.NoError(t, err)
	assert.NotNil(t, none.IgnoredKeys)
	assert.Empty(t, none.IgnoredKeys)

	some, err := agent.NewConfigurationApplied([]string{"workspace", "model"})
	require.NoError(t, err)
	assert.Equal(t, []string{"model", "workspace"}, some.IgnoredKeys)
}

// TestParseRejectionReason_BoundsTheReason puts each refused reason
// beside the one closest to it that is accepted.
func TestParseRejectionReason_BoundsTheReason(t *testing.T) {
	cases := map[string]struct {
		refused  string
		accepted string
	}{
		"empty":                        {"", "x"},
		"only spaces":                  {"   ", " x "},
		"longer than 500 characters":   {strings.Repeat("é", 501), strings.Repeat("é", 500)},
		"carrying a control character": {"image\npull failed", "image pull failed"},
		"not utf-8":                    {"image \xff", "image ÿ"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := agent.ParseRejectionReason(c.refused)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "refused: %v", err)

			reason, err := agent.ParseRejectionReason(c.accepted)
			require.NoError(t, err)
			assert.Equal(t, agent.RejectionReason(c.accepted), reason)
		})
	}
}
