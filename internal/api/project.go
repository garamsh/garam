package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/org"
)

// CreateProject creates a project in the org. Requires the admin role.
func (s *Server) CreateProject(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionProjectCreate); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.ProjectCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Name == "" {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "name is required"))
		return
	}
	roles, err := scopedRoles(body.Roles)
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := randomHex(8)
	if err != nil {
		writeError(w, err)
		return
	}
	p := org.NewProject(projectGRN(orgSlug, id), body.Name, orgGRN(orgSlug), roles...)
	if err := s.orgs.CreateProject(r.Context(), p); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, projectToAPI(p))
}

// ListProjects lists the org's projects. Requires the member role.
func (s *Server) ListProjects(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListProjectsParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionProjectRead); err != nil {
		writeError(w, err)
		return
	}
	f, err := listfilter.Parse(params.Limit, params.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	projects, err := s.orgs.ListProjects(r.Context(), orgGRN(orgSlug), f)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Project, 0, len(projects))
	for _, p := range projects {
		out = append(out, projectToAPI(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetProject returns a project of the org. Requires the member role.
func (s *Server) GetProject(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, projectSlug apigen.ProjectSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), projectGRN(orgSlug, projectSlug), u, org.ActionProjectRead); err != nil {
		writeError(w, err)
		return
	}
	p, err := s.orgs.GetProject(r.Context(), projectGRN(orgSlug, projectSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectToAPI(p))
}

// UpdateProject renames a project. Requires the admin role.
func (s *Server) UpdateProject(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, projectSlug apigen.ProjectSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), projectGRN(orgSlug, projectSlug), u, org.ActionProjectUpdate); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.ProjectPatch
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Name != nil && *body.Name == "" {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "name must not be empty"))
		return
	}
	roles, err := scopedRoles(body.Roles)
	if err != nil {
		writeError(w, err)
		return
	}
	p, err := s.orgs.UpdateProject(r.Context(), projectGRN(orgSlug, projectSlug), org.ProjectPatch{Name: body.Name, Roles: roles})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, projectToAPI(p))
}

// DeleteProject removes a project. Requires the admin role.
func (s *Server) DeleteProject(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, projectSlug apigen.ProjectSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), projectGRN(orgSlug, projectSlug), u, org.ActionProjectDelete); err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.DeleteProject(r.Context(), projectGRN(orgSlug, projectSlug)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListProjectMembers lists a project's members. Requires the member
// role.
func (s *Server) ListProjectMembers(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, projectSlug apigen.ProjectSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), projectGRN(orgSlug, projectSlug), u, org.ActionProjectMemberList); err != nil {
		writeError(w, err)
		return
	}
	g := projectGRN(orgSlug, projectSlug)
	// A missing project must 404 rather than list as empty.
	if _, err := s.orgs.GetProject(r.Context(), g); err != nil {
		writeError(w, err)
		return
	}
	members, err := s.orgs.ListProjectMembers(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, memberRefs(members))
}

// AddProjectMember adds a member to a project. Requires the admin
// role.
func (s *Server) AddProjectMember(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, projectSlug apigen.ProjectSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), projectGRN(orgSlug, projectSlug), u, org.ActionProjectMemberAdd); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.MemberRef
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	member, err := grn.Parse(body.MemberGrn)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.AddProjectMember(r.Context(), projectGRN(orgSlug, projectSlug), member); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveProjectMember removes a member from a project. Requires the
// admin role.
func (s *Server) RemoveProjectMember(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, projectSlug apigen.ProjectSlug, memberGrn apigen.MemberGrn) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), projectGRN(orgSlug, projectSlug), u, org.ActionProjectMemberRemove); err != nil {
		writeError(w, err)
		return
	}
	member, err := grn.Parse(memberGrn)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.RemoveProjectMember(r.Context(), projectGRN(orgSlug, projectSlug), member); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// projectGRN builds the GRN of a project owned by the org. The org
// segment scopes the identifier, so a project of another org is not
// addressable through that org's paths.
func projectGRN(orgSlug, projectSlug string) grn.GRN {
	return grn.New(orgSlug, grn.DefaultProject, org.ServiceTypeProject, projectSlug)
}

func projectToAPI(p *org.Project) apigen.Project {
	return apigen.Project{
		Grn:    p.GRN().String(),
		Name:   p.Name(),
		OrgGrn: p.Organization().String(),
		Roles:  rolesToAPI(p.Roles()),
	}
}
