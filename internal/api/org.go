package api

import (
	"net/http"

	apigen "github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
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
	// Organizations are owned by the reserved root owner (ADR-0001).
	g := grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeOrg, id)
	o := org.NewOrganization(g, body.Name)
	if err := s.orgs.CreateOrg(r.Context(), o); err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.AddMember(r.Context(), org.NewMembership(g, u.GRN(), org.RoleOwner)); err != nil {
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
	if err := s.authorize(r, g, u, org.ActionOrgRead); err != nil {
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
	if err := s.authorize(r, g, u, org.ActionOrgUpdate); err != nil {
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
	if err := s.authorize(r, g, u, org.ActionOrgDelete); err != nil {
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
	if err := s.authorize(r, g, u, org.ActionOrgMemberList); err != nil {
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

// authorize enforces the action on the org itself.
func (s *Server) authorize(r *http.Request, orgGRN grn.GRN, u *user.User, action string) error {
	return s.authorizeOn(r, orgGRN, orgGRN, u, action)
}

// authorizeOn enforces the action on target, a resource of orgGRN.
// Membership in the org is the precondition; the statements collected
// for the subject then decide through the Evaluator seam, so a role
// held on the target group or project counts. Non-members and denied
// actions both yield 403.
func (s *Server) authorizeOn(r *http.Request, orgGRN, target grn.GRN, u *user.User, action string) error {
	m, err := s.orgs.GetMember(r.Context(), orgGRN, u.GRN())
	if errorx.IsKind(err, errorx.KindNotFound) {
		return errorx.New(errorx.KindPermissionDenied, "not a member of this org")
	}
	if err != nil {
		return err
	}
	statements, err := s.scopedStatements(r, orgGRN, m, target, u.GRN())
	if err != nil {
		return err
	}
	allowed, err := s.evaluator.Evaluate(r.Context(), EvalInput{
		Subject:    u.GRN(),
		Object:     target,
		Action:     action,
		Statements: statements,
	})
	if err != nil {
		return err
	}
	if !allowed {
		return errorx.New(errorx.KindPermissionDenied, "insufficient role")
	}
	return nil
}

// orgGRN builds the GRN for an org identifier from an API path.
func orgGRN(slug string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeOrg, slug)
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
