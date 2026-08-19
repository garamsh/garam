package api

import (
	"context"
	"net/http"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
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
	var f listfilter.Filter
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
	nicknames, err := s.nicknamesOf(r.Context(), members)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Member, 0, len(members))
	for _, m := range members {
		out = append(out, memberToAPI(m, nicknames[m.User().String()]))
	}
	writeJSON(w, http.StatusOK, out)
}

// nicknamesOf resolves the members' display names in one query, keyed
// by user GRN. A user deleted between the two reads is absent from the
// map: the reads are not atomic, and the delete cascades the
// membership away by the next request.
func (s *Server) nicknamesOf(ctx context.Context, members []*org.Membership) (map[string]*string, error) {
	grns := make([]grn.GRN, 0, len(members))
	for _, m := range members {
		grns = append(grns, m.User())
	}
	users, err := s.users.ListByGRNs(ctx, grns)
	if err != nil {
		return nil, err
	}
	byGRN := make(map[string]*string, len(users))
	for _, u := range users {
		nickname := u.Nickname()
		byGRN[u.GRN().String()] = &nickname
	}
	return byGRN, nil
}

// authorize enforces the action on the org itself.
func (s *Server) authorize(r *http.Request, orgGRN grn.GRN, u *user.User, action string) error {
	return s.authorizeOn(r, orgGRN, orgGRN, u, action)
}

// authorizeOn enforces the action on target, a resource of orgGRN.
// Org membership is the precondition; non-members and denied actions
// both yield 403. Collection: authz.md §Statement collection.
//
// Both GRNs carry a path segment the caller chose, and this is where
// one stops being inert: scopedStatements splices the org identifier
// into a resource pattern, where a wildcard would widen the statement
// past the org it was scoped to. Validating here refuses that before
// the pattern is built, rather than resting on the membership lookup
// below happening to miss.
func (s *Server) authorizeOn(r *http.Request, orgGRN, target grn.GRN, u *user.User, action string) error {
	if err := orgGRN.Validate(); err != nil {
		return err
	}
	if err := target.Validate(); err != nil {
		return err
	}
	m, err := s.orgs.GetMember(r.Context(), orgGRN, u.GRN())
	if errorx.IsKind(err, errorx.KindNotFound) {
		return errorx.New(errorx.KindPermissionDenied, "not a member of this org")
	}
	if err != nil {
		return err
	}
	statements, err := s.scopedStatements(r, orgGRN, m, target, u)
	if err != nil {
		return err
	}
	groups, err := s.orgs.ListGroupsByMember(r.Context(), orgGRN, u.GRN())
	if err != nil {
		return err
	}
	allowed, err := s.evaluator.Evaluate(r.Context(), EvalInput{
		Subject:       u.GRN(),
		Object:        target,
		Action:        action,
		SubjectGroups: groups,
		Statements:    statements,
		SubjectOrg:    orgGRN,
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

// memberToAPI renders a Membership. nickname is nil when the member's
// user row could not be resolved.
func memberToAPI(m *org.Membership, nickname *string) apigen.Member {
	return apigen.Member{
		UserGrn:  m.User().String(),
		Nickname: nickname,
		Role:     apigen.Role(m.Role()),
		Since:    m.CreatedAt(),
	}
}
