package api

import (
	"net/http"
	"slices"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/org"
)

// CreateGroup creates a group in the org. Requires the admin role.
func (s *Server) CreateGroup(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorize(r, orgGRN(orgSlug), u, org.ActionGroupCreate); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.GroupCreate
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Name == "" {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "name is required"))
		return
	}
	roles, err := scopedRoles(body.Roles)
	if err != nil {
		writeError(w, err)
		return
	}
	id, err := randomHex(8)
	if err != nil {
		writeError(w, err)
		return
	}
	gr := org.NewGroup(groupGRN(orgSlug, id), body.Name, orgGRN(orgSlug), roles...)
	if err := s.orgs.CreateGroup(r.Context(), gr); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, groupToAPI(gr))
}

// ListGroups lists the org's groups, or with member=me those the
// caller belongs to, leaving out each group whose own read the caller
// is denied. Requires the member role.
func (s *Server) ListGroups(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, params apigen.ListGroupsParams) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	c, err := s.authorizedCaller(r, orgGRN(orgSlug), orgGRN(orgSlug), u, org.ActionGroupRead)
	if err != nil {
		writeError(w, err)
		return
	}
	f, err := listfilter.Parse(params.Limit, params.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	var groups []*org.Group
	switch {
	case params.Member == nil:
		groups, err = s.orgs.ListGroups(r.Context(), orgGRN(orgSlug), f)
	case *params.Member == apigen.GroupMemberMe:
		groups, err = s.orgs.ListMemberGroups(r.Context(), orgGRN(orgSlug), u.GRN, f)
	default:
		err = errorx.New(errorx.KindInvalidArgument, "member is me or absent")
	}
	if err != nil {
		writeError(w, err)
		return
	}
	groups, err = visible(s, r, c, org.ActionGroupRead, groups, func(gr *org.Group) BatchObject {
		return BatchObject{Object: gr.GRN, Statements: groupRoleStatements(gr, slices.Contains(c.groups, gr.GRN))}
	})
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Group, 0, len(groups))
	for _, gr := range groups {
		out = append(out, groupToAPI(gr))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetGroup returns a group of the org. Requires the member role.
func (s *Server) GetGroup(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, groupSlug apigen.GroupSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), groupGRN(orgSlug, groupSlug), u, org.ActionGroupRead); err != nil {
		writeError(w, err)
		return
	}
	gr, err := s.orgs.GetGroup(r.Context(), groupGRN(orgSlug, groupSlug))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groupToAPI(gr))
}

// UpdateGroup renames a group. Requires the admin role.
func (s *Server) UpdateGroup(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, groupSlug apigen.GroupSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), groupGRN(orgSlug, groupSlug), u, org.ActionGroupUpdate); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.GroupPatch
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if body.Name != nil && *body.Name == "" {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "name must not be empty"))
		return
	}
	roles, err := scopedRoles(body.Roles)
	if err != nil {
		writeError(w, err)
		return
	}
	gr, err := s.orgs.UpdateGroup(r.Context(), groupGRN(orgSlug, groupSlug), org.GroupPatch{Name: body.Name, Roles: roles})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, groupToAPI(gr))
}

// DeleteGroup removes a group. Requires the admin role.
func (s *Server) DeleteGroup(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, groupSlug apigen.GroupSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), groupGRN(orgSlug, groupSlug), u, org.ActionGroupDelete); err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.DeleteGroup(r.Context(), groupGRN(orgSlug, groupSlug)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListGroupMembers lists a group's members. Requires the member role.
func (s *Server) ListGroupMembers(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, groupSlug apigen.GroupSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), groupGRN(orgSlug, groupSlug), u, org.ActionGroupMemberList); err != nil {
		writeError(w, err)
		return
	}
	g := groupGRN(orgSlug, groupSlug)
	// A missing group must 404 rather than list as empty.
	if _, err := s.orgs.GetGroup(r.Context(), g); err != nil {
		writeError(w, err)
		return
	}
	members, err := s.orgs.ListGroupMembers(r.Context(), g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, memberRefs(members))
}

// AddGroupMember adds a member to a group. Requires the admin role.
func (s *Server) AddGroupMember(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, groupSlug apigen.GroupSlug) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), groupGRN(orgSlug, groupSlug), u, org.ActionGroupMemberAdd); err != nil {
		writeError(w, err)
		return
	}
	var body apigen.MemberRef
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	member, err := grn.Parse(body.MemberGrn)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.AddGroupMember(r.Context(), groupGRN(orgSlug, groupSlug), member); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RemoveGroupMember removes a member from a group. Requires the admin
// role.
func (s *Server) RemoveGroupMember(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org, groupSlug apigen.GroupSlug, memberGRN apigen.MemberGrn) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.authorizeOn(r, orgGRN(orgSlug), groupGRN(orgSlug, groupSlug), u, org.ActionGroupMemberRemove); err != nil {
		writeError(w, err)
		return
	}
	member, err := grn.Parse(memberGRN)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.RemoveGroupMember(r.Context(), groupGRN(orgSlug, groupSlug), member); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// groupGRN builds the GRN of a group owned by the org. The org
// segment scopes the identifier, so a group of another org is not
// addressable through that org's paths.
func groupGRN(orgSlug, groupSlug string) grn.GRN {
	return grn.New(orgSlug, grn.DefaultProject, org.ServiceTypeGroup, groupSlug)
}

func groupToAPI(g *org.Group) apigen.Group {
	return apigen.Group{
		Grn:    g.GRN.String(),
		Name:   g.Name,
		OrgGrn: g.Organization.String(),
		Roles:  rolesToAPI(g.Roles),
	}
}

// scopedRoles validates the roles a group or project grants its own
// members and renders them for the domain. A nil field leaves the
// roles unchanged; an empty array clears them.
func scopedRoles(roles *apigen.ScopedRoles) ([]string, error) {
	if roles == nil {
		return nil, nil
	}
	out := make([]string, 0, len(*roles))
	for _, r := range *roles {
		role, err := org.ParseRole(string(r))
		if err != nil {
			return nil, err
		}
		out = append(out, string(role))
	}
	return out, nil
}

// rolesToAPI renders group or project roles for the wire.
func rolesToAPI(roles []string) apigen.ScopedRoles {
	out := make(apigen.ScopedRoles, 0, len(roles))
	for _, r := range roles {
		out = append(out, apigen.Role(r))
	}
	return out
}

// memberRefs renders group or project member GRNs for the wire.
func memberRefs(members []grn.GRN) []apigen.MemberRef {
	out := make([]apigen.MemberRef, 0, len(members))
	for _, m := range members {
		out = append(out, apigen.MemberRef{MemberGrn: m.String()})
	}
	return out
}
