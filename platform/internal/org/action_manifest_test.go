package org_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/org"
)

// declared is every action the manifests declare, as grn.Actions
// holds them.
func declared() []string {
	var names []string
	for _, a := range grn.Actions() {
		names = append(names, a.Name)
	}
	return names
}

// undeclared returns the patterns that name no declared action: a
// literal the manifests do not hold, or a wildcard matching none.
func undeclared(patterns, declared []string) []string {
	var out []string
	for _, p := range patterns {
		prefix, wildcard := strings.CutSuffix(p, "*")
		if !slices.ContainsFunc(declared, func(name string) bool {
			if wildcard {
				return strings.HasPrefix(name, prefix)
			}
			return name == p
		}) {
			out = append(out, p)
		}
	}
	return out
}

// TestUndeclared_ReportsANameTheManifestsDoNotHold gives the check the
// other tests here rely on a name it must report and a control it must
// not, so a check that reports nothing fails.
func TestUndeclared_ReportsANameTheManifestsDoNotHold(t *testing.T) {
	assert.Equal(t, []string{"iam:org.frobnicate", "org.read", "iam:billing.*"},
		undeclared([]string{
			"iam:org.read", "iam:org.*", "iam:org.frobnicate", "org.read", "iam:billing.*",
		}, declared()))
}

// TestActions_AreDeclaredByTheManifests holds role.go to the manifests:
// every action it defines, and every pattern a role grants, is one
// grn.Actions holds.
func TestActions_AreDeclaredByTheManifests(t *testing.T) {
	assert.Empty(t, undeclared(org.Actions(), declared()))

	for _, r := range []org.Role{org.RoleOwner, org.RoleAdmin, org.RoleMember} {
		assert.Empty(t, undeclared(r.Actions(), declared()), string(r))
	}
}

// TestActions_OmitNoActionTheIamManifestDeclaresButThree holds that the
// actions of the `iam` service role.go does not define are the three
// named here and no other. `iam:message.send` is the platform's own and
// belongs to no role; `iam:permit.write` is held by owners alone
// (ADR-0145 item 4) and no role.go constant names it yet;
// `iam:agent.activate` is a delegated operation the operator package
// names and no role holds.
func TestActions_OmitNoActionTheIamManifestDeclaresButThree(t *testing.T) {
	var omitted []string
	for _, name := range declared() {
		if strings.HasPrefix(name, "iam:") && !org.IsAction(name) {
			omitted = append(omitted, name)
		}
	}
	assert.Equal(t, []string{"iam:agent.activate", "iam:message.send", "iam:permit.write"}, omitted)
}
