package agent_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
)

func TestParseGeneration_AcceptsOnlyTheRuntimeForm(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{name: "32 lowercase hex", in: strings.Repeat("a1", 16), ok: true},
		{name: "uppercase", in: strings.Repeat("A1", 16)},
		{name: "31 characters", in: strings.Repeat("a", 31)},
		{name: "33 characters", in: strings.Repeat("a", 33)},
		{name: "not hex", in: strings.Repeat("g", 32)},
		{name: "empty", in: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := agent.ParseGeneration(tt.in)
			if !tt.ok {
				assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, agent.Generation(tt.in), g)
		})
	}
}

func TestParseRequestID_AcceptsOneTo128UnreservedCharacters(t *testing.T) {
	for _, in := range []string{"a", "A-z_0.9~", strings.Repeat("x", 128)} {
		_, err := agent.ParseRequestID(in)
		assert.NoError(t, err, in)
	}
	for _, in := range []string{"", strings.Repeat("x", 129), "a/b", "a b"} {
		_, err := agent.ParseRequestID(in)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), in)
	}
}

func TestToken_RoundTripsAndHashesWhatItCarries(t *testing.T) {
	token, err := agent.NewToken()
	require.NoError(t, err)
	parsed, err := agent.ParseToken(token.String())
	require.NoError(t, err)
	assert.Equal(t, token, parsed)
	assert.Equal(t, token.Hash(), parsed.Hash())

	other, err := agent.NewToken()
	require.NoError(t, err)
	assert.NotEqual(t, token.Hash(), other.Hash(), "control: another token hashes differently")

	for _, in := range []string{"", token.String()[:42], token.String() + "AA", "!" + token.String()[1:]} {
		_, err := agent.ParseToken(in)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), in)
	}
}

func TestAdmission_ErrNamesEachRefusal(t *testing.T) {
	assert.NoError(t, agent.AdmissionAdmitted.Err())
	refusals := map[agent.Admission]error{
		agent.AdmissionUnknownAgent:     agent.ErrAgentNotFound,
		agent.AdmissionTokenRequired:    agent.ErrExecutionTokenRequired,
		agent.AdmissionTokenStale:       agent.ErrExecutionTokenStale,
		agent.AdmissionCredentialFenced: agent.ErrCredentialFenced,
	}
	for answer, want := range refusals {
		assert.ErrorIs(t, answer.Err(), want, string(answer))
	}
	assert.True(t, errorx.IsKind(agent.Admission("other").Err(), errorx.KindInternal),
		"an answer the fence does not give is not an admission")
}
