package authz

import "strings"

// Effect is a statement's outcome when it matches.
type Effect string

const (
	EffectAllow Effect = "Allow"
	EffectDeny  Effect = "Deny"
)

// Statement is one IAM-style rule (ADR-0002). Action and Resource
// entries are patterns: "*" matches everything, a trailing "*"
// wildcard matches by prefix (`org:*`, `grn:acme:*`), anything else
// must match exactly.
type Statement struct {
	Sid      string
	Effect   Effect
	Action   []string
	Resource []string
}

// PolicyDocument is a versioned statement list (ADR-0002). Condition
// is reserved and not evaluated in the MVP.
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
	if pattern == value {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return false
}
