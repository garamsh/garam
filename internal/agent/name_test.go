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
