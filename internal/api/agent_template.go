package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// ListAgentTemplates lists the org's templates, leaving out each
// template whose own read the caller is denied.
func (s *Server) ListAgentTemplates(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := s.authorizedCaller(r, orgGRN(orgSlug), orgGRN(orgSlug), u, org.ActionAgentTemplateRead)
	if err != nil {
		writeError(w, err)
		return
	}
	templates, err := s.agents.ListTemplatesByOrg(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	templates, err = visible(s, r, c, org.ActionAgentTemplateRead, templates, func(t *agent.Template) BatchObject { return onItself(t.GRN) })
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.AgentTemplate, 0, len(templates))
	for _, t := range templates {
		out = append(out, templateToAPI(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetAgentTemplate returns one template of the org.
func (s *Server) GetAgentTemplate(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, templateSlug apigen.TemplateSlug) {
	g, err := s.authorizeTemplate(r, orgSlug, templateSlug, org.ActionAgentTemplateRead)
	if err != nil {
		writeError(w, err)
		return
	}
	t, err := s.agents.GetTemplate(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, templateToAPI(t))
}

// authorizeTemplate enforces action on the template the path names, so
// a policy scoped to one template reaches that template alone.
func (s *Server) authorizeTemplate(r *http.Request, orgSlug, templateSlug, action string) (grn.GRN, error) {
	u, err := s.currentUser(r)
	if err != nil {
		return grn.GRN{}, err
	}
	g := templateGRN(orgSlug, templateSlug)
	if err := s.authorizeOn(r, orgGRN(orgSlug), g, u, action); err != nil {
		return grn.GRN{}, err
	}
	return g, nil
}

// templateGRN builds the GRN of a template owned by the org. The org
// segment scopes the identifier, so a template of another org is not
// addressable through that org's paths.
func templateGRN(orgSlug, templateSlug string) grn.GRN {
	return grn.New(orgSlug, grn.DefaultProject, agent.ServiceTypeTemplate, templateSlug)
}

// templateToAPI renders a Template.
func templateToAPI(t *agent.Template) apigen.AgentTemplate {
	return apigen.AgentTemplate{
		Grn:         t.GRN.String(),
		OrgGrn:      t.OrgGRN.String(),
		Name:        string(t.Name),
		Description: string(t.Description),
		Values:      apigen.AgentValues(t.Values),
		CreatedBy:   t.CreatedBy.String(),
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}
