package policyrelay_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policyrelay"
)

func mustGRN(t *testing.T, s string) grn.GRN {
	t.Helper()
	g, err := grn.Parse(s)
	require.NoError(t, err)
	return g
}

func TestBodyValidate(t *testing.T) {
	user := mustGRN(t, "grn:root/iam/user/ada")
	team := mustGRN(t, "grn:acme/iam/group/ops")
	project := mustGRN(t, "grn:acme/iam/project/site")
	agent := mustGRN(t, "grn:acme/iam/agent/bot")
	orgGRN := mustGRN(t, "grn:root/iam/org/acme")

	link := func(role org.Role, scope policyrelay.Scope, principal, resource grn.GRN) policyrelay.Body {
		return policyrelay.Body{Kind: policyrelay.KindRoleLink, Link: &policyrelay.RoleLink{
			Role: role, Scope: scope, Principal: principal, Resource: resource,
		}}
	}

	accepted := map[string]policyrelay.Body{
		"a guardrail":                      {Kind: policyrelay.KindGuardrail, Text: "forbid (principal, action, resource);"},
		"a permit":                         {Kind: policyrelay.KindPermit, Text: "permit (principal, action, resource);"},
		"an agent linked to the org":       link(org.RoleMember, policyrelay.ScopeOrganization, agent, orgGRN),
		"a team linked to the org":         link(org.RoleAdmin, policyrelay.ScopeOrganization, team, orgGRN),
		"a project linked to the org":      link(org.RoleMember, policyrelay.ScopeOrganization, project, orgGRN),
		"a user linked to a team":          link(org.RoleMember, policyrelay.ScopeTeamOrProject, user, team),
		"a user linked to a project":       link(org.RoleAdmin, policyrelay.ScopeTeamOrProject, user, project),
		"an agent linked to a team scoped": link(org.RoleMember, policyrelay.ScopeTeamOrProject, agent, team),
	}
	for name, body := range accepted {
		t.Run("control: "+name, func(t *testing.T) {
			require.NoError(t, body.Validate())
		})
	}

	refused := map[string]policyrelay.Body{
		"a user's organization role is its membership's": link(org.RoleMember, policyrelay.ScopeOrganization, user, orgGRN),
		"a team's own scoped link is derived":            link(org.RoleMember, policyrelay.ScopeTeamOrProject, team, team),
		"a project's own scoped link is derived":         link(org.RoleMember, policyrelay.ScopeTeamOrProject, project, project),
		"a guardrail carrying a role link":               {Kind: policyrelay.KindGuardrail, Link: &policyrelay.RoleLink{}},
		"a permit carrying a role link":                  {Kind: policyrelay.KindPermit, Link: &policyrelay.RoleLink{}},
		"a role link carrying no link":                   {Kind: policyrelay.KindRoleLink},
		"a role link carrying text": {Kind: policyrelay.KindRoleLink, Text: "x", Link: &policyrelay.RoleLink{
			Role: org.RoleMember, Scope: policyrelay.ScopeOrganization, Principal: agent, Resource: orgGRN,
		}},
		"an unknown role":  link("root", policyrelay.ScopeOrganization, agent, orgGRN),
		"an unknown scope": link(org.RoleMember, "everywhere", agent, orgGRN),
		"an unknown kind":  {Kind: "statement", Text: "x"},
	}
	for name, body := range refused {
		t.Run(name, func(t *testing.T) {
			err := body.Validate()
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
		})
	}
}

func TestNewID_IsNeverAReservedLinkIdentifier(t *testing.T) {
	a, err := policyrelay.NewID()
	require.NoError(t, err)
	b, err := policyrelay.NewID()
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
	assert.Regexp(t, `^p-[0-9a-f]{32}$`, a)
}
