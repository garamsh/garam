package api

import (
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/garamsh/garam/lib/errorx"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/org"
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
	inv := org.NewInvitation(token, g, role, u.GRN, time.Now().UTC().Add(ttl))
	if err := s.orgs.CreateInvitation(r.Context(), inv); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apigen.Invitation{
		Token:     inv.Token,
		OrgGrn:    inv.Organization.String(),
		Role:      apigen.Role(inv.Role),
		ExpiresAt: inv.ExpiresAt,
	})
}

// ListInvitations lists the org's invitations that can still be
// accepted, by the id that admits no one and never by their token. It
// is authorized on org:invite, the action that issues them (ADR-0136).
func (s *Server) ListInvitations(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListInvitationsParams) {
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
	f, err := listfilter.Parse(params.Limit, params.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	invitations, err := s.orgs.ListPendingInvitations(r.Context(), g, f)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.PendingInvitation, 0, len(invitations))
	for _, inv := range invitations {
		out = append(out, apigen.PendingInvitation{
			Id:           openapi_types.UUID(inv.ID),
			Role:         apigen.Role(inv.Role),
			CreatedByGrn: inv.CreatedBy.String(),
			CreatedAt:    inv.CreatedAt,
			ExpiresAt:    inv.ExpiresAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeInvitation withdraws an unspent invitation of the org, so its
// token answers as one that never existed (ADR-0136).
func (s *Server) RevokeInvitation(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, invitationID openapi_types.UUID) {
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
	if err := s.orgs.RevokeInvitation(r.Context(), g, org.InvitationID(invitationID)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetInvitation answers what an invitation would admit its holder to,
// so a page can name the organization and the role before anyone
// accepts. It takes no session: holding the token is the whole claim
// (ADR-0006), and the answer reveals nothing accepting would not.
//
// An invitation whose organization is gone admits to nothing, so it is
// answered as an unknown token rather than as a missing organization:
// a second not-found sentence would tell the two apart.
func (s *Server) GetInvitation(w http.ResponseWriter, r *http.Request, token string) {
	inv, err := s.orgs.GetInvitation(r.Context(), token)
	if err != nil {
		writeError(w, err)
		return
	}
	o, err := s.orgs.GetOrg(r.Context(), inv.Organization)
	if errorx.IsKind(err, errorx.KindNotFound) {
		writeError(w, org.ErrInvitationNotFound)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	state := apigen.InvitationPreviewStatePending
	switch {
	case inv.Accepted():
		state = apigen.InvitationPreviewStateAccepted
	case inv.Expired(time.Now().UTC()):
		state = apigen.InvitationPreviewStateExpired
	}
	writeJSON(w, http.StatusOK, apigen.InvitationPreview{
		Org:       inv.Organization.Identifier,
		OrgName:   o.Name,
		Role:      apigen.Role(inv.Role),
		State:     state,
		ExpiresAt: inv.ExpiresAt,
	})
}

// AcceptInvitation joins the caller into the invitation's org with the
// invitation's role. The store spends the invitation and adds the
// membership together, so of two concurrent accepts one joins and the
// other is refused (#1381).
func (s *Server) AcceptInvitation(w http.ResponseWriter, r *http.Request, token string) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	m, err := s.orgs.AcceptInvitation(r.Context(), token, u.GRN)
	if err != nil {
		writeError(w, err)
		return
	}
	nickname := u.Nickname
	writeJSON(w, http.StatusOK, memberToAPI(m, &nickname))
}
