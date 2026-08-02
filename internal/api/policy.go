package api

import (
	"net/http"

	apigen "github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
)

// serviceTypePolicy is the GRN service type of a stored policy. The
// engine owns policies (ADR-0003), so the api server addresses them by
// name rather than importing it.
const serviceTypePolicy = "policy"

// CreatePolicy stores a draft policy with its first version. Requires
// the admin role.
func (s *Server) CreatePolicy(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionPolicyCreate); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.PolicyCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Name == "" {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "name is required"))
		return
	}
	if len(body.Document) == 0 {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "document is required"))
		return
	}
	p, err := s.policies.CreatePolicy(r.Context(), orgGRN(orgSlug), body.Name, body.Document, u.GRN())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, policyToAPI(p))
}

// ListPolicies lists the org's policies. Requires the admin role.
func (s *Server) ListPolicies(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionPolicyRead); err != nil {
		writeError(w, err)
		return
	}
	policies, err := s.policies.ListPolicies(r.Context(), orgGRN(orgSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Policy, 0, len(policies))
	for _, p := range policies {
		out = append(out, policyToAPI(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// ListPolicyVersions lists a policy's versions. Requires the admin
// role.
func (s *Server) ListPolicyVersions(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, policySlug apigen.PolicySlug) {
	g, _, err := s.authorizePolicy(r, orgSlug, policySlug, org.ActionPolicyRead)
	if err != nil {
		writeError(w, err)
		return
	}
	versions, err := s.policies.ListPolicyVersions(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.PolicyVersion, 0, len(versions))
	for _, v := range versions {
		out = append(out, policyVersionToAPI(v))
	}
	writeJSON(w, http.StatusOK, out)
}

// AddPolicyVersion appends a version to a policy. Requires the admin
// role.
func (s *Server) AddPolicyVersion(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, policySlug apigen.PolicySlug) {
	g, u, err := s.authorizePolicy(r, orgSlug, policySlug, org.ActionPolicyUpdate)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.PolicyVersionCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if len(body.Document) == 0 {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "document is required"))
		return
	}
	v, err := s.policies.AddPolicyVersion(r.Context(), g, body.Document, u.GRN())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, policyVersionToAPI(v))
}

// ActivatePolicy makes a policy count in evaluation. Requires the
// admin role.
func (s *Server) ActivatePolicy(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, policySlug apigen.PolicySlug) {
	g, _, err := s.authorizePolicy(r, orgSlug, policySlug, org.ActionPolicyUpdate)
	if err != nil {
		writeError(w, err)
		return
	}
	p, err := s.policies.ActivatePolicy(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policyToAPI(p))
}

// ArchivePolicy retires a policy. Requires the admin role.
func (s *Server) ArchivePolicy(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, policySlug apigen.PolicySlug) {
	g, _, err := s.authorizePolicy(r, orgSlug, policySlug, org.ActionPolicyUpdate)
	if err != nil {
		writeError(w, err)
		return
	}
	p, err := s.policies.ArchivePolicy(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policyToAPI(p))
}

// RollbackPolicy points a policy at an earlier version. Requires the
// admin role.
func (s *Server) RollbackPolicy(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, policySlug apigen.PolicySlug) {
	g, _, err := s.authorizePolicy(r, orgSlug, policySlug, org.ActionPolicyUpdate)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.PolicyRollback
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	p, err := s.policies.RollbackPolicy(r.Context(), g, body.Version)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, policyToAPI(p))
}

// ListPolicyAttachments lists a policy's attachments. Requires the
// admin role.
func (s *Server) ListPolicyAttachments(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, policySlug apigen.PolicySlug) {
	g, _, err := s.authorizePolicy(r, orgSlug, policySlug, org.ActionPolicyRead)
	if err != nil {
		writeError(w, err)
		return
	}
	attachments, err := s.policies.ListPolicyAttachments(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.PolicyAttachment, 0, len(attachments))
	for _, a := range attachments {
		out = append(out, attachmentToAPI(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// AttachPolicy binds a policy to a subject. Requires the admin role.
func (s *Server) AttachPolicy(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, policySlug apigen.PolicySlug) {
	g, _, err := s.authorizePolicy(r, orgSlug, policySlug, org.ActionPolicyAttach)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.PolicyAttachmentCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	subject, err := grn.Parse(body.SubjectGrn)
	if err != nil {
		writeError(w, err)
		return
	}
	a, err := s.policies.AttachPolicy(r.Context(), g, subject)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, attachmentToAPI(a))
}

// DetachPolicy removes one of a policy's attachments. Requires the
// admin role.
func (s *Server) DetachPolicy(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, policySlug apigen.PolicySlug, subjectGrn apigen.SubjectGrn) {
	g, _, err := s.authorizePolicy(r, orgSlug, policySlug, org.ActionPolicyDetach)
	if err != nil {
		writeError(w, err)
		return
	}
	subject, err := grn.Parse(subjectGrn)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.policies.DetachPolicy(r.Context(), g, subject); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// authorizePolicy resolves the signed-in user and enforces action on
// the addressed policy, returning both the policy's GRN and the user
// the write is recorded against.
func (s *Server) authorizePolicy(r *http.Request, orgSlug, policySlug, action string) (grn.GRN, *user.User, error) {
	u, err := s.currentUser(r)
	if err != nil {
		return grn.GRN{}, nil, err
	}
	g := policyGRN(orgSlug, policySlug)
	if err := s.authorizeOn(r, orgGRN(orgSlug), g, u, action); err != nil {
		return grn.GRN{}, nil, err
	}
	return g, u, nil
}

// policyGRN builds the GRN of a policy owned by the org. The org
// segment scopes the identifier, so a policy of another org is not
// addressable through that org's paths.
func policyGRN(orgSlug, policySlug string) grn.GRN {
	return grn.New(orgSlug, grn.DefaultProject, serviceTypePolicy, policySlug)
}

func policyToAPI(p *Policy) apigen.Policy {
	return apigen.Policy{
		Grn:            p.GRN.String(),
		OrgGrn:         p.OrgGRN.String(),
		Name:           p.Name,
		Status:         apigen.PolicyStatus(p.Status),
		DefaultVersion: p.DefaultVersion,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}

func policyVersionToAPI(v *PolicyVersion) apigen.PolicyVersion {
	return apigen.PolicyVersion{
		PolicyGrn: v.PolicyGRN.String(),
		Version:   v.Version,
		Document:  v.Document,
		CreatedBy: v.CreatedBy.String(),
		CreatedAt: v.CreatedAt,
	}
}

func attachmentToAPI(a *PolicyAttachment) apigen.PolicyAttachment {
	return apigen.PolicyAttachment{
		PolicyGrn:  a.PolicyGRN.String(),
		SubjectGrn: a.SubjectGRN.String(),
		CreatedAt:  a.CreatedAt,
	}
}
