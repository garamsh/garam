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
	g := grn.Org(id)
	o := org.NewOrganization(g, body.Name)
	if err := s.orgs.CreateOrg(r.Context(), o); err != nil {
		writeError(w, err)
		return
	}
	if err := s.orgs.AddMember(r.Context(), org.NewMembership(g, u.GRN, org.RoleOwner)); err != nil {
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
	f, err := listfilter.Parse(params.Limit, params.Offset)
	if err != nil {
		writeError(w, err)
		return
	}
	orgs, err := s.orgs.ListOrgsByMember(r.Context(), u.GRN, f)
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

// GetOrgDeletionPreview answers what deleting the organization would
// destroy. Requires the owner role, which is the role DeleteOrg
// requires: the preview is authorized as the act it describes, so it
// tells nobody the size of an organization they could not delete.
//
// It reads and changes nothing. Each domain counts what it owns, and
// this handler composes the answers because the cascade crosses
// boundaries no one of them may reach across (ADR-0047). The counts
// are separate reads, so an organization written to while they run can
// answer from either side of that write; what the answer must not be
// is the length of a page, which is what a caller assembling this from
// its own lists would get.
func (s *Server) GetOrgDeletionPreview(w http.ResponseWriter, r *http.Request, orgSlug apigen.Org) {
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
	// Membership is authorize's precondition and a membership row goes
	// with the organization, so reaching here is already proof the
	// organization existed: an organization that does not exist has no
	// members and is refused above, which is the same answer a
	// non-member gets (ADR-0011). Nothing below can answer a page of
	// zeros for an organization that is not there.
	ctx := r.Context()
	contents, err := s.orgs.CountOrgContents(ctx, g)
	if err != nil {
		writeError(w, err)
		return
	}
	agents, err := s.agents.CountByOrg(ctx, g)
	if err != nil {
		writeError(w, err)
		return
	}
	operators, err := s.operators.CountByOrg(ctx, g)
	if err != nil {
		writeError(w, err)
		return
	}
	consents, err := s.consents.CountByOrg(ctx, g)
	if err != nil {
		writeError(w, err)
		return
	}
	// The engine owns policies and returns the organization's whole
	// list rather than a page, so its length is the count.
	policies, err := s.policies.ListPolicies(ctx, g)
	if err != nil {
		writeError(w, err)
		return
	}
	authority, err := s.authorities.HasAuthority(ctx, g)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apigen.OrgDeletionPreview{
		CertificateAuthority: authority,
		Counts: apigen.OrgDeletionCounts{
			Groups:           contents.Groups,
			Projects:         contents.Projects,
			Members:          contents.Members,
			Invitations:      contents.Invitations,
			Policies:         len(policies),
			Agents:           agents.Agents,
			AgentDefinitions: agents.Definitions,
			AgentTemplates:   agents.Templates,
			Operators:        operators,
			Consents:         consents,
		},
	})
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
		out = append(out, memberToAPI(m, nicknames[m.User.String()]))
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
		grns = append(grns, m.User)
	}
	users, err := s.users.ListByGRNs(ctx, grns)
	if err != nil {
		return nil, err
	}
	byGRN := make(map[string]*string, len(users))
	for _, u := range users {
		nickname := u.Nickname
		byGRN[u.GRN.String()] = &nickname
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
// Both GRNs are validated first because scopedStatements splices the
// org identifier into a resource pattern, where a wildcard would widen
// the statement past the org it was scoped to.
func (s *Server) authorizeOn(r *http.Request, orgGRN, target grn.GRN, u *user.User, action string) error {
	if err := target.Validate(); err != nil {
		return err
	}
	c, err := s.callerIn(r, orgGRN, u)
	if err != nil {
		return err
	}
	allowed, err := s.allows(r, c, target, action)
	if err != nil {
		return err
	}
	if !allowed {
		return errorx.New(errorx.KindPermissionDenied, "insufficient role")
	}
	return nil
}

// caller is what every check of one user inside one org shares: the
// membership every check requires, and the groups the engine finds
// attached policies through (ADR-0008).
type caller struct {
	org    grn.GRN
	user   *user.User
	member *org.Membership
	groups []grn.GRN
}

// callerIn resolves u inside orgGRN. A non-member is refused, since
// membership is the precondition of every check.
func (s *Server) callerIn(r *http.Request, orgGRN grn.GRN, u *user.User) (*caller, error) {
	if err := orgGRN.Validate(); err != nil {
		return nil, err
	}
	m, err := s.orgs.GetMember(r.Context(), orgGRN, u.GRN)
	if errorx.IsKind(err, errorx.KindNotFound) {
		return nil, errorx.New(errorx.KindPermissionDenied, "not a member of this org")
	}
	if err != nil {
		return nil, err
	}
	groups, err := s.orgs.ListGroupsByMember(r.Context(), orgGRN, u.GRN)
	if err != nil {
		return nil, err
	}
	return &caller{org: orgGRN, user: u, member: m, groups: groups}, nil
}

// allows asks the engine whether c may take action on target, a
// validated resource of c's org.
func (s *Server) allows(r *http.Request, c *caller, target grn.GRN, action string) (bool, error) {
	statements, err := s.scopedStatements(r, c.org, c.member, target, c.user)
	if err != nil {
		return false, err
	}
	return s.evaluator.Evaluate(r.Context(), EvalInput{
		Subject:       c.user.GRN,
		Object:        target,
		Action:        action,
		SubjectGroups: c.groups,
		Statements:    statements,
		SubjectOrg:    c.org,
	})
}

// orgGRN builds the GRN for an org identifier from an API path.
func orgGRN(slug string) grn.GRN {
	return grn.Org(slug)
}

func orgToAPI(o *org.Organization) apigen.Organization {
	return apigen.Organization{
		Grn:       o.GRN.String(),
		Name:      o.Name,
		CreatedAt: o.CreatedAt,
	}
}

// memberToAPI renders a Membership. nickname is nil when the member's
// user row could not be resolved.
func memberToAPI(m *org.Membership, nickname *string) apigen.Member {
	return apigen.Member{
		UserGrn:  m.User.String(),
		Nickname: nickname,
		Role:     apigen.Role(m.Role),
		Since:    m.CreatedAt,
	}
}
