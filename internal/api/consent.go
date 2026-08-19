package api

import (
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/org"
)

// ListOrgConsents lists every cross-organization consent relation the
// org is a side of. Requires the admin role.
func (s *Server) ListOrgConsents(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := orgGRN(orgSlug)
	if err := s.authorize(r, g, u, org.ActionConsentRead); err != nil {
		writeError(w, err)
		return
	}
	consents, err := s.consents.List(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.OrgConsent, 0, len(consents))
	for _, c := range consents {
		out = append(out, consentToAPI(g, c))
	}
	writeJSON(w, http.StatusOK, out)
}

// GrantOrgConsent stands the org's own half of the pair. Requires the
// admin role.
func (s *Server) GrantOrgConsent(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, otherSlug apigen.OtherSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := orgGRN(orgSlug)
	if err := s.authorize(r, g, u, org.ActionConsentGrant); err != nil {
		writeError(w, err)
		return
	}
	other, err := s.consentPartner(r, g, otherSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	consent, err := s.consents.Grant(r.Context(), g, other)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, consentToAPI(g, consent))
}

// WithdrawOrgConsent clears the org's own half of the pair. Requires
// the admin role.
func (s *Server) WithdrawOrgConsent(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, otherSlug apigen.OtherSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := orgGRN(orgSlug)
	if err := s.authorize(r, g, u, org.ActionConsentWithdraw); err != nil {
		writeError(w, err)
		return
	}
	other, err := s.consentPartner(r, g, otherSlug)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.consents.Withdraw(r.Context(), g, other); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// consentPartner resolves the other organization of the pair. An org
// consents with another org, never with itself, and granting toward an
// org that does not exist is a 404 rather than the foreign key's 500.
func (s *Server) consentPartner(r *http.Request, g grn.GRN, otherSlug apigen.OtherSlug) (grn.GRN, error) {
	other := orgGRN(otherSlug)
	if other == g {
		return grn.GRN{}, errorx.New(errorx.KindInvalidArgument, "an organization consents with another, not with itself")
	}
	if _, err := s.orgs.GetOrg(r.Context(), other); err != nil {
		return grn.GRN{}, err
	}
	return other, nil
}

// consentToAPI renders the relation from the side the path named: the
// other organization's GRN and which halves stand.
func consentToAPI(g grn.GRN, c *message.Consent) apigen.OrgConsent {
	if c.OrgA() == g {
		return apigen.OrgConsent{
			OrgGrn:       c.OrgB().String(),
			GrantedHere:  c.AConsentedAt() != nil,
			GrantedThere: c.BConsentedAt() != nil,
			Stands:       c.Stands(),
		}
	}
	return apigen.OrgConsent{
		OrgGrn:       c.OrgA().String(),
		GrantedHere:  c.BConsentedAt() != nil,
		GrantedThere: c.AConsentedAt() != nil,
		Stands:       c.Stands(),
	}
}
