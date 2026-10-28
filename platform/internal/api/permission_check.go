package api

import (
	"fmt"
	"net/http"
	"slices"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// maxPermissionChecks bounds one request, so a page asking what it
// offers costs the engine a page's worth of checks and no more.
const maxPermissionChecks = 50

// onOrg is the kind of a pair that names no resource: the org itself.
const onOrg apigen.PermissionResourceType = ""

const (
	kindGroup   = apigen.PermissionResourceTypeGroup
	kindProject = apigen.PermissionResourceTypeProject
	kindAgent   = apigen.PermissionResourceTypeAgent
	kindPolicy  = apigen.PermissionResourceTypePolicy
	// kindCoworkPage is a page cowork keeps (ADR-0127). No operation
	// here addresses one; cowork asks before it acts on one.
	kindCoworkPage = apigen.PermissionResourceTypeCoworkPage
	// kindCoworkChannel is a channel cowork keeps (ADR-0128), on the
	// same terms.
	kindCoworkChannel = apigen.PermissionResourceTypeCoworkChannel
	// kindCoworkTask is a task cowork keeps (ADR-0127), on the same
	// terms.
	kindCoworkTask = apigen.PermissionResourceTypeCoworkTask
	// kindCoworkArtifact is an artifact cowork keeps (ADR-0127), on
	// the same terms.
	kindCoworkArtifact = apigen.PermissionResourceTypeCoworkArtifact
)

// decidedBy is what enforcement reads to decide an action.
type decidedBy int

const (
	// decidedByCheck is the engine's Check alone, which this operation
	// asks too.
	decidedByCheck decidedBy = iota
	// decidedByGrant is the check a mint of the action runs, which this
	// operation asks too, and a delegation grant to the operator
	// (ADR-0078), which it does not read.
	decidedByGrant
	// decidedByDelegation is the check every write of a delegation runs,
	// on current policy past the engine's cache, which this operation
	// asks too.
	decidedByDelegation
	// decidedByEnroll is the check every approval, withdrawal and
	// revoke of a standalone host runs, on current policy past the
	// engine's cache, which this operation asks too.
	decidedByEnroll
)

// actionRule is how enforcement checks one action: the kinds of
// resource its operations address, and what decides it.
type actionRule struct {
	on        []apigen.PermissionResourceType
	decidedBy decidedBy
}

// actionRules holds one rule per action org.Actions defines, read off
// the operations that enforce it, or for a cowork action, off the
// operations cowork asks it for (ADR-0127). An action with no rule
// pairs with nothing, so every pair naming it is refused.
var actionRules = map[string]actionRule{
	org.ActionOrgRead:       {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionOrgUpdate:     {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionOrgDelete:     {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionOrgMemberList: {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionOrgInvite:     {on: []apigen.PermissionResourceType{onOrg}},

	org.ActionGroupCreate:       {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionGroupRead:         {on: []apigen.PermissionResourceType{onOrg, kindGroup}},
	org.ActionGroupUpdate:       {on: []apigen.PermissionResourceType{kindGroup}},
	org.ActionGroupDelete:       {on: []apigen.PermissionResourceType{kindGroup}},
	org.ActionGroupMemberList:   {on: []apigen.PermissionResourceType{kindGroup}},
	org.ActionGroupMemberAdd:    {on: []apigen.PermissionResourceType{kindGroup}},
	org.ActionGroupMemberRemove: {on: []apigen.PermissionResourceType{kindGroup}},

	org.ActionProjectCreate:       {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionProjectRead:         {on: []apigen.PermissionResourceType{onOrg, kindProject}},
	org.ActionProjectUpdate:       {on: []apigen.PermissionResourceType{kindProject}},
	org.ActionProjectDelete:       {on: []apigen.PermissionResourceType{kindProject}},
	org.ActionProjectMemberList:   {on: []apigen.PermissionResourceType{kindProject}},
	org.ActionProjectMemberAdd:    {on: []apigen.PermissionResourceType{kindProject}},
	org.ActionProjectMemberRemove: {on: []apigen.PermissionResourceType{kindProject}},

	org.ActionPolicyCreate: {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionPolicyRead:   {on: []apigen.PermissionResourceType{onOrg, kindPolicy}},
	org.ActionPolicyUpdate: {on: []apigen.PermissionResourceType{kindPolicy}},
	org.ActionPolicyAttach: {on: []apigen.PermissionResourceType{kindPolicy}},
	org.ActionPolicyDetach: {on: []apigen.PermissionResourceType{kindPolicy}},
	org.ActionPolicyDelete: {on: []apigen.PermissionResourceType{kindPolicy}},

	org.ActionConsentRead:     {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionConsentGrant:    {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionConsentWithdraw: {on: []apigen.PermissionResourceType{onOrg}},

	org.ActionOperatorRead:     {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionOperatorRegister: {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionOperatorUpdate:   {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionOperatorDelete:   {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionOperatorReissue:  {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionOperatorDelegate: {on: []apigen.PermissionResourceType{onOrg}, decidedBy: decidedByDelegation},

	org.ActionAgentRead:            {on: []apigen.PermissionResourceType{onOrg, kindAgent}},
	org.ActionAgentWatch:           {on: []apigen.PermissionResourceType{kindAgent}},
	org.ActionAgentRename:          {on: []apigen.PermissionResourceType{kindAgent}},
	org.ActionAgentEnroll:          {on: []apigen.PermissionResourceType{onOrg, kindAgent}, decidedBy: decidedByEnroll},
	org.ActionAgentCreate:          {on: []apigen.PermissionResourceType{onOrg}, decidedBy: decidedByGrant},
	org.ActionAgentConfigure:       {on: []apigen.PermissionResourceType{kindAgent}, decidedBy: decidedByGrant},
	org.ActionAgentRecover:         {on: []apigen.PermissionResourceType{kindAgent}, decidedBy: decidedByGrant},
	org.ActionAgentReplace:         {on: []apigen.PermissionResourceType{kindAgent}, decidedBy: decidedByGrant},
	org.ActionAgentExecutionRead:   {on: []apigen.PermissionResourceType{kindAgent}, decidedBy: decidedByGrant},
	org.ActionAgentTemplateRead:    {on: []apigen.PermissionResourceType{onOrg}},
	org.ActionAgentTemplatePublish: {on: []apigen.PermissionResourceType{onOrg}, decidedBy: decidedByGrant},
	org.ActionExecutionProfileRead: {on: []apigen.PermissionResourceType{onOrg}, decidedBy: decidedByGrant},

	org.ActionCoworkPageRead:  {on: []apigen.PermissionResourceType{onOrg, kindCoworkPage}},
	org.ActionCoworkPageWrite: {on: []apigen.PermissionResourceType{onOrg, kindCoworkPage}},

	org.ActionCoworkChannelRead: {on: []apigen.PermissionResourceType{onOrg, kindCoworkChannel}},
	org.ActionCoworkChannelPost: {on: []apigen.PermissionResourceType{onOrg, kindCoworkChannel}},

	org.ActionCoworkTaskRead:  {on: []apigen.PermissionResourceType{onOrg, kindCoworkTask}},
	org.ActionCoworkTaskWrite: {on: []apigen.PermissionResourceType{onOrg, kindCoworkTask}},

	org.ActionCoworkArtifactRead:  {on: []apigen.PermissionResourceType{onOrg, kindCoworkArtifact}},
	org.ActionCoworkArtifactWrite: {on: []apigen.PermissionResourceType{onOrg, kindCoworkArtifact}},
}

// CheckPermissions answers, for each pair asked, whether enforcement
// would allow the signed-in caller the action on the resource: the
// same statements, groups and engine `Check` authorizeOn uses. It
// answers allow or deny and never why. An action a delegation grant
// also decides is denied where its mint's check denies and unknown
// where only the grant is left. operator:delegate and agent:enroll are
// each answered by the check their writes run. Requires membership.
func (s *Server) CheckPermissions(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.PermissionCheckRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if n := len(body.Checks); n == 0 || n > maxPermissionChecks {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "a permission check asks 1 to 50 pairs"))
		return
	}
	targets := make([]grn.GRN, len(body.Checks))
	for i, check := range body.Checks {
		if targets[i], err = permissionTarget(orgSlug, check); err != nil {
			writeError(w, err)
			return
		}
	}
	c, err := s.callerIn(r, orgGRN(orgSlug), u)
	if err != nil {
		writeError(w, err)
		return
	}
	results := make([]apigen.PermissionCheckResult, len(body.Checks))
	for i, check := range body.Checks {
		decision, err := s.decide(r, c, targets[i], check.Action)
		// One pair the engine cannot decide fails the request: a
		// partial answer would read its missing pairs as decided.
		if err != nil {
			writeError(w, err)
			return
		}
		results[i] = apigen.PermissionCheckResult{Action: check.Action, Resource: check.Resource, Decision: decision}
	}
	writeJSON(w, http.StatusOK, apigen.PermissionCheckAnswer{Results: results})
}

// decide answers one pair whose target permissionTarget accepted.
func (s *Server) decide(r *http.Request, c *caller, target grn.GRN, action string) (apigen.PermissionDecision, error) {
	if !org.IsAction(action) {
		return apigen.DecisionDeny, nil
	}
	switch actionRules[action].decidedBy {
	case decidedByGrant:
		return s.decideAsMint(r, c, target, action)
	case decidedByDelegation:
		return s.decideAsDelegation(r, c)
	case decidedByEnroll:
		return s.decideAsEnroll(r, c, target)
	case decidedByCheck:
	}
	allowed, err := s.allows(r, c, target, action)
	if err != nil {
		return "", err
	}
	if allowed {
		return apigen.DecisionAllow, nil
	}
	return apigen.DecisionDeny, nil
}

// decideAsMint answers a grant-decided action with the check its mint
// runs on the same actor, action and target. A deny there is final; an
// allow leaves the grant, which this operation does not read, so the
// answer is unknown.
func (s *Server) decideAsMint(r *http.Request, c *caller, target grn.GRN, action string) (apigen.PermissionDecision, error) {
	op, err := operator.ParseOperation(action)
	if err != nil {
		return "", err
	}
	err = s.authority.AuthorizeOperation(r.Context(), c.user.GRN, c.org, target, op)
	switch {
	case err == nil:
		return apigen.DecisionUnknown, nil
	case errorx.IsKind(err, errorx.KindPermissionDenied):
		return apigen.DecisionDeny, nil
	}
	return "", err
}

// decideAsDelegation answers operator:delegate with the check every
// write of a delegation runs, for the same actor and organization. That
// check is all of their authority, so it answers allow or deny.
func (s *Server) decideAsDelegation(r *http.Request, c *caller) (apigen.PermissionDecision, error) {
	err := s.authority.AuthorizeDelegation(r.Context(), c.user.GRN, c.org)
	switch {
	case err == nil:
		return apigen.DecisionAllow, nil
	case errorx.IsKind(err, errorx.KindPermissionDenied):
		return apigen.DecisionDeny, nil
	}
	return "", err
}

// decideAsEnroll answers agent:enroll with the check every approval,
// withdrawal and revoke of a standalone host runs, for the same actor
// and object. That check is all of their authority, so it answers
// allow or deny.
func (s *Server) decideAsEnroll(r *http.Request, c *caller, target grn.GRN) (apigen.PermissionDecision, error) {
	err := s.standalone.AuthorizeEnroll(r.Context(), c.user.GRN, c.org, target)
	switch {
	case err == nil:
		return apigen.DecisionAllow, nil
	case errorx.IsKind(err, errorx.KindPermissionDenied):
		return apigen.DecisionDeny, nil
	}
	return "", err
}

// permissionTarget is the GRN enforcement checks a pair's resource as,
// built the way the operation addressing that resource builds it. No
// resource is the org itself. A defined action paired with a kind none
// of its operations address is refused: no operation asks that pair.
func permissionTarget(orgSlug string, check apigen.PermissionCheck) (grn.GRN, error) {
	kind := onOrg
	if check.Resource != nil {
		kind = check.Resource.Type
	}
	if org.IsAction(check.Action) && !slices.Contains(actionRules[check.Action].on, kind) {
		return grn.GRN{}, errorx.New(errorx.KindInvalidArgument,
			fmt.Sprintf("%s is not checked on %s", check.Action, kindName(kind)))
	}
	if check.Resource == nil {
		return orgGRN(orgSlug), nil
	}
	var g grn.GRN
	id := check.Resource.Id
	switch kind {
	case kindGroup:
		g = groupGRN(orgSlug, id)
	case kindProject:
		g = projectGRN(orgSlug, id)
	case kindAgent:
		g = agentGRN(orgSlug, id)
	case kindPolicy:
		g = policyGRN(orgSlug, id)
	case kindCoworkPage:
		g = grn.New(orgSlug, grn.DefaultProject, org.ServiceTypeCoworkPage, id)
	case kindCoworkChannel:
		g = grn.New(orgSlug, grn.DefaultProject, org.ServiceTypeCoworkChannel, id)
	case kindCoworkTask:
		g = grn.New(orgSlug, grn.DefaultProject, org.ServiceTypeCoworkTask, id)
	case kindCoworkArtifact:
		g = grn.New(orgSlug, grn.DefaultProject, org.ServiceTypeCoworkArtifact, id)
	default:
		return grn.GRN{}, errorx.New(errorx.KindInvalidArgument, "unknown resource type")
	}
	return g, g.Validate()
}

// kindName is how a refusal names the kind a pair addressed.
func kindName(kind apigen.PermissionResourceType) string {
	if kind == onOrg {
		return "the organization"
	}
	return "a resource of type " + string(kind)
}
