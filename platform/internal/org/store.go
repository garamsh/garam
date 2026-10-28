package org

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
)

// Patch holds Organization partial-update fields. Nil entries are
// unchanged.
type Patch struct {
	Name *string
}

// GroupPatch holds Group partial-update fields. Nil entries are
// unchanged.
type GroupPatch struct {
	Name     *string
	Roles    []string
	Policies []string
}

// ProjectPatch holds Project partial-update fields. Nil entries are
// unchanged.
type ProjectPatch struct {
	Name     *string
	Roles    []string
	Policies []string
}

// Store persists Organizations, Groups, and Projects. Cascade-delete
// semantics live here: removing an Organization removes its Groups
// and Projects in the same call.
type Store interface {
	// Organization
	CreateOrg(ctx context.Context, o *Organization) error
	GetOrg(ctx context.Context, g grn.GRN) (*Organization, error)
	ListOrgs(ctx context.Context, f listfilter.Filter) ([]*Organization, error)
	UpdateOrg(ctx context.Context, g grn.GRN, p Patch) (*Organization, error)
	DeleteOrg(ctx context.Context, g grn.GRN) error
	// CountOrgContents counts what the Organization holds of what
	// DeleteOrg removes with it. An Organization that does not exist
	// holds nothing, so the zero Contents is its answer and not an
	// error: the caller asking what a delete would take is told it
	// would take nothing.
	CountOrgContents(ctx context.Context, g grn.GRN) (Contents, error)

	// Group
	CreateGroup(ctx context.Context, g *Group) error
	GetGroup(ctx context.Context, g grn.GRN) (*Group, error)
	ListGroups(ctx context.Context, orgGRN grn.GRN, f listfilter.Filter) ([]*Group, error)
	// SearchGroups answers every group of the org whose name or
	// identifier holds n, case aside, ordered by GRN.
	SearchGroups(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*Group, error)
	UpdateGroup(ctx context.Context, g grn.GRN, p GroupPatch) (*Group, error)
	DeleteGroup(ctx context.Context, g grn.GRN) error
	AddGroupMember(ctx context.Context, groupGRN, memberGRN grn.GRN) error
	// RemoveGroupMember returns ErrGroupMemberNotFound when the pair
	// names no membership, which a Group that does not exist does too.
	RemoveGroupMember(ctx context.Context, groupGRN, memberGRN grn.GRN) error
	ListGroupMembers(ctx context.Context, groupGRN grn.GRN) ([]grn.GRN, error)
	// ListGroupsByMember returns the org's Groups the member belongs
	// to; a check reads it to resolve group-attached policies.
	ListGroupsByMember(ctx context.Context, orgGRN, memberGRN grn.GRN) ([]grn.GRN, error)
	// ListMemberGroups pages the org's Groups the member belongs to,
	// ordered by GRN as ListGroups orders them.
	ListMemberGroups(ctx context.Context, orgGRN, memberGRN grn.GRN, f listfilter.Filter) ([]*Group, error)

	// Project
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, g grn.GRN) (*Project, error)
	ListProjects(ctx context.Context, orgGRN grn.GRN, f listfilter.Filter) ([]*Project, error)
	// SearchProjects answers the org's projects as SearchGroups answers
	// its groups.
	SearchProjects(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*Project, error)
	UpdateProject(ctx context.Context, g grn.GRN, p ProjectPatch) (*Project, error)
	DeleteProject(ctx context.Context, g grn.GRN) error
	AddProjectMember(ctx context.Context, projectGRN, memberGRN grn.GRN) error
	// RemoveProjectMember returns ErrProjectMemberNotFound when the
	// pair names no membership, which a Project that does not exist
	// does too.
	RemoveProjectMember(ctx context.Context, projectGRN, memberGRN grn.GRN) error
	ListProjectMembers(ctx context.Context, projectGRN grn.GRN) ([]grn.GRN, error)
	// ListProjectsByMember returns the org's Projects the member
	// belongs to; a list reads it to collect what each project's roles
	// grant the member.
	ListProjectsByMember(ctx context.Context, orgGRN, memberGRN grn.GRN) ([]grn.GRN, error)

	// Membership — org-scoped, carries a Role.
	AddMember(ctx context.Context, m *Membership) error
	GetMember(ctx context.Context, orgGRN, userGRN grn.GRN) (*Membership, error)
	ListMembers(ctx context.Context, orgGRN grn.GRN) ([]*Membership, error)
	// RemoveMember returns ErrMemberNotFound when the pair names no
	// Membership, which an Organization that does not exist does too.
	RemoveMember(ctx context.Context, orgGRN, userGRN grn.GRN) error
	ListOrgsByMember(ctx context.Context, userGRN grn.GRN, f listfilter.Filter) ([]*Organization, error)

	// Invitation — pending until accepted, single-use.
	CreateInvitation(ctx context.Context, inv *Invitation) error
	GetInvitation(ctx context.Context, token string) (*Invitation, error)
	// AcceptInvitation spends the invitation and adds the user as a
	// member with its role, both or neither.
	AcceptInvitation(ctx context.Context, token string, userGRN grn.GRN) (*Membership, error)
	// ClaimInvitation spends the invitation for an account not yet
	// created; the claim is completed once it exists, or released.
	ClaimInvitation(ctx context.Context, token string) (*Invitation, error)
	CompleteInvitationClaim(ctx context.Context, inv *Invitation, userGRN grn.GRN) error
	ReleaseInvitationClaim(ctx context.Context, inv *Invitation) error
}
