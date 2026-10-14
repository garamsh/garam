package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// maxPermissionChecks bounds one request, so a page asking what it
// offers costs the engine a page's worth of checks and no more.
const maxPermissionChecks = 50

// CheckPermissions answers, for each pair asked, whether enforcement
// would allow the signed-in caller the action on the resource: the
// same statements, groups and engine `Check` authorizeOn uses. It
// answers allow or deny and never why. Requires membership.
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
		if targets[i], err = permissionTarget(orgSlug, check.Resource); err != nil {
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
		allowed := false
		if org.IsAction(check.Action) {
			if allowed, err = s.allows(r, c, targets[i], check.Action); err != nil {
				writeError(w, err)
				return
			}
		}
		results[i] = apigen.PermissionCheckResult{Action: check.Action, Resource: check.Resource, Allowed: allowed}
	}
	writeJSON(w, http.StatusOK, apigen.PermissionCheckAnswer{Results: results})
}

// permissionTarget is the GRN enforcement checks a pair's resource as,
// built the way the operation addressing that resource builds it. No
// resource is the org itself.
func permissionTarget(orgSlug string, resource *apigen.PermissionResource) (grn.GRN, error) {
	if resource == nil {
		return orgGRN(orgSlug), nil
	}
	var g grn.GRN
	switch resource.Type {
	case apigen.PermissionResourceTypeGroup:
		g = groupGRN(orgSlug, resource.Id)
	case apigen.PermissionResourceTypeProject:
		g = projectGRN(orgSlug, resource.Id)
	case apigen.PermissionResourceTypeAgent:
		g = agentGRN(orgSlug, resource.Id)
	case apigen.PermissionResourceTypeAgentTemplate:
		g = templateGRN(orgSlug, resource.Id)
	case apigen.PermissionResourceTypePolicy:
		g = policyGRN(orgSlug, resource.Id)
	default:
		return grn.GRN{}, errorx.New(errorx.KindInvalidArgument, "unknown resource type")
	}
	return g, g.Validate()
}
