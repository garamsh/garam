package machineapi

import (
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ListDefinitions answers the calling operator with the definitions
// naming it: the agent GRN each minted, the values, and the claim
// state (ADR-0033). The definition's operator field is the scoping, so
// no policy engine is asked.
func (s *Server) ListDefinitions(w http.ResponseWriter, r *http.Request) {
	operator, ok := caller(w, r)
	if !ok {
		return
	}
	states, err := s.agents.Definitions(r.Context(), operator)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]machinegen.Definition, 0, len(states))
	for _, state := range states {
		d := machinegen.Definition{
			AgentGrn: state.Definition().Agent().String(),
			Values:   map[string]string(state.Definition().Values()),
		}
		if claim := state.Claim(); claim != nil {
			d.Claim = &machinegen.DefinitionClaim{Epoch: claim.Epoch()}
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// ClaimDefinition binds the calling operator to the agent GRN the
// definition minted and answers with the assignment it wrote, which is
// what admits the operator to that agent's certificate route
// (ADR-0033). A second claim of the same agent GRN is a conflict.
func (s *Server) ClaimDefinition(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) {
	operator, ok := caller(w, r)
	if !ok {
		return
	}
	subject, err := grn.Parse(agentGRN)
	if err != nil {
		writeError(w, errorx.Wrap(err, "agent", errorx.KindInvalidArgument))
		return
	}
	assignment, err := s.agents.Claim(r.Context(), operator, subject)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, machinegen.AgentAssignment{
		Grn:      assignment.Agent().String(),
		Operator: assignment.Operator().String(),
		Epoch:    assignment.Epoch(),
	})
}
