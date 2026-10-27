package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/wire"
)

// coworkPage is a pair asking action on the cowork page id.
func coworkPage(action, id string) apigen.PermissionCheck {
	return apigen.PermissionCheck{Action: action,
		Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeCoworkPage, Id: id}}
}

// TestCheckPermissions_AllowsAMemberCoworkPages holds ADR-0127's
// default over the real engine: a member reads and writes the
// organization's page tree and any page in it, by role alone.
// Registering an operator, which a member's role does not hold, is the
// control: the same caller is denied it in the same request, so what
// allows the cowork pairs is the role's grant and not an engine that
// allows everything.
func TestCheckPermissions_AllowsAMemberCoworkPages(t *testing.T) {
	s := newCheckStack(t)

	assert.Equal(t,
		[]apigen.PermissionDecision{apigen.DecisionAllow, apigen.DecisionAllow, apigen.DecisionAllow, apigen.DecisionAllow, apigen.DecisionDeny},
		checkedDecisions(t, s.url, s.member,
			apigen.PermissionCheck{Action: org.ActionCoworkPageRead},
			apigen.PermissionCheck{Action: org.ActionCoworkPageWrite},
			coworkPage(org.ActionCoworkPageRead, "p-1"),
			coworkPage(org.ActionCoworkPageWrite, "p-1"),
			apigen.PermissionCheck{Action: org.ActionOperatorRegister},
		))
}

// TestCheckPermissions_RefusesANonMemberCoworkPages holds that someone
// signed in who belongs to no organization is answered nothing about
// its pages: the request is refused, as every check from a non-member
// is. The member asking the same pairs is the control.
func TestCheckPermissions_RefusesANonMemberCoworkPages(t *testing.T) {
	s := newCheckStack(t)
	body := `{"checks":[{"action":"cowork:page-read","resource":{"type":"cowork-page","id":"p-1"}}]}`

	memberStatus, got := s.send(t, http.MethodPost, "/orgs/acme/permission-checks", s.member, body)
	require.Equal(t, http.StatusOK, memberStatus, got)
	outsiderStatus, got := s.send(t, http.MethodPost, "/orgs/acme/permission-checks", s.outsider, body)
	assert.Equal(t, http.StatusForbidden, outsiderStatus, got)
}

// TestCheckPermissions_NarrowsACoworkPageByPolicy holds that a policy
// decides a cowork page like any other resource: a Deny on one page,
// attached to the member, turns that page's write to deny and leaves
// another page, the control, allowed.
func TestCheckPermissions_NarrowsACoworkPageByPolicy(t *testing.T) {
	s := newCheckStack(t)
	locked := grn.New(s.acme.Identifier, grn.DefaultProject, org.ServiceTypeCoworkPage, "locked")
	require.Equal(t, []apigen.PermissionDecision{apigen.DecisionAllow},
		checkedDecisions(t, s.url, s.member, coworkPage(org.ActionCoworkPageWrite, "locked")))

	deny := activePolicy(t, s.policies, s.acme, "lock-page",
		statementDoc("lock-page", wire.EffectDeny, org.ActionCoworkPageWrite, locked.String()))
	attachPolicy(t, s.policies, deny, s.memberGRN)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionAllow},
		checkedDecisions(t, s.url, s.member,
			coworkPage(org.ActionCoworkPageWrite, "locked"),
			coworkPage(org.ActionCoworkPageWrite, "open"),
		))
}

// coworkChannel is a pair asking action on the cowork channel id.
func coworkChannel(action, id string) apigen.PermissionCheck {
	return apigen.PermissionCheck{Action: action,
		Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeCoworkChannel, Id: id}}
}

