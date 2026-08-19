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
	statements := []Statement{{
		Sid:      "org-role-" + string(m.Role()),
		Effect:   wire.EffectAllow,
		Action:   m.Role().Actions(),
		Resource: []string{orgGRN.String(), orgScope(orgGRN)},
	}}
	statements = append(statements, userStatements(orgGRN, u)...)
	var derived []Statement
	var err error
	switch target.ServiceType {
	case org.ServiceTypeGroup:
		derived, err = s.groupStatements(r, target, u.GRN())
	case org.ServiceTypeProject:
		derived, err = s.projectStatements(r, target, u.GRN())
	}
	if err != nil {
		return nil, err
	}
	return append(statements, derived...), nil
}

// userStatements returns what the roles stored on the subject grant,
// scoped like the membership role. Unknown names grant nothing;
// attached policies belong to the engine (ADR-0008).
func userStatements(orgGRN grn.GRN, u *user.User) []Statement {
	actions := org.ActionsFor(u.Roles())
	if len(actions) == 0 {
		return nil
	}
	return []Statement{{
		Sid:      "user-role-" + u.GRN().Identifier,
		Effect:   wire.EffectAllow,
		Action:   actions,
		Resource: []string{orgGRN.String(), orgScope(orgGRN)},
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
	if !slices.Contains(members, subject) {
		return nil, nil
	}
	actions := org.ActionsFor(g.Roles())
	if len(actions) == 0 {
		return nil, nil
	}
	return []Statement{{
		Sid:      "group-role-" + groupGRN.Identifier,
		Effect:   wire.EffectAllow,
		Action:   actions,
		Resource: []string{groupGRN.String()},
	}}, nil
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
	if !slices.Contains(members, subject) {
		return nil, nil
	}
	actions := org.ActionsFor(p.Roles())
	if len(actions) == 0 {
		return nil, nil
	}
	return []Statement{{
		Sid:      "project-role-" + projectGRN.Identifier,
		Effect:   wire.EffectAllow,
		Action:   actions,
		Resource: []string{projectGRN.String()},
	}}, nil
}

// orgScope is the resource pattern for everything the org owns. Org
// GRNs are root-owned, so the org identifier is the organization
// segment of every resource beneath it.
func orgScope(orgGRN grn.GRN) string {
	return "grn:" + orgGRN.Identifier + ":*"
}
