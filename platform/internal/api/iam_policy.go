package api

import (
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policyrelay"
)

// WithPolicyRelay sets what relays an organization's role links,
// guardrails and permits to the policy service (ADR-0169). Without it
// the endpoints answer unavailable: the platform stores none of a body
// and has nowhere else to send it.
func (s *Server) WithPolicyRelay(relay policyrelay.Service) *Server {
	s.policyRelay = relay
	return s
}

// relayed authenticates the caller and names the policy a request
// concerns. Whether the caller may is the policy service's to decide,
// read current, and it answers in the kind the response carries
// (ADR-0146 §Security): the platform asks no question of its own, so
// there is nothing here to disagree with it.
func (s *Server) relayed(
	w http.ResponseWriter, r *http.Request, orgSlug, id string,
) (policyrelay.Write, bool) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return policyrelay.Write{}, false
	}
	if s.policyRelay == nil {
		writeError(w, errorx.New(errorx.KindUnavailable, "policy service is not configured"))
		return policyrelay.Write{}, false
	}
	return policyrelay.Write{Writer: u.GRN, Org: orgGRN(orgSlug), ID: id}, true
}

// CreateIamPolicy relays the first version of a role link, guardrail
// or permit.
func (s *Server) CreateIamPolicy(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	pw, ok := s.relayed(w, r, orgSlug, "")
	if !ok {
		return
	}
	var req apigen.IamPolicyCreate
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	body, err := relayBody(req.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	if pw.ID, err = policyrelay.NewID(); err != nil {
		writeError(w, err)
		return
	}
	if err := s.policyRelay.Create(r.Context(), pw, req.Name, body); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apigen.IamPolicy{
		Id: pw.ID, Name: req.Name, Kind: apigen.IamPolicyKind(body.Kind),
		Status: apigen.PolicyStatus(policyrelay.StatusDraft), DefaultVersion: 1,
	})
}

// ListIamPolicies reads the organization's role links, guardrails and
// permits.
func (s *Server) ListIamPolicies(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	pw, ok := s.relayed(w, r, orgSlug, "")
	if !ok {
		return
	}
	found, err := s.policyRelay.List(r.Context(), pw.Writer, pw.Org)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.IamPolicy, len(found))
	for i, p := range found {
		out[i] = iamPolicyToAPI(p)
	}
	writeJSON(w, http.StatusOK, out)
}

// GetIamPolicy reads one policy with its versions.
func (s *Server) GetIamPolicy(
	w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, id apigen.IamPolicyId,
) {
	pw, ok := s.relayed(w, r, orgSlug, id)
	if !ok {
		return
	}
	detail, err := s.policyRelay.Get(r.Context(), pw)
	if err != nil {
		writeError(w, err)
		return
	}
	out := apigen.IamPolicyDetail{
		Policy:   iamPolicyToAPI(detail.Policy),
		Versions: make([]apigen.IamPolicyVersion, len(detail.Versions)),
	}
	for i, v := range detail.Versions {
		out.Versions[i] = apigen.IamPolicyVersion{
			Version: v.Number,
			Body:    iamBodyToAPI(v.Body), CreatedBy: v.CreatedBy,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateIamPolicyVersion relays a further version of a policy.
func (s *Server) CreateIamPolicyVersion(
	w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, id apigen.IamPolicyId,
) {
	pw, ok := s.relayed(w, r, orgSlug, id)
	if !ok {
		return
	}
	var req apigen.IamPolicyVersionCreate
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	body, err := relayBody(req.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	version, err := s.policyRelay.AppendVersion(r.Context(), pw, body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apigen.IamPolicyVersionNumber{Version: version})
}

// ActivateIamPolicy relays an activation.
func (s *Server) ActivateIamPolicy(
	w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, id apigen.IamPolicyId,
) {
	pw, ok := s.relayed(w, r, orgSlug, id)
	if !ok {
		return
	}
	if err := s.policyRelay.Activate(r.Context(), pw); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RollbackIamPolicy relays a roll-back to a stored version.
func (s *Server) RollbackIamPolicy(
	w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, id apigen.IamPolicyId,
) {
	pw, ok := s.relayed(w, r, orgSlug, id)
	if !ok {
		return
	}
	var req apigen.PolicyRollback
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, err)
		return
	}
	if err := s.policyRelay.RollBack(r.Context(), pw, req.Version); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteIamPolicy relays a removal.
func (s *Server) DeleteIamPolicy(
	w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, id apigen.IamPolicyId,
) {
	pw, ok := s.relayed(w, r, orgSlug, id)
	if !ok {
		return
	}
	if err := s.policyRelay.Delete(r.Context(), pw); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// relayBody reads a request's body and refuses one the platform never
// relays (policyrelay.Body.Validate).
func relayBody(in apigen.IamPolicyBody) (policyrelay.Body, error) {
	body := policyrelay.Body{Kind: policyrelay.Kind(in.Kind)}
	if in.Text != nil {
		body.Text = *in.Text
	}
	if l := in.RoleLink; l != nil {
		principal, err := grn.Parse(l.Principal)
		if err != nil {
			return policyrelay.Body{}, errorx.Wrap(err, "role link principal is not a GRN", errorx.KindInvalidArgument)
		}
		resource, err := grn.Parse(l.Resource)
		if err != nil {
			return policyrelay.Body{}, errorx.Wrap(err, "role link resource is not a GRN", errorx.KindInvalidArgument)
		}
		body.Link = &policyrelay.RoleLink{
			Role: org.Role(l.Role), Scope: policyrelay.Scope(l.Scope),
			Principal: principal, Resource: resource,
		}
	}
	if err := body.Validate(); err != nil {
		return policyrelay.Body{}, err
	}
	return body, nil
}

func iamBodyToAPI(b policyrelay.Body) apigen.IamPolicyBody {
	out := apigen.IamPolicyBody{Kind: apigen.IamPolicyKind(b.Kind)}
	if b.Link != nil {
		out.RoleLink = &apigen.IamRoleLink{
			Role: apigen.Role(b.Link.Role), Scope: apigen.IamRoleLinkScope(b.Link.Scope),
			Principal: b.Link.Principal.String(), Resource: b.Link.Resource.String(),
		}
		return out
	}
	out.Text = &b.Text
	return out
}

func iamPolicyToAPI(p policyrelay.Policy) apigen.IamPolicy {
	return apigen.IamPolicy{
		Id: p.ID, Name: p.Name, Kind: apigen.IamPolicyKind(p.Kind),
		Status: apigen.PolicyStatus(p.Status), DefaultVersion: p.DefaultVersion,
	}
}