// TestCheckPermissions_AllowsAMemberCoworkChannels holds the default
// for messaging over the real engine (ADR-0128): a member reads and
// posts in the organization's channels by role alone, and a Deny on
// one channel, attached to the member, turns that channel's post to
// deny while another channel, the control, stays allowed.
func TestCheckPermissions_AllowsAMemberCoworkChannels(t *testing.T) {
	s := newCheckStack(t)
	require.Equal(t,
		[]apigen.PermissionDecision{apigen.DecisionAllow, apigen.DecisionAllow, apigen.DecisionAllow},
		checkedDecisions(t, s.url, s.member,
			apigen.PermissionCheck{Action: org.ActionCoworkChannelRead},
			coworkChannel(org.ActionCoworkChannelRead, "general"),
			coworkChannel(org.ActionCoworkChannelPost, "general"),
		))

	locked := grn.New(s.acme.Identifier, grn.DefaultProject, org.ServiceTypeCoworkChannel, "announcements")
	deny := activePolicy(t, s.policies, s.acme, "read-only-announcements",
		statementDoc("read-only-announcements", wire.EffectDeny, org.ActionCoworkChannelPost, locked.String()))
	attachPolicy(t, s.policies, deny, s.memberGRN)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionAllow, apigen.DecisionAllow},
		checkedDecisions(t, s.url, s.member,
			coworkChannel(org.ActionCoworkChannelPost, "announcements"),
			coworkChannel(org.ActionCoworkChannelRead, "announcements"),
			coworkChannel(org.ActionCoworkChannelPost, "general"),
		))
}

// coworkTask is a pair asking action on the cowork task id.
func coworkTask(action, id string) apigen.PermissionCheck {
	return apigen.PermissionCheck{Action: action,
		Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeCoworkTask, Id: id}}
}

// TestCheckPermissions_AllowsAMemberCoworkTasks holds the default for
// tasks over the real engine (ADR-0127): a member reads and writes the
// organization's tasks by role alone, and a Deny on one task, attached
// to the member, turns that task's write to deny while another task,
// the control, stays allowed.
func TestCheckPermissions_AllowsAMemberCoworkTasks(t *testing.T) {
	s := newCheckStack(t)
	require.Equal(t,
		[]apigen.PermissionDecision{apigen.DecisionAllow, apigen.DecisionAllow, apigen.DecisionAllow},
		checkedDecisions(t, s.url, s.member,
			apigen.PermissionCheck{Action: org.ActionCoworkTaskWrite},
			coworkTask(org.ActionCoworkTaskRead, "launch"),
			coworkTask(org.ActionCoworkTaskWrite, "launch"),
		))

	locked := grn.New(s.acme.Identifier, grn.DefaultProject, org.ServiceTypeCoworkTask, "audit")
	deny := activePolicy(t, s.policies, s.acme, "read-only-audit",
		statementDoc("read-only-audit", wire.EffectDeny, org.ActionCoworkTaskWrite, locked.String()))
	attachPolicy(t, s.policies, deny, s.memberGRN)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionAllow, apigen.DecisionAllow},
		checkedDecisions(t, s.url, s.member,
			coworkTask(org.ActionCoworkTaskWrite, "audit"),
			coworkTask(org.ActionCoworkTaskRead, "audit"),
			coworkTask(org.ActionCoworkTaskWrite, "launch"),
		))
}

// TestCheckPermissions_AllowsAMemberCoworkArtifacts holds the default
// for artifacts over the real engine (ADR-0127): a member reads and
// writes them by role alone, and a Deny on one artifact turns its
// write to deny while another, the control, stays allowed.
func TestCheckPermissions_AllowsAMemberCoworkArtifacts(t *testing.T) {
	s := newCheckStack(t)
	artifact := func(action, id string) apigen.PermissionCheck {
		return apigen.PermissionCheck{Action: action,
			Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeCoworkArtifact, Id: id}}
	}
	locked := grn.New(s.acme.Identifier, grn.DefaultProject, org.ServiceTypeCoworkArtifact, "brand")
	deny := activePolicy(t, s.policies, s.acme, "read-only-brand",
		statementDoc("read-only-brand", wire.EffectDeny, org.ActionCoworkArtifactWrite, locked.String()))
	attachPolicy(t, s.policies, deny, s.memberGRN)

	assert.Equal(t,
		[]apigen.PermissionDecision{apigen.DecisionAllow, apigen.DecisionDeny, apigen.DecisionAllow, apigen.DecisionAllow},
		checkedDecisions(t, s.url, s.member,
			apigen.PermissionCheck{Action: org.ActionCoworkArtifactWrite},
			artifact(org.ActionCoworkArtifactWrite, "brand"),
			artifact(org.ActionCoworkArtifactRead, "brand"),
			artifact(org.ActionCoworkArtifactWrite, "report"),
		))
}
