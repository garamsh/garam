package api

import (
	"context"
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/org"
)

// ListAgents lists the org's agents. Requires the member role.
func (s *Server) ListAgents(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListAgentsParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionAgentRead); err != nil {
		writeError(w, err)
		return
	}
	f, err := listfilter.Parse(params.Limit, params.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	agents, err := s.agents.ListByOrg(r.Context(), orgGRN(orgSlug), f)
	if err != nil {
		writeError(w, err)
		return
	}
	assignments, err := s.assignmentsOf(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Agent, 0, len(agents))
	for _, a := range agents {
		out = append(out, agentToAPI(a, assignments[a.GRN().String()]))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetAgent returns an agent of the org. Requires the member role.
func (s *Server) GetAgent(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, agentSlug apigen.AgentSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := agentGRN(orgSlug, agentSlug)
	if err := s.authorizeOn(r, orgGRN(orgSlug), g, u, org.ActionAgentRead); err != nil {
		writeError(w, err)
		return
	}
	a, err := s.agents.Get(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	assignment, err := s.assignmentOf(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentToAPI(a, assignment))
}

// DefineAgent records what an agent of the org should be: which
// operator is to run it, and the values it is constructed from
// (ADR-0026). Requires the member role, and the operator must be one
// the org has registered (ADR-0045). It creates no agent: the response
// carries the definition's own shape and not an agent's.
func (s *Server) DefineAgent(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionAgentDefine); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.AgentDefinitionCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	operatorGRN, err := grn.Parse(body.OperatorGrn)
	if err != nil {
		writeError(w, errorx.Wrap(err, "operator", errorx.KindInvalidArgument))
		return
	}
	var raw map[string]string
	if body.Values != nil {
		raw = *body.Values
	}
	values, err := agent.ParseValues(raw)
	if err != nil {
		writeError(w, err)
		return
	}
	// A name left out is none given; one given must parse, so an empty
	// or blank one is refused rather than read as left out.
	var name agent.Name
	if body.Name != nil {
		if name, err = agent.ParseName(*body.Name); err != nil {
			writeError(w, err)
			return
		}
	}
	d, err := agent.NewDefinition(orgGRN(orgSlug), operatorGRN, name, values, u.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	// NewDefinition proves the GRN names an operator of this org; this
	// proves the org registered it. The two refusals name different
	// things wrong, so they stay apart.
	if err := s.requireRegisteredOperator(r, operatorGRN); err != nil {
		writeError(w, err)
		return
	}
	if err := s.agents.Define(r.Context(), d); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, definitionToAPI(d))
}

// ListAgentDefinitions lists the agents the org says it should have.
// Requires the member role. An entry is a definition and not an agent:
// nothing here reads the agent rows an operator reports.
func (s *Server) ListAgentDefinitions(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionAgentRead); err != nil {
		writeError(w, err)
		return
	}
	definitions, err := s.agents.ListDefinitionsByOrg(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.AgentDefinition, 0, len(definitions))
	for _, d := range definitions {
		out = append(out, definitionToAPI(d))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetAgentDefinition returns the definition that minted an agent of the
// org. Requires the member role. It answers what the org asked for and
// nothing an operator reported.
func (s *Server) GetAgentDefinition(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, agentSlug apigen.AgentSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := agentGRN(orgSlug, agentSlug)
	if err := s.authorizeOn(r, orgGRN(orgSlug), g, u, org.ActionAgentRead); err != nil {
		writeError(w, err)
		return
	}
	d, err := s.agents.GetDefinition(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitionToAPI(d))
}

// RenameAgentDefinition changes what the org calls a defined agent and
// nothing else (ADR-0075). Requires the member role, on an action of
// its own: the name is not a value any operator reads.
func (s *Server) RenameAgentDefinition(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, agentSlug apigen.AgentSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := agentGRN(orgSlug, agentSlug)
	if err := s.authorizeOn(r, orgGRN(orgSlug), g, u, org.ActionAgentRename); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.AgentDefinitionPatch
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	name, err := agent.ParseName(body.Name)
	if err != nil {
		writeError(w, err)
		return
	}
	d, err := s.agents.RenameDefinition(r.Context(), g, name)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, definitionToAPI(d))
}

// assignmentsOf resolves the org's assignments in one query, keyed by
// agent GRN. An agent assigned between the two reads is absent from the
// map: the reads are not atomic, and the next request carries it.
func (s *Server) assignmentsOf(ctx context.Context, orgGRN grn.GRN) (map[string]*agent.Assignment, error) {
	assignments, err := s.agents.ListAssignmentsByOrg(ctx, orgGRN)
	if err != nil {
		return nil, err
	}
	byAgent := make(map[string]*agent.Assignment, len(assignments))
	for _, a := range assignments {
		byAgent[a.Agent().String()] = a
	}
	return byAgent, nil
}

// assignmentOf returns the assignment holding the agent, or nil when
// none does. Assignment is a relation rather than part of the agent
// (ADR-0012), so an agent no operator holds is an ordinary answer.
func (s *Server) assignmentOf(ctx context.Context, g grn.GRN) (*agent.Assignment, error) {
	a, err := s.agents.GetAssignment(ctx, g)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// agentGRN builds the GRN of an agent owned by the org. The org
// segment scopes the identifier, so an agent of another org is not
// addressable through that org's paths.
func agentGRN(orgSlug, agentSlug string) grn.GRN {
	return grn.New(orgSlug, grn.DefaultProject, agent.ServiceType, agentSlug)
}

// agentToAPI renders an Agent together with the assignment holding it,
// which is nil when nothing does. The two states stay separate
// fields: different parties report them (ADR-0012).
func agentToAPI(a *agent.Agent, assignment *agent.Assignment) apigen.Agent {
	out := apigen.Agent{
		Grn:               a.GRN().String(),
		OrgGrn:            a.Organization().String(),
		ProvisioningState: apigen.AgentProvisioningState(a.ProvisioningState()),
		RuntimeState:      apigen.AgentRuntimeState(a.RuntimeState()),
		CreatedAt:         a.CreatedAt(),
		UpdatedAt:         a.UpdatedAt(),
	}
	if assignment != nil {
		out.Assignment = &apigen.AgentAssignment{
			AssigneeGrn: assignment.Assignee().String(),
			Epoch:       assignment.Epoch(),
			Since:       assignment.UpdatedAt(),
		}
		// The legacy field names an operator and never a supervisor.
		if assignment.Assignee().ServiceType == agent.ServiceTypeOperator {
			operatorGRN := assignment.Assignee().String()
			out.Assignment.OperatorGrn = &operatorGRN
		}
	}
	return out
}

// definitionToAPI renders a Definition. It shares no field with
// [agentToAPI]: a definition carries no state, because no one has
// reported any (ADR-0026), and the agent GRN it names is the identity
// minted for an agent that may not exist.
func definitionToAPI(d *agent.Definition) apigen.AgentDefinition {
	return apigen.AgentDefinition{
		AgentGrn:    d.Agent().String(),
		OrgGrn:      d.Organization().String(),
		Name:        string(d.Name),
		OperatorGrn: d.Operator().String(),
		Values:      apigen.AgentValues(d.Values()),
		DefinedBy:   d.DefinedBy().String(),
		CreatedAt:   d.CreatedAt(),
	}
}
