package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// RegisterOperator records that the org has an operator under the
// identifier given (ADR-0045) and mints the one-time token it enrols
// with (ADR-0064). Requires the admin role.
//
// The token is answered here and nowhere else. garam stores its hash,
// so this response is the only place it exists in a form anyone can
// present, and the record and the token are written together — a
// registration that arrived without one would be an operator nothing
// can enrol.
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
	o, err := operator.New(orgGRN(orgSlug), body.Identifier, u.GRN())
	if err != nil {
		writeError(w, err)
		return
	}
	enrollment, err := operator.NewEnrollment(o.GRN(), u.GRN())
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.operators.Register(r.Context(), o, enrollment); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apigen.RegisteredOperator{
		Operator: operatorToAPI(o),
		Enrollment: apigen.OperatorEnrollment{
			Token:     enrollment.Token(),
			ExpiresAt: enrollment.ExpiresAt(),
		},
	})
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

// requireRegisteredOperator refuses a GRN the org holds no record of
// (ADR-0045). The refusal is a bad request and not a missing resource:
// the caller named the operator in a body. It names the identifier
// alone, which is the whole of what was wrong, because the console
// renders the sentence it gets without printing a GRN (ADR-0011).
func (s *Server) requireRegisteredOperator(r *http.Request, g grn.GRN) error {
	_, err := s.operators.Get(r.Context(), g)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return errorx.New(errorx.KindInvalidArgument,
			"this organization has registered no operator named "+g.Identifier)
	}
	return err
}

// operatorToAPI renders an Operator. It carries no state: garam never
// dials an operator (ADR-0012), so a record says the organization
// registered one and nothing about whether it is running.
func operatorToAPI(o *operator.Operator) apigen.Operator {
	return apigen.Operator{
		Grn:          o.GRN().String(),
		OrgGrn:       o.Organization().String(),
		RegisteredBy: o.RegisteredBy().String(),
		CreatedAt:    o.CreatedAt(),
	}
}
