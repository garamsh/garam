package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/audit"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// RegisterOperator records that the org has an operator under the
// identifier given (ADR-0045) and mints the one-time token it enrols
// with (ADR-0064). Requires the admin role.
//
// garam stores the token's hash, so no route reads one back and this
// response is the only place this token exists in a form anyone can
// present. The record and the token are written together — a
// registration that arrived without one would be an operator nothing
// can enrol until [Server.MintOperatorEnrollment] mints for it.
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
	enrollment, err := operator.NewEnrollment(o.GRN(), u.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	credential, err := operator.NewIssuedCredential(o.GRN(), u.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	registered, err := s.operators.Register(r.Context(), o, enrollment, credential)
	if err != nil {
		// A registration issues both, so its refusal refuses both.
		s.refusedCredential(r, o.GRN(), audit.CredentialOperatorEnrollmentToken, err)
		s.refusedCredential(r, o.GRN(), audit.CredentialOperatorCredential, err)
		writeError(w, err)
		return
	}
	s.issuedEnrollment(r, enrollment)
	s.auditCredential(r, audit.CredentialIssued(o.GRN(), audit.CredentialOperatorCredential, audit.ActionIssue, "",
		audit.Validity{NotBefore: credential.IssuedAt()}, false))
	writeJSON(w, http.StatusCreated, apigen.RegisteredOperator{
		Operator: operatorToAPI(registered),
		Enrollment: apigen.OperatorEnrollment{
			Token:     enrollment.Token(),
			ExpiresAt: enrollment.ExpiresAt(),
		},
		Credential: credentialToAPI(credential),
	})
}

// ReissueOperatorCredential issues the operator a new credential and
// answers it once, voiding the previous one and every token exchanged
// for it (ADR-0100). Requires the admin role.
//
// It is authorized on an action of its own rather than on the one that
// registers, for the reason DeleteOperator is: it withdraws what the
// operator runs on until the new credential is placed.
func (s *Server) ReissueOperatorCredential(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionOperatorReissue); err != nil {
		writeError(w, err)
		return
	}
	g, err := operator.GRNOf(orgGRN(orgSlug), operatorSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	credential, err := operator.NewIssuedCredential(g, u.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.operators.ReissueCredential(r.Context(), credential); err != nil {
		s.refusedCredential(r, g, audit.CredentialOperatorCredential, err)
		writeError(w, err)
		return
	}
	s.auditCredential(r, audit.CredentialIssued(g, audit.CredentialOperatorCredential, audit.ActionIssue, "",
		audit.Validity{NotBefore: credential.IssuedAt()}, false))
	writeJSON(w, http.StatusCreated, credentialToAPI(credential))
}

// issuedEnrollment records the enrollment token e minted: when it was
// minted and until when it is spendable, and never the token.
func (s *Server) issuedEnrollment(r *http.Request, e *operator.Enrollment) {
	s.auditCredential(r, audit.CredentialIssued(e.Operator(), audit.CredentialOperatorEnrollmentToken, audit.ActionIssue, "",
		audit.Validity{NotBefore: e.MintedAt(), NotAfter: e.ExpiresAt()}, false))
}

func credentialToAPI(c *operator.IssuedCredential) apigen.OperatorCredential {
	return apigen.OperatorCredential{Credential: c.Value(), IssuedAt: c.IssuedAt()}
}

// MintOperatorEnrollment mints the operator a fresh enrollment token
// and answers it once, refusing an operator that already enrolled
// (ADR-0067). Requires the admin role.
//
// It is authorized exactly as registration is, on the org and on the
// action that registers: re-minting where no certificate exists grants
// what registering the same identifier again would grant, so a member
// who may do the one may do the other, and no action was added for it.
//
// The refusal for an enrolled operator is the store's, decided beside
// the write rather than here: this handler holds no second reading of
// what a certificate outranks.
func (s *Server) MintOperatorEnrollment(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, operatorSlug apigen.OperatorSlug) {
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
	enrollment, err := operator.NewEnrollment(g, u.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.operators.Remint(r.Context(), enrollment); err != nil {
		s.refusedCredential(r, g, audit.CredentialOperatorEnrollmentToken, err)
		writeError(w, err)
		return
	}
	s.issuedEnrollment(r, enrollment)
	writeJSON(w, http.StatusCreated, apigen.OperatorEnrollment{
		Token:     enrollment.Token(),
		ExpiresAt: enrollment.ExpiresAt(),
	})
}

// DeleteOperator removes the org's record of the operator, which stops
// that operator authenticating (ADR-0070). Requires the admin role.
//
// It is authorized on an action of its own rather than on the one that
// registers: registering grants nothing, and this withdraws what an
// operator runs on, so a member who may add one does not thereby hold
// the power to stop one.
//
// The refusal for an operator a definition names is the store's,
// decided by the foreign key beside the write: this handler holds no
// second count of what names the operator.
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

// operatorToAPI renders an Operator. The enrollment state is what garam
// holds about the operator rather than anything the operator reports:
// garam never dials one (ADR-0012), so `enrolled` says a certificate
// was issued and nothing about whether the operator is running.
//
// certificateIssuedAt is answered only where garam has issued one,
// because an absent timestamp and a zero one are different states and
// only the absence is the property.
func operatorToAPI(o *operator.Operator) apigen.Operator {
	out := apigen.Operator{
		Grn:             o.GRN().String(),
		Name:            o.Name,
		Description:     o.Description,
		OrgGrn:          o.Organization().String(),
		RegisteredBy:    o.RegisteredBy().String(),
		CreatedAt:       o.CreatedAt(),
		EnrollmentState: apigen.OperatorEnrollmentState(o.EnrollmentState()),
	}
	if issuedAt := o.CertificateIssuedAt(); !issuedAt.IsZero() {
		out.CertificateIssuedAt = &issuedAt
	}
	if o.ControlEndpoint != nil {
		endpoint := string(*o.ControlEndpoint)
		out.ControlEndpoint = &endpoint
	}
	return out
}
