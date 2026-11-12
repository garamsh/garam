package authz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/org"
)

// TestStatementMatches_ATypeGroupNamesItsTypeAndNoPrefixOfIt pins what
// ADR-0145 item 2 rests on, under the platform's own evaluator: a
// `<type>.*` pattern is the type's actions and no other type's, though
// `agent-template` begins with `agent` and `permit` is not `policy`.
// Under the hyphen form, `iam:agent-*` reached `iam:agent-template-
// publish` and the administrator's `iam:policy-*` reached
// `iam:policy-permit`, the right only an owner holds.
//
// Each refusal has its control: the pattern matches an action of its
// own type, so the refusal is the dot and not a pattern that matches
// nothing.
func TestStatementMatches_ATypeGroupNamesItsTypeAndNoPrefixOfIt(t *testing.T) {
	const resource = "grn:root/iam/org/acme"
	matches := func(patterns []string, action string) bool {
		return authz.Statement{Action: patterns, Resource: []string{resource}}.Matches(action, resource)
	}

	admin := org.RoleAdmin.Actions()
	assert.True(t, matches(admin, "iam:policy.create"), "control: an administrator's own group")
	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.False(t, matches(r.Actions(), "iam:permit.write"), "%s: only the policy service's owner role holds it", r)
	}

	agent := []string{"iam:agent.*"}
	assert.False(t, matches(agent, "iam:agent-template.publish"))
	assert.False(t, matches(agent, "iam:agent-template.read"))
	assert.True(t, matches(agent, "iam:agent.read"), "control: an action of type agent")
	assert.True(t, matches(agent, "iam:agent.execution-read"), "control: a hyphenated verb")
}
