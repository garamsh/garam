package api

import (
	"net/http"

	apigen "github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/org"
)

// CreateOrg creates an organization and makes the caller its owner.
func (s *Server) CreateOrg(w http.ResponseWriter, r *http.Request) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.OrganizationCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Name == "" {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "name is required"))
		return
	}
	id, err := randomHex(8)
	if err != nil {
		writeError(w, err)
		return
	}
	g := grn.New("garam", "default", org.ServiceTypeOrg, id)
	o := org.NewOrganization(g, body.Name)
	if err := s.orgs.CreateOrg(r.Context(), o); err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.AddMember(r.Context(), org.NewMembership(g, u.GRN(), org.RoleOwner)); err != nil {
		writeError(w, err)
		return
	}
	if err := s.users.AddOrganization(r.Context(), u.GRN(), g); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, orgToAPI(o))
}

// ListOrgs lists the caller's organizations.
func (s *Server) ListOrgs(w http.ResponseWriter, r *http.Request, params apigen.ListOrgsParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var f org.ListFilter
	if params.Limit != nil {
		f.Limit = *params.Limit
	}
	if params.Offset != nil {
		f.Offset = *params.Offset
	}
	orgs, err := s.orgs.ListOrgsByMember(r.Context(), u.GRN(), f)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Organization, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, orgToAPI(o))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetOrg returns an organization the caller belongs to.
func (s *Server) GetOrg(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := orgGRN(orgSlug)
	if err := s.requireRole(r, g, u.GRN(), org.RoleMember); err != nil {
		writeError(w, err)
		return
	}
	o, err := s.orgs.GetOrg(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orgToAPI(o))
}

// UpdateOrg renames an organization. Requires the admin role.
func (s *Server) UpdateOrg(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := orgGRN(orgSlug)
	if err := s.requireRole(r, g, u.GRN(), org.RoleAdmin); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.OrganizationPatch
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Name != nil && *body.Name == "" {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "name must not be empty"))
		return
	}
	o, err := s.orgs.UpdateOrg(r.Context(), g, org.Patch{Name: body.Name})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, orgToAPI(o))
}

// DeleteOrg removes an organization. Requires the owner role.
func (s *Server) DeleteOrg(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := orgGRN(orgSlug)
	if err := s.requireRole(r, g, u.GRN(), org.RoleOwner); err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.DeleteOrg(r.Context(), g); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListOrgMembers lists an organization's members. Requires the member
// role.
func (s *Server) ListOrgMembers(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	g := orgGRN(orgSlug)
	if err := s.requireRole(r, g, u.GRN(), org.RoleMember); err != nil {
		writeError(w, err)
		return
	}
	members, err := s.orgs.ListMembers(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Member, 0, len(members))
	for _, m := range members {
		out = append(out, memberToAPI(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// requireRole enforces that userGRN holds at least the required role
// in orgGRN. Non-members and insufficient roles both yield 403.
func (s *Server) requireRole(r *http.Request, orgGRN, userGRN grn.GRN, required org.Role) error {
	m, err := s.orgs.GetMember(r.Context(), orgGRN, userGRN)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return errorx.New(errorx.KindPermissionDenied, "not a member of this org")
	}
	if err != nil {
		return err
	}
	if !m.Role().Allows(required) {
		return errorx.New(errorx.KindPermissionDenied, "insufficient role")
	}
	return nil
}

// orgGRN builds the GRN for an org identifier from an API path.
func orgGRN(slug string) grn.GRN {
	return grn.New("garam", "default", org.ServiceTypeOrg, slug)
}

func orgToAPI(o *org.Organization) apigen.Organization {
	return apigen.Organization{
		Grn:       o.GRN().String(),
		Name:      o.Name(),
		CreatedAt: o.CreatedAt(),
	}
}

func memberToAPI(m *org.Membership) apigen.Member {
	return apigen.Member{
		UserGrn: m.User().String(),
		Role:    apigen.Role(m.Role()),
		Since:   m.CreatedAt(),
	}
}
