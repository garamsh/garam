package authz

import (
	"github.com/garamsh/garam/internal/wire"
)

// Statement is one IAM-style rule (ADR-0002). Action and Resource
// entries are patterns: "*" matches any (possibly empty) run of
// characters, "?" matches exactly one; wildcards may appear anywhere
// and repeat. Anything else must match literally.
type Statement struct {
	Sid      string
	Effect   wire.Effect
	Action   []string
	Resource []string
}

// PolicyDocument is a versioned statement list (ADR-0002). Condition
// is reserved and not evaluated in the MVP. The field names are the
// document's JSON keys; [ParsePolicyDocument] is the parse boundary.
type PolicyDocument struct {
	Version   string
	Statement []Statement
}

// Matches reports whether the statement applies to an action on a
// resource. Both an action pattern and a resource pattern must match.
func (s Statement) Matches(action, resource string) bool {
	return matchAny(s.Action, action) && matchAny(s.Resource, resource)
}

func matchAny(patterns []string, value string) bool {
	for _, p := range patterns {
		if matchPattern(p, value) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, value string) bool {
	px, vx := 0, 0
	star, starV := -1, 0
	for vx < len(value) {
		switch {
		case px < len(pattern) && (pattern[px] == '?' || pattern[px] == value[vx]):
			px++
			vx++
		case px < len(pattern) && pattern[px] == '*':
			star = px
			starV = vx
			px++
		case star != -1:
			px = star + 1
			starV++
			vx = starV
		default:
			return false
		}
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}
