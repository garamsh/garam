//go:build policyservice

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
)

// The cases here hold explainPermission against the stack's real policy
// service (ADR-0174): its decision is the one checkPermissions answers
// for the same pair, and its grounds are the policies the service
// names, not what the platform infers from a role.

// explain asks why cookie may or may not take action on acme itself.
func (s *enforcementStack) explain(t *testing.T, cookie *http.Cookie, action string) (apigen.PermissionExplanation, string) {
	t.Helper()
	status, body := s.get(t, "/orgs/acme/permission-explanation?"+url.Values{"action": {action}}.Encode(), cookie)
	require.Equal(t, http.StatusOK, status, body)
	var answer apigen.PermissionExplanation
	require.NoError(t, json.Unmarshal([]byte(body), &answer))
	return answer, body
}

// TestEnforcement_ExplainNamesTheRoleLinkThatAllows explains org.invite
// to the owner, whose organization role the service allows it by: the
// answer names the role link and the path it reaches the owner by. The
// member, whose role carries nothing that permits it, is the control: a
// deny that nothing permits names no grounds.
func TestEnforcement_ExplainNamesTheRoleLinkThatAllows(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)

	owner, body := s.explain(t, s.owner, org.ActionOrgInvite)
	assert.Equal(t, apigen.DecisionAllow, owner.Decision)
	assert.Equal(t, s.check(t, s.owner, org.ActionOrgInvite), owner.Decision, "the decision is checkPermissions'")
	assert.Equal(t, apigen.BasisPolicyService, owner.Basis)
	var links []apigen.ExplainedGround
	for _, g := range owner.Grounds {
		if g.Kind == apigen.GroundRoleLink {
			links = append(links, g)
		}
	}
	require.Len(t, links, 1, body)
	assert.Equal(t, ptr(apigen.Role(org.RoleOwner)), links[0].Role)
	assert.Equal(t, ptr(apigen.GroundPathOrganization), links[0].Path)
	require.NotNil(t, links[0].Policy)
	assert.True(t, strings.HasPrefix(*links[0].Policy, "link-owner-"), "named by its reserved identifier: %s", *links[0].Policy)

	member, body := s.explain(t, s.member, org.ActionOrgInvite)
	assert.Equal(t, apigen.DecisionDeny, member.Decision)
	assert.Equal(t, s.check(t, s.member, org.ActionOrgInvite), member.Decision, "the decision is checkPermissions'")
	assert.Empty(t, member.Grounds, "nothing permits it, and nothing forbids it: %s", body)
}

// TestEnforcement_ExplainNamesTheGuardrailThatForbids writes a
// guardrail forbidding org.invite, which the owner's role allows: the
// owner, who may read policies, is answered deny by the guardrail,
// named with its name and text. The answer before the guardrail is the
// control: allow, by the role link, with no guardrail named.
func TestEnforcement_ExplainNamesTheGuardrailThatForbids(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)
	before, _ := s.explain(t, s.owner, org.ActionOrgInvite)
	require.Equal(t, apigen.DecisionAllow, before.Decision)
	for _, g := range before.Grounds {
		require.NotEqual(t, apigen.GroundGuardrail, g.Kind, "control: no guardrail yet")
	}
	id := s.writeGuardrail(t, guardrailName, enforcementGuardrail, true)

	got, body := s.explain(t, s.owner, org.ActionOrgInvite)

	assert.Equal(t, apigen.DecisionDeny, got.Decision)
	assert.Equal(t, s.check(t, s.owner, org.ActionOrgInvite), got.Decision, "the decision is checkPermissions'")
	require.Len(t, got.Grounds, 1, body)
	assert.Equal(t, apigen.GroundGuardrail, got.Grounds[0].Kind)
	assert.Equal(t, ptr(id), got.Grounds[0].Policy)
	assert.Equal(t, ptr(guardrailName), got.Grounds[0].Name)
	assert.Equal(t, ptr(enforcementGuardrail), got.Grounds[0].Text)
}

// TestEnforcement_ExplainGuardrailWithoutContentWithoutPolicyRead
// explains org.invite to the member, whom the same guardrail forbids and
// whose role does not hold iam:policy.read: the guardrail is named by
// its identifier and nothing of it, neither name nor text, is given.
// The owner's answer to the same question, which names both, is the
// control: what holds them back is the read, not the guardrail.
func TestEnforcement_ExplainGuardrailWithoutContentWithoutPolicyRead(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)
	require.Equal(t, apigen.DecisionDeny, s.check(t, s.member, org.ActionPolicyRead), "the member may not read policies")
	require.Equal(t, apigen.DecisionAllow, s.check(t, s.owner, org.ActionPolicyRead))
	id := s.writeGuardrail(t, guardrailName, enforcementGuardrail, true)

	member, body := s.explain(t, s.member, org.ActionOrgInvite)

	assert.Equal(t, apigen.DecisionDeny, member.Decision)
	assert.Equal(t, s.check(t, s.member, org.ActionOrgInvite), member.Decision, "the decision is checkPermissions'")
	require.Len(t, member.Grounds, 1, body)
	assert.Equal(t, apigen.GroundGuardrail, member.Grounds[0].Kind)
	assert.Equal(t, ptr(id), member.Grounds[0].Policy)
	assert.Nil(t, member.Grounds[0].Name)
	assert.Nil(t, member.Grounds[0].Text)
	assert.NotContains(t, body, guardrailName)
	assert.NotContains(t, body, "forbid")

	owner, _ := s.explain(t, s.owner, org.ActionOrgInvite)
	require.Len(t, owner.Grounds, 1)
	assert.Equal(t, ptr(guardrailName), owner.Grounds[0].Name, "control: the owner may read it")
}
