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

// ListAgents lists the org's agents, leaving out each agent whose own
// read the caller is denied. Requires the member role.
func (s *Server) ListAgents(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListAgentsParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := s.authorizedCaller(r, orgGRN(orgSlug), orgGRN(orgSlug), u, org.ActionAgentRead)
	if err != nil {
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
	agents, err = visible(s, r, c, org.ActionAgentRead, agents, func(a *agent.Agent) BatchObject { return onItself(a.GRN) })
	if err != nil {
		writeError(w, err)
		return
	}
	assignments, err := s.assignmentsOf(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	generations, err := s.agents.LatestGenerationsByOrg(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Agent, 0, len(agents))
	for _, a := range agents {
		var generation *agent.Generation
		if g, ok := generations[a.GRN]; ok {
			generation = &g
		}
		out = append(out, agentToAPI(a, assignments[a.GRN.String()], generation))
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
	generation, err := s.agents.LatestGeneration(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentToAPI(a, assignment, generation))
}

// RenameAgent sets or clears what the org calls an agent, and nothing
// else (ADR-0119). It is authorized on the agent's own GRN, so a
// policy denying agent:rename on that agent refuses it.
func (s *Server) RenameAgent(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, agentSlug apigen.AgentSlug) {
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
	var body apigen.AgentPatch
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	var name *agent.Name
	if body.Name != nil {
		n, err := agent.ParseName(*body.Name)
		if err != nil {
			writeError(w, err)
			return
		}
		name = &n
	}
	a, err := s.agents.Rename(r.Context(), g, name)
	if err != nil {
		writeError(w, err)
		return
	}
	assignment, err := s.assignmentOf(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	generation, err := s.agents.LatestGeneration(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, agentToAPI(a, assignment, generation))
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
		byAgent[a.Agent.String()] = a
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
// which is nil when nothing does, and the generation garam last
// activated for it, nil before any activation. The two states stay
// separate fields: different parties report them (ADR-0012).
func agentToAPI(a *agent.Agent, assignment *agent.Assignment, generation *agent.Generation) apigen.Agent {
	out := apigen.Agent{
		Grn:               a.GRN.String(),
		OrgGrn:            a.Organization.String(),
		ProvisioningState: apigen.AgentProvisioningState(a.ProvisioningState),
		RuntimeState:      apigen.AgentRuntimeState(a.RuntimeState),
		CreatedAt:         a.CreatedAt,
		UpdatedAt:         a.UpdatedAt,
	}
	if name := a.Name; name != "" {
		n := string(name)
		out.Name = &n
	}
	if generation != nil {
		g := string(*generation)
		out.Generation = &g
	}
	if assignment != nil {
		out.Assignment = &apigen.AgentAssignment{
			AssigneeGrn: assignment.Assignee.String(),
			Epoch:       assignment.Epoch,
			Since:       assignment.UpdatedAt,
		}
	}
	return out
}
