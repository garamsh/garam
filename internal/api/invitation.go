package api

import (
	"net/http"
	"time"

	apigen "github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/org"
)

// defaultInvitationTTL applies when the request omits expiresInHours.
const defaultInvitationTTL = 7 * 24 * time.Hour

// CreateInvitation issues a single-use invitation token. Requires the
// admin role in the org.
func (s *Server) CreateInvitation(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := orgGRN(orgSlug)
	if err := s.authorize(r, g, u, org.ActionOrgInvite); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.InvitationCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	role, err := org.ParseRole(string(body.Role))
	if err != nil {
		writeError(w, err)
		return
	}
	if role == org.RoleOwner {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "owner is not assignable by invitation"))
		return
	}
	ttl := defaultInvitationTTL
	if body.ExpiresInHours != nil {
		if *body.ExpiresInHours < 1 || *body.ExpiresInHours > 720 {
			writeError(w, errorx.New(errorx.KindInvalidArgument, "expiresInHours out of range"))
			return
		}
		ttl = time.Duration(*body.ExpiresInHours) * time.Hour
	}
	token, err := org.NewInvitationToken()
	if err != nil {
		writeError(w, err)
		return
	}
	inv := org.NewInvitation(token, g, role, u.GRN(), time.Now().UTC().Add(ttl))
	if err := s.orgs.CreateInvitation(r.Context(), inv); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apigen.Invitation{
		Token:     inv.Token(),
		OrgGrn:    inv.Organization().String(),
		Role:      apigen.Role(inv.Role()),
		ExpiresAt: inv.ExpiresAt(),
	})
}

// AcceptInvitation joins the caller into the invitation's org with the
// invitation's role.
func (s *Server) AcceptInvitation(w http.ResponseWriter, r *http.Request, token string) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	inv, err := s.orgs.GetInvitation(r.Context(), token)
	if err != nil {
		writeError(w, err)
		return
	}
	if inv.Accepted() {
		writeError(w, org.ErrInvitationAccepted)
		return
	}
	if inv.Expired(time.Now().UTC()) {
		writeError(w, org.ErrInvitationExpired)
		return
	}
	// Membership first: a duplicate membership rejects the accept
	// without burning the invitation.
	m := org.NewMembership(inv.Organization(), u.GRN(), inv.Role())
	if err := s.orgs.AddMember(r.Context(), m); err != nil {
		writeError(w, err)
		return
	}
	if _, err := s.orgs.AcceptInvitation(r.Context(), token, u.GRN()); err != nil {
		writeError(w, err)
		return
	}
	if err := s.users.AddOrganization(r.Context(), u.GRN(), inv.Organization()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, memberToAPI(m))
}
