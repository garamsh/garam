package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// CreateAgentTemplate records a template of the org (ADR-0075). It
// defines no agent and names no operator.
func (s *Server) CreateAgentTemplate(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionAgentTemplateCreate); err != nil {
		writeError(w, err)
		return
	}
	content, err := decodeTemplateContent(r)
	if err != nil {
		writeError(w, err)
		return
	}
	t, err := agent.NewTemplate(orgGRN(orgSlug), content, u.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.agents.CreateTemplate(r.Context(), t); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, templateToAPI(t))
}

// ListAgentTemplates lists the org's templates.
func (s *Server) ListAgentTemplates(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionAgentTemplateRead); err != nil {
		writeError(w, err)
		return
	}
	templates, err := s.agents.ListTemplatesByOrg(r.Context(), orgGRN(orgSlug))
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

// UpdateAgentTemplate replaces what a template says. Agents defined
// from it keep the values they copied (ADR-0075).
func (s *Server) UpdateAgentTemplate(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, templateSlug apigen.TemplateSlug) {
	g, err := s.authorizeTemplate(r, orgSlug, templateSlug, org.ActionAgentTemplateUpdate)
	if err != nil {
		writeError(w, err)
		return
	}
	content, err := decodeTemplateContent(r)
	if err != nil {
		writeError(w, err)
		return
	}
	t, err := s.agents.UpdateTemplate(r.Context(), g, content)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, templateToAPI(t))
}

// DeleteAgentTemplate removes a template. Agents defined from it keep
// the values they copied (ADR-0075).
func (s *Server) DeleteAgentTemplate(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, templateSlug apigen.TemplateSlug) {
	g, err := s.authorizeTemplate(r, orgSlug, templateSlug, org.ActionAgentTemplateDelete)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.agents.DeleteTemplate(r.Context(), g); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

// decodeTemplateContent parses the body of a create or an update: the
// whole of what a template says, each field through its own parse.
func decodeTemplateContent(r *http.Request) (agent.TemplateContent, error) {
	var body apigen.AgentTemplateWrite
	if err := decodeJSON(r, &body); err != nil {
		return agent.TemplateContent{}, err
	}
	name, err := agent.ParseName(body.Name)
	if err != nil {
		return agent.TemplateContent{}, err
	}
	var description string
	if body.Description != nil {
		description = *body.Description
	}
	parsedDescription, err := agent.ParseDescription(description)
	if err != nil {
		return agent.TemplateContent{}, err
	}
	var raw map[string]string
	if body.Values != nil {
		raw = *body.Values
	}
	values, err := agent.ParseValues(raw)
	if err != nil {
		return agent.TemplateContent{}, err
	}
	return agent.TemplateContent{Name: name, Description: parsedDescription, Values: values}, nil
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
