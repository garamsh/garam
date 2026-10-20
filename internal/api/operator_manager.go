package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// SetOperatorManager links the operator, as a control service, to the
// cluster manager the body names, once (ADR-0124). It is authorized on
// the action that registers, since it records what registering the
// pair would have: it links a pair registered before the two could be
// registered together.
func (s *Server) SetOperatorManager(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorRegister); err != nil {
		writeError(w, err)
		return
	}
	g, err := operator.GRNOf(orgGRN(orgSlug), operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.OperatorManagerWrite
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	manager, err := operator.ParseManager(g, body.Manager)
	if err != nil {
		writeError(w, err)
		return
	}
	linked, err := s.operators.SetManager(r.Context(), g, manager)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operatorToAPI(linked))
}
