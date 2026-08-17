package agent_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

func TestNewDefinition_MintsTheAgentItDescribes(t *testing.T) {
	orgGRN := grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme")
	operator := grn.New("acme", grn.DefaultProject, agent.ServiceTypeOperator, "field-one")
	definedBy := grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada")
	values, err := agent.ParseValues(map[string]string{"model": "haiku"})
	require.NoError(t, err)

	d, err := agent.NewDefinition(orgGRN, operator, values, definedBy)
	require.NoError(t, err)
	assert.Equal(t, "acme", d.Agent().Organization)
	assert.Equal(t, agent.ServiceType, d.Agent().ServiceType)
	assert.Len(t, d.Agent().Identifier, 16, "eight random bytes, hex-encoded")
	assert.Equal(t, orgGRN, d.Organization())
	assert.Equal(t, operator, d.Operator())
	assert.Equal(t, agent.Values{"model": "haiku"}, d.Values())
	assert.Equal(t, definedBy, d.DefinedBy())

	// Two definitions of the same org name two agents. A definition
	// describes one agent, so a shared identifier would make the second
	// one a rewrite of the first.
	other, err := agent.NewDefinition(orgGRN, operator, values, definedBy)
	require.NoError(t, err)
	assert.NotEqual(t, d.Agent(), other.Agent())
}

func TestNewDefinition_RejectsAnOperatorTheOrganizationDoesNotOwn(t *testing.T) {
	orgGRN := grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme")
	definedBy := grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada")

	tests := []struct {
		name     string
		operator grn.GRN
	}{
		{
			name:     "another service type",
			operator: grn.New("acme", grn.DefaultProject, agent.ServiceType, "9f2ac1b40d8e7a35"),
		},
		{
			name:     "another organization",
			operator: grn.New("evilcorp", grn.DefaultProject, agent.ServiceTypeOperator, "field-one"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := agent.NewDefinition(orgGRN, tt.operator, agent.Values{}, definedBy)
			assert.Nil(t, d)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
		})
	}

	// The same call with this organization's operator is accepted, so
	// the refusals above are the operator and not the constructor.
	d, err := agent.NewDefinition(orgGRN, grn.New("acme", grn.DefaultProject, agent.ServiceTypeOperator, "field-one"), agent.Values{}, definedBy)
	require.NoError(t, err)
	assert.Equal(t, "acme", d.Agent().Organization)
}

func TestParseValues_RefusesWhatTheRowCannotHold(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
	}{
		{
			name: "too many entries",
			in:   entries(33),
		},
		{
			name: "empty key",
			in:   map[string]string{"": "haiku"},
		},
		{
			name: "key outside the segment character set",
			in:   map[string]string{"Model_Name": "haiku"},
		},
		{
			name: "key past the segment length",
			in:   map[string]string{strings.Repeat("k", 65): "haiku"},
		},
		{
			name: "value past the length bound",
			in:   map[string]string{"model": strings.Repeat("v", 4097)},
		},
		{
			// jsonb stores no NUL, so a value carrying one would fail
			// on the way to the row rather than at this boundary.
			name: "value carrying a nul byte",
			in:   map[string]string{"model": "hai\x00ku"},
		},
		{
			name: "value that is not valid utf-8",
			in:   map[string]string{"model": "\xff"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := agent.ParseValues(tt.in)
			assert.Nil(t, values)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
		})
	}

	// The bounds are the ones stated, not smaller: the largest payload
	// inside every one of them is accepted, and it is the case that
	// stops being accepted when a bound is removed.
	largest := entries(32)
	largest[strings.Repeat("k", 64)] = strings.Repeat("v", 4096)
	delete(largest, "key-0")
	values, err := agent.ParseValues(largest)
	require.NoError(t, err)
	assert.Len(t, values, 32)
}

func TestParseValues_HoldsNoValues(t *testing.T) {
	values, err := agent.ParseValues(nil)
	require.NoError(t, err)
	assert.Empty(t, values)
	assert.NotNil(t, values, "an operator reads an empty payload, not a missing one")
}

// entries returns n values, keyed `key-0` upward.
func entries(n int) map[string]string {
	out := make(map[string]string, n)
	for i := 0; i < n; i++ {
		out[fmt.Sprintf("key-%d", i)] = "v"
	}
	return out
}
