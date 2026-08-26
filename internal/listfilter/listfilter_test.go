package listfilter_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/listfilter"
)

func ptr(n int) *int { return &n }

// TestParse_RefusesAPageTheColumnsCannotHold covers both ends of the
// range and both parameters. The accepted rows are the control: a
// refusal proves nothing beside a Parse that refuses everything, and
// int32's own maximum is the value that separates the bound from an
// arbitrary one.
func TestParse_RefusesAPageTheColumnsCannotHold(t *testing.T) {
	tests := []struct {
		name          string
		limit, offset *int
		want          listfilter.Filter
		refuses       string
	}{
		{name: "absent is no paging", want: listfilter.Filter{}},
		{name: "zero is no limit", limit: ptr(0), offset: ptr(0), want: listfilter.Filter{}},
		{name: "the column's own maximum", limit: ptr(math.MaxInt32), offset: ptr(math.MaxInt32),
			want: listfilter.Filter{Limit: math.MaxInt32, Offset: math.MaxInt32}},
		{name: "an ordinary page", limit: ptr(25), offset: ptr(50),
			want: listfilter.Filter{Limit: 25, Offset: 50}},
		{name: "negative limit", limit: ptr(-1), refuses: "limit"},
		{name: "negative offset", offset: ptr(-1), refuses: "offset"},
		{name: "limit one past the column", limit: ptr(math.MaxInt32 + 1), refuses: "limit"},
		{name: "offset one past the column", offset: ptr(math.MaxInt32 + 1), refuses: "offset"},
		{name: "limit past uint32, which truncates to zero", limit: ptr(4294967296), refuses: "limit"},
		{name: "offset past uint32, which truncates to zero", offset: ptr(4294967296), refuses: "offset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := listfilter.Parse(tt.limit, tt.offset)

			if tt.refuses == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
				return
			}
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindOutOfRange),
				"the kind wire.StatusOf maps to 400")
			assert.Contains(t, err.Error(), tt.refuses, "the refusal names the parameter")
			assert.Equal(t, listfilter.Filter{}, got)
		})
	}
}

// TestParse_NamesTheParameterAndNotTheValue keeps the refusal from
// repeating what the caller submitted, which is the property #711
// settled for this surface's other parameter refusal.
func TestParse_NamesTheParameterAndNotTheValue(t *testing.T) {
	_, err := listfilter.Parse(ptr(4294967296), nil)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "4294967296")
}
