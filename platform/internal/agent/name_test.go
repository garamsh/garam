package agent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
)

func TestParseName_RefusesWhatTheRowCannotHold(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{name: "empty", in: ""},
		{name: "only whitespace", in: " \t\n"},
		{name: "past the length bound", in: strings.Repeat("가", 101)},
		{name: "nul byte", in: "field\x00agent"},
		{name: "not valid utf-8", in: "field \xff"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := agent.ParseName(tt.in)
			assert.Empty(t, n)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
		})
	}

	// The bound counts characters, not bytes: the longest name inside
	// it is accepted, and three hundred bytes of Hangul is that name.
	n, err := agent.ParseName(" " + strings.Repeat("가", 100) + " ")
	require.NoError(t, err)
	assert.Equal(t, agent.Name(strings.Repeat("가", 100)), n, "trimmed")
}

// TestParseName_RefusesInTheOrderAndWordsAnOperatorsNameIs pins the one
// shape both name bounds share (#1152): text a column cannot hold is
// refused before the length, so a 101-character name holding NUL is
// refused for its NUL. The same name without the NUL is refused for its
// length, which is the control that the length check still runs.
func TestParseName_RefusesInTheOrderAndWordsAnOperatorsNameIs(t *testing.T) {
	long := strings.Repeat("가", agent.NameMaxLength)
	for in, want := range map[string]string{
		long + "\x00": "an agent's name must be UTF-8 text without NUL characters",
		long + "가":    "an agent's name is at most 100 characters",
		"   ":         "an agent's name must not be empty",
		"field \xff":  "an agent's name must be UTF-8 text without NUL characters",
	} {
		_, err := agent.ParseName(in)
		require.Error(t, err)
		assert.Equal(t, want, err.Error(), "%q", in)
	}
}
