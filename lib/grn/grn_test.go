package grn_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

func TestNewAndString(t *testing.T) {
	g := grn.New("garam", "default", "user", "alice")
	assert.Equal(t, "grn:garam:default:user:alice", g.String())
}

func TestOrg(t *testing.T) {
	assert.Equal(t, "grn:root:default:org:acme", grn.Org("acme").String())
}

func TestIsZero(t *testing.T) {
	assert.True(t, grn.GRN{}.IsZero())
	assert.False(t, grn.New("garam", "default", "user", "alice").IsZero())
}

// TestValidate reads the accepted rows against the rejected ones. The
// accepted ones are the segment shapes garam builds GRNs from, so a
// rule tightened past what is stored fails here rather than in
// production; the rejected ones are the characters that rename a
// resource somewhere it is carried.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		g       grn.GRN
		wantErr bool
	}{
		{"reserved owner and project", grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme"), false},
		{"randomHex identifier", grn.New(grn.RootOwner, grn.DefaultProject, "org", "9f2c4b81d0e7a35c"), false},
		{"oidc subject identifier", grn.New(grn.RootOwner, grn.DefaultProject, "user", "117421510527410544321"), false},
		{"hyphenated slug", grn.New("acme", grn.DefaultProject, "agent", "bot-1"), false},
		{"identifier at the length bound", grn.New("acme", grn.DefaultProject, "agent", strings.Repeat("a", 64)), false},

		{"identifier past the length bound", grn.New("acme", grn.DefaultProject, "agent", strings.Repeat("a", 65)), true},
		{"identifier holds a question mark", grn.New("acme", grn.DefaultProject, "agent", "bot?1"), true},
		{"identifier holds a hash", grn.New("acme", grn.DefaultProject, "agent", "bot#1"), true},
		{"identifier holds an asterisk", grn.New("acme", grn.DefaultProject, "agent", "bot*"), true},
		{"organization holds a colon", grn.New("ac:me", grn.DefaultProject, "agent", "bot-1"), true},
		{"organization is uppercase", grn.New("ACME", grn.DefaultProject, "agent", "bot-1"), true},
		{"project holds an underscore", grn.New("acme", "team_one", "agent", "bot-1"), true},
		{"service type holds a slash", grn.New("acme", grn.DefaultProject, "agent/x", "bot-1"), true},
		{"identifier holds a control character", grn.New("acme", grn.DefaultProject, "agent", "bot\n1"), true},
		{"identifier is not ascii", grn.New("acme", grn.DefaultProject, "agent", "bot-é"), true},
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

// TestValidate_KeepsAGRNWholeInAURI reads the rule against what it is
// for. A GRN is an opaque URI, so a verifier recovers the identity
// from the whole URI or from Opaque alone (ADR-0014): an accepted GRN
// comes back from both, and every rejected row is a character that
// makes at least one reading recover something else.
func TestValidate_KeepsAGRNWholeInAURI(t *testing.T) {
	accepted := map[string]grn.GRN{
		"randomHex identifier": grn.New(grn.RootOwner, grn.DefaultProject, "org", "9f2c4b81d0e7a35c"),
		"hyphenated slug":      grn.New("acme", grn.DefaultProject, "agent", "bot-1"),
	}
	for name, g := range accepted {
		t.Run("accepts a "+name, func(t *testing.T) {
			require.NoError(t, g.Validate())
			whole, opaque := readings(t, g)
			assert.Equal(t, g, whole)
			assert.Equal(t, g, opaque)
		})
	}

	rejected := map[string]grn.GRN{
		"question mark": grn.New("acme", grn.DefaultProject, "agent", "bot?1"),
		"hash":          grn.New("acme", grn.DefaultProject, "agent", "bot#1"),
		"colon":         grn.New("ac:me", grn.DefaultProject, "agent", "bot-1"),
	}
	for name, g := range rejected {
		t.Run("rejects a "+name, func(t *testing.T) {
			assert.True(t, errorx.IsKind(g.Validate(), errorx.KindInvalidArgument))
			whole, opaque := readings(t, g)
			assert.False(t, whole == g && opaque == g,
				"both readings recover this GRN, so the row no longer shows what the rule is for")
		})
	}
}

// readings returns the GRN recovered from the whole URI and from its
// opaque part alone. A reading that does not parse comes back as the
// zero GRN, which no reading of a valid GRN yields.
func readings(t *testing.T, g grn.GRN) (whole, opaque grn.GRN) {
	t.Helper()
	u, err := url.Parse(g.String())
	require.NoError(t, err)
	parse := func(s string) grn.GRN {
		parsed, err := grn.Parse(s)
		if err != nil {
			return grn.GRN{}
		}
		return parsed
	}
	return parse(u.String()), parse(u.Scheme + ":" + u.Opaque)
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
		{"too many segments", "grn:garam:default:user:alice:extra", grn.GRN{}, true},
		{"empty segment", "grn:garam::user:alice", grn.GRN{}, true},
		{"empty input", "", grn.GRN{}, true},
		{"identifier holds a question mark", "grn:garam:default:user:al?ice", grn.GRN{}, true},
		{"identifier holds a hash", "grn:garam:default:user:al#ice", grn.GRN{}, true},
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
