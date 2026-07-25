package grn_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
)

func TestNewAndString(t *testing.T) {
	g := grn.New("garam", "default", "user", "alice")
	assert.Equal(t, "grn:garam:default:user:alice", g.String())
}

func TestIsZero(t *testing.T) {
	assert.True(t, grn.GRN{}.IsZero())
	assert.False(t, grn.New("garam", "default", "user", "alice").IsZero())
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		g       grn.GRN
		wantErr bool
	}{
		{"valid", grn.New("o", "p", "t", "i"), false},
		{"empty org", grn.New("", "p", "t", "i"), true},
		{"empty project", grn.New("o", "", "t", "i"), true},
		{"empty type", grn.New("o", "p", "", "i"), true},
		{"empty id", grn.New("o", "p", "t", ""), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.g.Validate()
			if tc.wantErr {
				assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    grn.GRN
		wantErr bool
	}{
		{"valid", "grn:garam:default:user:alice", grn.New("garam", "default", "user", "alice"), false},
		{"missing prefix", "arn:garam:default:user:alice", grn.GRN{}, true},
		{"too few segments", "grn:garam:default:user", grn.GRN{}, true},
		{"empty segment", "grn:garam::user:alice", grn.GRN{}, true},
		{"empty input", "", grn.GRN{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := grn.Parse(tc.input)
			if tc.wantErr {
				assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
