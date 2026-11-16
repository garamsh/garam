package api

import (
	"net/http"
	"slices"
	"strings"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
	"github.com/garamsh/garam/platform/internal/policyrelay"
)

// The identifiers the platform reserves to the role links it writes
// (ADR-0161, ADR-0163): `link-<role>-<hex>` holds a member's role in
// the organization, and `link-scoped-<role>-<hex>` the role a team or a
// project holds on itself.
const (
	linkPrefix       = "link-"
	scopedLinkPrefix = "link-scoped-"
)

// ExplainPermission answers why the signed-in caller may or may not
// take one action: the decision checkPermissions answers for the pair,
// and where the policy service decides it, the grounds it names. A
// guardrail or a permit the caller may not read is named without its
// content. Requires membership.
func (s *Server) ExplainPermission(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ExplainPermissionParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	check, err := permissionPair(params.Action, params.ResourceType, params.ResourceId)
	if err != nil {
		writeError(w, err)
		return
	}
	target, err := permissionTarget(orgSlug, check)
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := s.callerIn(r, orgGRN(orgSlug), u)
	if err != nil {
		writeError(w, err)
		return
	}
	// Nothing an explanation asks is acted on, so each question is a
	// probe (ADR-0171).
	r = r.WithContext(policydecision.AsProbe(r.Context()))
	answer, err := s.explain(r, c, target, check.Action)
	if err != nil {
		writeError(w, err)
		return
	}
	answer.Action, answer.Resource = check.Action, check.Resource
	writeJSON(w, http.StatusOK, answer)
}

// permissionPair is the pair a query names, as checkPermissions' body
// names one. A type without an identifier, or the reverse, names
// neither the organization nor a resource of it.
func permissionPair(action string, kind *apigen.PermissionResourceType, id *string) (apigen.PermissionCheck, error) {
	check := apigen.PermissionCheck{Action: action}
	switch {
	case kind == nil && id == nil:
		return check, nil
	case kind == nil || id == nil:
		return check, errorx.New(errorx.KindInvalidArgument, "resourceType and resourceId are given together or not at all")
	}
	check.Resource = &apigen.PermissionResource{Type: *kind, Id: *id}
	return check, nil
}

// explain answers one pair whose target permissionTarget accepted. An
// action the policy service does not alone decide is answered with
// checkPermissions' decision and no grounds, since what decides it is
// the operation. Any other is the question checkPermissions asks, so
// its decision is the same.
func (s *Server) explain(r *http.Request, c *caller, target grn.GRN, action string) (apigen.PermissionExplanation, error) {
	none := []apigen.ExplainedGround{}
	if !org.IsAction(action) {
		return apigen.PermissionExplanation{Decision: apigen.DecisionDeny, Basis: apigen.BasisUndefinedAction, Grounds: none}, nil
	}
	if actionRules[action].decidedBy != decidedByCheck {
		decision, err := s.decide(r, c, target, action)
		if err != nil {
			return apigen.PermissionExplanation{}, err
		}
		return apigen.PermissionExplanation{Decision: decision, Basis: apigen.BasisOperation, Grounds: none}, nil
	}
	d, err := s.asks(r, c, target, action)
	if err != nil {
		return apigen.PermissionExplanation{}, err
	}
	grounds, err := s.groundsOf(r, c, target, d)
	if err != nil {
		return apigen.PermissionExplanation{}, err
	}
	decision := apigen.DecisionDeny
	if d.Allowed {
		decision = apigen.DecisionAllow
	}
	return apigen.PermissionExplanation{Decision: decision, Basis: apigen.BasisPolicyService, Grounds: grounds}, nil
}

// groundsOf names each reason of d, in the order of the policy each
// names so that the same decision reads the same. Whether the caller
// may read a guardrail or a permit is asked once, however many the
// decision names, and only when one is named.
func (s *Server) groundsOf(r *http.Request, c *caller, target grn.GRN, d policydecision.Decision) ([]apigen.ExplainedGround, error) {
	reasons := make([]policydecision.Reason, 0, len(d.Reasons))
	for reason := range d.Reasons {
		reasons = append(reasons, reason)
	}
	slices.SortFunc(reasons, func(a, b policydecision.Reason) int {
		if byPolicy := strings.Compare(a.PolicyID, b.PolicyID); byPolicy != 0 {
			return byPolicy
		}
		return strings.Compare(a.Error, b.Error)
	})
	var may *bool
	readable := func() (bool, error) {
		if may == nil {
			allowed, err := s.allows(r, c, c.org, org.ActionPolicyRead)
			if err != nil {
				return false, err
			}
			may = &allowed
		}
		return *may, nil
	}
	out := make([]apigen.ExplainedGround, 0, len(reasons))
	for _, reason := range reasons {
		ground, err := s.groundOf(r, c, target, d.Allowed, reason, readable)
		if err != nil {
			return nil, err
		}
		out = append(out, ground)
	}
	return out, nil
}

