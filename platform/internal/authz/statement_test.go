package authz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/platform/internal/authz"
)

// matchResource is a literal every case below matches on, so that only
// the action pattern under test decides the result.
const matchResource = "grn:root:default:org:acme"

// TestStatementMatches_LiteralPatternNeverBacktracks pins the guard on
// the matcher's backtracking branch: a pattern holding no `*` has
// nothing to backtrack to, so it matches only the string it is. The
// branch is reached through a sentinel index standing for "no `*`
// seen", and a sentinel that is a real index instead lets a
// wildcard-free pattern consume the value and report a match it never
// had — a false grant, since Matches decides whether a statement
// applies.
//
// Which pattern length a wrong sentinel reaches is the value it holds,
// so every length from one to eight is asserted rather than a chosen
// one.
func TestStatementMatches_LiteralPatternNeverBacktracks(t *testing.T) {
	// Same length, no byte in common and none in a shared position:
	// nothing but the literal rule can refuse these.
	pairs := [][2]string{
		{"a", "z"},
		{"ab", "yz"},
		{"abc", "xyz"},
		{"abcd", "wxyz"},
		{"abcde", "vwxyz"},
		{"abcdef", "uvwxyz"},
		{"abcdefg", "tuvwxyz"},
		{"abcdefgh", "stuvwxyz"},
	}
	for _, p := range pairs {
		pattern, other := p[0], p[1]
		t.Run(pattern+" against "+other, func(t *testing.T) {
			st := authz.Statement{Action: []string{pattern}, Resource: []string{matchResource}}
			assert.False(t, st.Matches(other, matchResource))
			// The control. Without it the refusal above also holds for
			// a statement that matches nothing at all, which is a
			// different state and the one a broken matcher reaches by
			// refusing everything rather than by granting too much.
			assert.True(t, st.Matches(pattern, matchResource), "control: the pattern still matches itself")
		})
	}
}

// TestStatementMatches_Wildcards pins what the two wildcards mean:
// `*` matches any run including an empty one, `?` matches exactly one
// byte, and both may repeat and appear anywhere. The interior cases
// exercise the backtracking branch, where a `*` that consumed too
// little or too much is retried at the next offset.
func TestStatementMatches_Wildcards(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		value   string
		want    bool
	}{
		{"star alone matches anything", "*", "org:read", true},
		{"star alone matches the empty value", "*", "", true},
		{"trailing star matches the rest", "org:*", "org:member-list", true},
		{"trailing star matches an empty rest", "org:*", "org:", true},
		{"trailing star does not cross its literal prefix", "org:*", "group:read", false},
		{"leading star matches a suffix", "*:read", "project:read", true},
		{"question matches exactly one byte", "or?", "org", true},
		{"question does not match zero bytes", "or?", "or", false},
		{"question does not match two bytes", "or?", "orgs", false},
		{"interior star backtracks to a later match", "a*bc", "abxbc", true},
		{"interior star fails when no offset works", "a*bc", "abxbd", false},
		{"two interior stars both backtrack", "a*b*c", "axxbyyc", true},
		{"star does not excuse a missing literal tail", "org:*-list", "org:member", false},
		{"empty pattern matches only the empty value", "", "org:read", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := authz.Statement{Action: []string{c.pattern}, Resource: []string{matchResource}}
			assert.Equal(t, c.want, st.Matches(c.value, matchResource))
		})
	}
}

// TestStatementMatches_RequiresBothActionAndResource pins the
// conjunction: a statement applies only where an action pattern and a
// resource pattern both match, and either list matches on any one
// entry. The two matching pairs are the control, so the refusals below
// are the conjunction and not a statement that matches nothing.
func TestStatementMatches_RequiresBothActionAndResource(t *testing.T) {
	st := authz.Statement{
		Action:   []string{"org:read", "org:update"},
		Resource: []string{"grn:root:default:org:acme", "grn:acme:*"},
	}
	assert.True(t, st.Matches("org:read", "grn:root:default:org:acme"), "first entry of each list")
	assert.True(t, st.Matches("org:update", "grn:acme:default:project:web"), "last entry of each list")
	assert.False(t, st.Matches("org:delete", "grn:root:default:org:acme"), "action matches no entry")
	assert.False(t, st.Matches("org:read", "grn:globex:default:org:other"), "resource matches no entry")
	assert.False(t, authz.Statement{}.Matches("org:read", "grn:root:default:org:acme"), "no patterns at all")
}
