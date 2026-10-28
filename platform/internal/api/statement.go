package api

import (
	"net/http"
	"slices"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	"github.com/garamsh/garam/internal/wire"
)

// scopedStatements collects the statements applying to the subject
// when acting on target: the membership role, the subject's direct
// roles, and the roles the target group or project grants its members.
func (s *Server) scopedStatements(r *http.Request, orgGRN grn.GRN, m *org.Membership, target grn.GRN, u *user.User) ([]Statement, error) {
	statements := memberStatements(orgGRN, m, u)
	var derived []Statement
	var err error
	switch target.ServiceType {
	case org.ServiceTypeGroup:
		derived, err = s.groupStatements(r, target, u.GRN)
	case org.ServiceTypeProject:
		derived, err = s.projectStatements(r, target, u.GRN)
	}
	if err != nil {
		return nil, err
	}
	return append(statements, derived...), nil
}

// memberStatements returns what applies to the member whatever it acts
// on: its membership role and its direct roles, both scoped to the org.
func memberStatements(orgGRN grn.GRN, m *org.Membership, u *user.User) []Statement {
	statements := []Statement{{
		Sid:      "org-role-" + string(m.Role),
		Effect:   wire.EffectAllow,
		Action:   m.Role.Actions(),
		Resource: []string{orgGRN.String(), orgScope(orgGRN)},
		Origin:   Origin{Kind: OriginOrgRole, Roles: []string{string(m.Role)}},
	}}
	return append(statements, userStatements(orgGRN, u)...)
}

// userStatements returns what the roles stored on the subject grant,
// scoped like the membership role. Unknown names grant nothing;
// attached policies belong to the engine (ADR-0008).
func userStatements(orgGRN grn.GRN, u *user.User) []Statement {
	actions := org.ActionsFor(u.Roles)
	if len(actions) == 0 {
		return nil
	}
	return []Statement{{
		Sid:      "user-role-" + u.GRN.Identifier,
		Effect:   wire.EffectAllow,
		Action:   actions,
		Resource: []string{orgGRN.String(), orgScope(orgGRN)},
		Origin:   Origin{Kind: OriginDirectRole, Roles: u.Roles},
	}}
}

// groupStatements returns what the group grants the subject as one of
// its members, scoped to the group's own GRN. A group the subject does
// not belong to grants nothing; a missing group is left to the handler
// to report.
func (s *Server) groupStatements(r *http.Request, groupGRN, subject grn.GRN) ([]Statement, error) {
	g, err := s.orgs.GetGroup(r.Context(), groupGRN)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	members, err := s.orgs.ListGroupMembers(r.Context(), groupGRN)
	if err != nil {
		return nil, err
	}
	return groupRoleStatements(g, slices.Contains(members, subject)), nil
}

// groupRoleStatements returns what g's roles grant a subject that is
// one of its members, scoped to g's own GRN, and nothing to one that
// is not.
func groupRoleStatements(g *org.Group, member bool) []Statement {
	actions := org.ActionsFor(g.Roles)
	if !member || len(actions) == 0 {
		return nil
	}
	return []Statement{{
		Sid:      "group-role-" + g.GRN.Identifier,
		Effect:   wire.EffectAllow,
		Action:   actions,
		Resource: []string{g.GRN.String()},
		Origin:   Origin{Kind: OriginGroupRole, Roles: g.Roles, Via: g.GRN},
	}}
}

// projectStatements returns what the project grants the subject as one
// of its members, scoped to the project's own GRN. A project the
// subject does not belong to grants nothing; a missing project is left
// to the handler to report.
func (s *Server) projectStatements(r *http.Request, projectGRN, subject grn.GRN) ([]Statement, error) {
	p, err := s.orgs.GetProject(r.Context(), projectGRN)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	members, err := s.orgs.ListProjectMembers(r.Context(), projectGRN)
	if err != nil {
		return nil, err
	}
	return projectRoleStatements(p, slices.Contains(members, subject)), nil
}

// projectRoleStatements returns what p's roles grant a subject that is
// one of its members, scoped to p's own GRN, and nothing to one that
// is not.
func projectRoleStatements(p *org.Project, member bool) []Statement {
	actions := org.ActionsFor(p.Roles)
	if !member || len(actions) == 0 {
		return nil
	}
	return []Statement{{
		Sid:      "project-role-" + p.GRN.Identifier,
		Effect:   wire.EffectAllow,
		Action:   actions,
		Resource: []string{p.GRN.String()},
		Origin:   Origin{Kind: OriginProjectRole, Roles: p.Roles, Via: p.GRN},
	}}
}

// orgScope is the resource pattern for everything the org owns. Org
// GRNs are root-owned, so the org identifier is the organization
// segment of every resource beneath it.
func orgScope(orgGRN grn.GRN) string {
	return "grn:" + orgGRN.Identifier + ":*"
}
