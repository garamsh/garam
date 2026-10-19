package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// RegisterOperator records that the org has an operator under the
// identifier given (ADR-0045). Requires the admin role. It issues
// nothing: the operator enters by a key, which IssueOperatorKey issues
// on the registration (ADR-0102, ADR-0117).
func (s *Server) RegisterOperator(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorRegister); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.OperatorRegistration
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	details, err := operator.NewDetailsPatch(body.Name, body.Description)
	if err != nil {
		writeError(w, err)
		return
	}
	o, err := operator.New(orgGRN(orgSlug), body.Identifier, u.GRN, details)
	if err != nil {
		writeError(w, err)
		return
	}
	registered, err := s.operators.Register(r.Context(), o)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, operatorToAPI(registered))
}

// DeleteOperator removes the org's record of the operator, which stops
// that operator authenticating (ADR-0070). Requires the admin role.
//
// It is authorized on an action of its own rather than on the one that
// registers: registering grants nothing, and this withdraws what an
// operator runs on, so a member who may add one does not thereby hold
// the power to stop one.
func (s *Server) DeleteOperator(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorDelete); err != nil {
		writeError(w, err)
		return
	}
	g, err := operator.GRNOf(orgGRN(orgSlug), operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.operators.Delete(r.Context(), g); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetOperator answers one operator the org has registered. Requires the
// member role.
func (s *Server) GetOperator(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorRead); err != nil {
		writeError(w, err)
		return
	}
	g, err := operator.GRNOf(orgGRN(orgSlug), operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	o, err := s.operators.Get(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operatorToAPI(o))
}

// UpdateOperator changes the operator's name and description
// (ADR-0074). Requires the admin role.
//
// It is authorized on an action of its own: renaming grants nothing a
// registration does and withdraws nothing a delete does, so neither of
// those actions is the right one to carry it.
func (s *Server) UpdateOperator(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorUpdate); err != nil {
		writeError(w, err)
		return
	}
	g, err := operator.GRNOf(orgGRN(orgSlug), operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.OperatorPatch
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	patch, err := operator.NewDetailsPatch(body.Name, body.Description)
	if err != nil {
		writeError(w, err)
		return
	}
	o, err := s.operators.UpdateDetails(r.Context(), g, patch)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operatorToAPI(o))
}

// ListOperators lists the operators the org has registered. Requires
// the member role.
func (s *Server) ListOperators(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorRead); err != nil {
		writeError(w, err)
		return
	}
	operators, err := s.operators.ListByOrg(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Operator, 0, len(operators))
	for _, o := range operators {
		out = append(out, operatorToAPI(o))
	}
	writeJSON(w, http.StatusOK, out)
}

// operatorToAPI renders an Operator from what garam holds about it
// rather than anything the operator reports: garam never dials one
// (ADR-0012).
func operatorToAPI(o *operator.Operator) apigen.Operator {
	out := apigen.Operator{
		Grn:          o.GRN().String(),
		Name:         o.Name,
		Description:  o.Description,
		OrgGrn:       o.Organization().String(),
		RegisteredBy: o.RegisteredBy().String(),
		CreatedAt:    o.CreatedAt(),
	}
	if o.ControlEndpoint != nil {
		endpoint := string(*o.ControlEndpoint)
		out.ControlEndpoint = &endpoint
	}
	return out
}
