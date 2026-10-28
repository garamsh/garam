package api

import (
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/wire"
)

// ExplainPermission answers why the signed-in caller may or may not
// take one action: the decision checkPermissions answers for the pair,
// and where the engine decides it, the statements that did, each with
// the path through which it reaches the caller. A policy statement the
// caller may not read is answered without its content. Requires
// membership.
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
// action the engine's Check does not alone decide is answered with
// checkPermissions' decision and no statements, since what decides it
// is the operation.
func (s *Server) explain(r *http.Request, c *caller, target grn.GRN, action string) (apigen.PermissionExplanation, error) {
	none := []apigen.ExplainedStatement{}
	if !org.IsAction(action) {
		return apigen.PermissionExplanation{Decision: apigen.DecisionDeny, Basis: apigen.BasisUndefinedAction, Statements: none}, nil
	}
	if actionRules[action].decidedBy != decidedByCheck {
		decision, err := s.decide(r, c, target, action)
		if err != nil {
			return apigen.PermissionExplanation{}, err
		}
		return apigen.PermissionExplanation{Decision: decision, Basis: apigen.BasisOperation, Statements: none}, nil
	}
	in, err := s.question(r, c, target, action)
	if err != nil {
		return apigen.PermissionExplanation{}, err
	}
	ex, err := s.evaluator.Explain(r.Context(), in)
	if err != nil {
		return apigen.PermissionExplanation{}, err
	}
	decision, deciding := apigen.DecisionDeny, wire.EffectDeny
	if ex.Allowed {
		decision, deciding = apigen.DecisionAllow, wire.EffectAllow
	}
	statements, err := s.decidingStatements(r, c, in.Statements, ex.Traces, deciding)
	if err != nil {
		return apigen.PermissionExplanation{}, err
	}
	return apigen.PermissionExplanation{Decision: decision, Basis: apigen.BasisStatements, Statements: statements}, nil
}

// decidingStatements renders the traces that matched with the effect
// that decided. A collected statement is named by the role it came
// from; a policy statement by its policy and each path it reached the
// caller by, where the caller may read the policy, and otherwise once,
// as unreadable, however many such statements decided.
func (s *Server) decidingStatements(r *http.Request, c *caller, collected []Statement, traces []Trace, deciding wire.Effect) ([]apigen.ExplainedStatement, error) {
	bySid := make(map[string]Statement, len(collected))
	for _, st := range collected {
		bySid[st.Sid] = st
	}
	effect := effectToAPI(deciding)
	readable := map[grn.GRN]bool{}
	out := []apigen.ExplainedStatement{}
	unreadable := false
	for _, t := range traces {
		if !t.Matched || t.Effect != deciding {
			continue
		}
		if t.Policy.IsZero() {
			st, ok := bySid[t.Sid]
			if !ok {
				return nil, errorx.New(errorx.KindInternal, "explain: the engine traced a statement nobody sent")
			}
			out = append(out, roleStatement(st, effect))
			continue
		}
		may, seen := readable[t.Policy]
		if !seen {
			var err error
			if may, err = s.allows(r, c, t.Policy, org.ActionPolicyRead); err != nil {
				return nil, err
			}
			readable[t.Policy] = may
		}
		if !may {
			unreadable = true
			continue
		}
		out = append(out, policyStatements(c, t, effect)...)
	}
	if unreadable {
		out = append(out, apigen.ExplainedStatement{Effect: effect, Source: apigen.SourceUnreadablePolicy})
	}
	return out, nil
}

// roleStatement names a collected statement by the role it came from.
func roleStatement(st Statement, effect apigen.ExplainedStatementEffect) apigen.ExplainedStatement {
	roles := st.Origin.Roles
	out := apigen.ExplainedStatement{Effect: effect, Sid: &st.Sid, Roles: &roles}
	via := st.Origin.Via.Identifier
	switch st.Origin.Kind {
	case OriginOrgRole:
		out.Source = apigen.SourceOrgRole
	case OriginDirectRole:
		out.Source = apigen.SourceDirectRole
	case OriginGroupRole:
		out.Source, out.Group = apigen.SourceGroupRole, &via
	case OriginProjectRole:
		out.Source, out.Project = apigen.SourceProjectRole, &via
	}
	return out
}

// policyStatements names a policy statement once for each path it
// reached the caller by: org-wide, attached to the caller, or attached
// to one of the caller's groups.
func policyStatements(c *caller, t Trace, effect apigen.ExplainedStatementEffect) []apigen.ExplainedStatement {
	named := func(attachment apigen.ExplainedStatementAttachment, group *string) apigen.ExplainedStatement {
		sid, policy := t.Sid, t.Policy.Identifier
		return apigen.ExplainedStatement{Effect: effect, Source: apigen.SourcePolicy, Sid: &sid, Policy: &policy,
			Attachment: &attachment, Group: group}
	}
	if len(t.AttachedTo) == 0 {
		return []apigen.ExplainedStatement{named(apigen.AttachmentOrganization, nil)}
	}
	out := make([]apigen.ExplainedStatement, 0, len(t.AttachedTo))
	for _, subject := range t.AttachedTo {
		if subject == c.user.GRN {
			out = append(out, named(apigen.AttachmentCaller, nil))
			continue
		}
		group := subject.Identifier
		out = append(out, named(apigen.AttachmentGroup, &group))
	}
	return out
}

// effectToAPI renders an effect for an explanation.
func effectToAPI(e wire.Effect) apigen.ExplainedStatementEffect {
	if e == wire.EffectAllow {
		return apigen.StatementEffectAllow
	}
	return apigen.StatementEffectDeny
}