// groundOf names one reason. A policy is told apart by how the service
// names it: a stored policy of the caller's organization by its GRN,
// which is a role link where the identifier is one the platform
// reserves (ADR-0161, ADR-0163) and otherwise a guardrail where the
// decision is a deny and a permit where it is an allow, since a
// guardrail only forbids and a permit only permits; a system policy by
// its file name; and a sending organization's guardrail by
// `<organization GRN>/<its GRN>`, which names a policy that is not the
// caller's organization's to read, so it is named by its kind alone. A
// reason that carries an error is an error, beside the policy it arose
// in where that one is named.
func (s *Server) groundOf(
	r *http.Request, c *caller, target grn.GRN, allowed bool, reason policydecision.Reason,
	readable func() (bool, error),
) (apigen.ExplainedGround, error) {
	ground := apigen.ExplainedGround{Kind: guardrailOrPermit(allowed)}
	if reason.Error != "" {
		ground.Kind, ground.Error = apigen.GroundError, &reason.Error
	}
	switch {
	case reason.PolicyID == "":
		ground.Kind = apigen.GroundError
		return ground, nil
	case !strings.HasPrefix(reason.PolicyID, "grn:"):
		ground.Policy = &reason.PolicyID
		if reason.Error == "" {
			ground.Kind = apigen.GroundSystemPolicy
		}
		return ground, nil
	}
	g, err := grn.Parse(reason.PolicyID)
	if err != nil || g.Organization != c.org.Identifier || g.ServiceType != serviceTypePolicy {
		return ground, nil
	}
	id := g.Identifier
	ground.Policy = &id
	if reason.Error != "" {
		return ground, nil
	}
	if role, scoped, ok := linkRole(id); ok {
		nameLink(&ground, target, role, scoped)
		return ground, nil
	}
	may, err := readable()
	if err != nil || !may {
		return ground, err
	}
	return ground, s.readContent(r, c, &ground, id)
}

// guardrailOrPermit is the kind of a stored policy that is no role
// link, read off the decision it took part in.
func guardrailOrPermit(allowed bool) apigen.ExplainedGroundKind {
	if allowed {
		return apigen.GroundPermit
	}
	return apigen.GroundGuardrail
}

// linkRole reads the role a role link's reserved identifier holds, and
// whether it is the link of a team or a project. A policy of the
// organization that opens neither prefix is no role link.
func linkRole(id string) (role org.Role, scoped, ok bool) {
	rest, scoped := strings.CutPrefix(id, scopedLinkPrefix)
	if !scoped {
		var found bool
		if rest, found = strings.CutPrefix(id, linkPrefix); !found {
			return "", false, false
		}
	}
	word, _, _ := strings.Cut(rest, "-")
	role, err := org.ParseRole(word)
	if err != nil {
		return "", false, false
	}
	return role, scoped, true
}

// nameLink names a role link by its role and the path it reaches the
// caller by. An organization role reaches the caller through their
// membership. The role of a team or a project is the link of that team
// or project on itself (ADR-0163), so it reaches the caller on the
// team or the project the question names and on none other; a link
// asked about a resource that is neither carries no path.
func nameLink(ground *apigen.ExplainedGround, target grn.GRN, role org.Role, scoped bool) {
	ground.Kind = apigen.GroundRoleLink
	apiRole := apigen.Role(role)
	ground.Role = &apiRole
	path := apigen.GroundPathOrganization
	slug := target.Identifier
	switch {
	case !scoped:
	case target.ServiceType == org.ServiceTypeGroup:
		path = apigen.GroundPathTeam
		ground.Team = &slug
	case target.ServiceType == org.ServiceTypeProject:
		path = apigen.GroundPathProject
		ground.Project = &slug
	default:
		return
	}
	ground.Path = &path
}

// readContent names a guardrail or a permit by the name and the text
// the policy service holds for it, read as the caller. A policy the
// service no longer shows the caller stays named without them: what
// decided is the delivered state, and a read is the current one.
func (s *Server) readContent(r *http.Request, c *caller, ground *apigen.ExplainedGround, id string) error {
	if s.policyRelay == nil {
		return errorx.New(errorx.KindUnavailable, "policy service is not configured")
	}
	detail, err := s.policyRelay.Get(r.Context(), policyrelay.Write{Writer: c.user.GRN, Org: c.org, ID: id})
	if errorx.IsKind(err, errorx.KindPermissionDenied) || errorx.IsKind(err, errorx.KindNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	name := detail.Policy.Name
	ground.Name = &name
	for _, v := range detail.Versions {
		if v.Number == detail.Policy.DefaultVersion && v.Body.Text != "" {
			text := v.Body.Text
			ground.Text = &text
		}
	}
	return nil
}
