package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
)

// enrollOn is the iam:agent.enroll pair on the organization, or on
// agent where one is given.
func enrollOn(agent *grn.GRN) apigen.PermissionCheck {
	check := apigen.PermissionCheck{Action: org.ActionAgentEnroll}
	if agent != nil {
		check.Resource = &apigen.PermissionResource{Type: apigen.PermissionResourceTypeAgent, Id: agent.Identifier}
	}
	return check
}

// TestCheckPermissions_AnswersAgentEnrollAsAnApprovalDecides holds each
// iam:agent.enroll answer beside the approval it describes, over the
// standalone service `serve api` composes. The owner is answered allow
// on the organization and on an agent, and both approvals succeed; the
// member, whose role holds no iam:agent.enroll, is answered deny and
// refused, which is the control that the check is not answering allow
// to everyone. The policy service then denying the owner the action
// turns both answers to deny, and the replacement and a new host are
// each refused. iam:agent.create, which a delegation grant also
// decides, is still answered unknown.
func TestCheckPermissions_AnswersAgentEnrollAsAnApprovalDecides(t *testing.T) {
	p := pool(t)
	s := newExecutionStack(t, p)
	_, held := s.standaloneOf(t)
	replace := map[string]any{"agentGrn": held.agent.String(), "assigneeGrn": held.supervisor.String(), "epoch": held.epoch}
	approveAs := func(cookie *http.Cookie, replaces map[string]any) int {
		status, _ := s.approve(t, cookie, "acme", newStandaloneHost(t).approval(t, time.Now().Add(time.Hour), replaces))
		return status
	}

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionAllow, apigen.DecisionAllow},
		checkedDecisions(t, s.browser, s.owner, enrollOn(nil), enrollOn(&held.agent)), "the owner")
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionDeny},
		checkedDecisions(t, s.browser, s.member, enrollOn(nil), enrollOn(&held.agent)), "the member")
	assert.Equal(t, http.StatusForbidden, approveAs(s.member, nil), "the approval refuses the member the check denied")
	assert.Equal(t, http.StatusCreated, approveAs(s.owner, nil), "the approval the check allowed")

	ada := grn.New(grn.RootOwner, user.ServiceType, "ada")
	s.decisions.forbid(ada, org.ActionAgentEnroll)

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny, apigen.DecisionDeny},
		checkedDecisions(t, s.browser, s.owner, enrollOn(nil), enrollOn(&held.agent)), "the service's denial reaches the owner")
	assert.Equal(t, http.StatusForbidden, approveAs(s.owner, replace), "the replacement is refused")
	assert.Equal(t, http.StatusForbidden, approveAs(s.owner, nil), "a new host is refused")

	require.Equal(t, []apigen.PermissionDecision{apigen.DecisionUnknown},
		checkedDecisions(t, s.browser, s.owner, apigen.PermissionCheck{Action: org.ActionAgentCreate}),
		"a delegation grant still decides iam:agent.create")
}
