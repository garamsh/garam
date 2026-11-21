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

	"github.com/garamsh/garam/lib/grn"
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

// explainOn asks why cookie may or may not take action on the team.
func (s *enforcementStack) explainOn(t *testing.T, cookie *http.Cookie, action, team string) apigen.PermissionExplanation {
	t.Helper()
	query := url.Values{"action": {action}, "resourceType": {"group"}, "resourceId": {team}}
	status, body := s.get(t, "/orgs/acme/permission-explanation?"+query.Encode(), cookie)
	require.Equal(t, http.StatusOK, status, body)
	var answer apigen.PermissionExplanation
	require.NoError(t, json.Unmarshal([]byte(body), &answer))
	return answer
}

// makeTeam creates a team holding the role admin on itself through the
// listener, makes each of members a member of it, and delivers what
// that changed to the service. It answers the team's slug.
func (s *enforcementStack) makeTeam(t *testing.T, name string, members ...grn.GRN) string {
	t.Helper()
	status, body := browse(t, http.MethodPost, s.url+"/orgs/acme/groups", s.owner, `{"name":"`+name+`","roles":["admin"]}`)
	require.Equal(t, http.StatusCreated, status, body)
	var made apigen.Group
	require.NoError(t, json.Unmarshal([]byte(body), &made))
	team, err := grn.Parse(made.Grn)
	require.NoError(t, err)
	s.rp.deliver(t) // the service must know the team before it decides on it
	for _, who := range members {
		status, body = browse(t, http.MethodPost, s.url+"/orgs/acme/groups/"+team.Identifier+"/members", s.owner,
			`{"memberGrn":"`+who.String()+`"}`)
		require.Equal(t, http.StatusNoContent, status, body)
	}
	s.rp.deliver(t)
	return team.Identifier
}

// groundNamed is the ground of explanation that names the policy id.
func groundNamed(t *testing.T, e apigen.PermissionExplanation, id string) apigen.ExplainedGround {
	t.Helper()
	for _, g := range e.Grounds {
		if g.Policy != nil && *g.Policy == id {
			return g
		}
	}
	t.Fatalf("no ground names %s: %+v", id, e.Grounds)
	return apigen.ExplainedGround{}
}

// TestEnforcement_ExplainNamesATeamsRoleLinkByTheTeamItReachesTheCallerOn
// makes the member a member of a team that holds the role admin on
// itself, which the platform delivers as a link under `link-scoped-`: the
// member is allowed group.update on that team, and the answer names the
// link as the team's. The same question on a team the member is not in
// is the control: denied, with no link named.
func TestEnforcement_ExplainNamesATeamsRoleLinkByTheTeamItReachesTheCallerOn(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)
	held := s.makeTeam(t, "held", s.memberGRN)
	other := s.makeTeam(t, "other")

	in := s.explainOn(t, s.member, org.ActionGroupUpdate, held)

	assert.Equal(t, apigen.DecisionAllow, in.Decision)
	var links []apigen.ExplainedGround
	for _, g := range in.Grounds {
		if g.Kind == apigen.GroundRoleLink {
			links = append(links, g)
		}
	}
	require.Len(t, links, 1, "%+v", in.Grounds)
	assert.Equal(t, ptr(apigen.Role(org.RoleAdmin)), links[0].Role)
	assert.Equal(t, ptr(apigen.GroundPathTeam), links[0].Path)
	assert.Equal(t, ptr(held), links[0].Team)
	require.NotNil(t, links[0].Policy)
	assert.True(t, strings.HasPrefix(*links[0].Policy, "link-scoped-admin-"), *links[0].Policy)

	out := s.explainOn(t, s.member, org.ActionGroupUpdate, other)
	assert.Equal(t, apigen.DecisionDeny, out.Decision, "control: the member is not in this team")
	for _, g := range out.Grounds {
		assert.NotEqual(t, apigen.GroundRoleLink, g.Kind, "control: no link reaches the member here")
	}
}

// TestEnforcement_ExplainNamesAConsoleWrittenRoleLinkAsOne writes through
// the relay a role link the platform does not name, as the console
// does (it gives every policy a `p-` identifier): the owner's own admin
// link on a team. The owner, who may read policies, is told it is a role
// link, by its body's role and team, and not a permit because it allows.
// The same link, written for the member, is named as a stored policy
// whose kind is withheld to the member, who may not read policies: it
// allows them, and the answer does not say a role link or a permit.
func TestEnforcement_ExplainNamesAConsoleWrittenRoleLinkAsOne(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)
	team := s.makeTeam(t, "held")
	teamGRN := grn.New("acme", "group", team).String()
	write := func(who grn.GRN) string {
		raw, err := json.Marshal(map[string]any{"name": "console-link", "body": json.RawMessage(
			`{"kind":"role-link","roleLink":{"role":"admin","scope":"team-or-project","principal":"` +
				who.String() + `","resource":"` + teamGRN + `"}}`)})
		require.NoError(t, err)
		status, got := browse(t, http.MethodPost, s.url+"/orgs/acme/iam-policies", s.owner, string(raw))
		require.Equal(t, http.StatusCreated, status, got)
		id := policyID(t, got)
		status, got = browse(t, http.MethodPost, s.url+"/orgs/acme/iam-policies/"+id+"/activate", s.owner, "")
		require.Equal(t, http.StatusNoContent, status, got)
		return id
	}
	forOwner, forMember := write(s.ownerGRN), write(s.memberGRN)

	owner := s.explainOn(t, s.owner, org.ActionGroupUpdate, team)
	assert.Equal(t, apigen.DecisionAllow, owner.Decision)
	link := groundNamed(t, owner, forOwner)
	assert.Equal(t, apigen.GroundRoleLink, link.Kind, "a role link, though it allows like a permit")
	assert.Equal(t, ptr(apigen.Role(org.RoleAdmin)), link.Role)
	assert.Equal(t, ptr(apigen.GroundPathTeam), link.Path)
	assert.Equal(t, ptr(team), link.Team)

	member := s.explainOn(t, s.member, org.ActionGroupUpdate, team)
	assert.Equal(t, apigen.DecisionAllow, member.Decision)
	withheld := groundNamed(t, member, forMember)
	assert.Equal(t, apigen.GroundStoredPolicy, withheld.Kind, "the member may not read policies")
	assert.Nil(t, withheld.Role)
	assert.Nil(t, withheld.Name)
	assert.Nil(t, withheld.Text)
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
// its identifier and nothing of it, neither kind nor name nor text, is
// given.
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
	assert.Equal(t, apigen.GroundStoredPolicy, member.Grounds[0].Kind, "the kind is the service's to tell a reader")
	assert.Equal(t, ptr(id), member.Grounds[0].Policy)
	assert.Nil(t, member.Grounds[0].Name)
	assert.Nil(t, member.Grounds[0].Text)
	assert.NotContains(t, body, guardrailName)
	assert.NotContains(t, body, "forbid")

	owner, _ := s.explain(t, s.owner, org.ActionOrgInvite)
	require.Len(t, owner.Grounds, 1)
	assert.Equal(t, ptr(guardrailName), owner.Grounds[0].Name, "control: the owner may read it")
}
