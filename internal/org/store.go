package org

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// ListFilter scopes a List call. Limit of 0 means "no limit".
type ListFilter struct {
	Offset int
	Limit  int
}

// Patch holds partial-update fields. Nil entries are unchanged.
type Patch struct {
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
	ListOrgs(ctx context.Context, f ListFilter) ([]*Organization, error)
	UpdateOrg(ctx context.Context, g grn.GRN, p Patch) (*Organization, error)
	DeleteOrg(ctx context.Context, g grn.GRN) error

	// Group
	CreateGroup(ctx context.Context, g *Group) error
	GetGroup(ctx context.Context, g grn.GRN) (*Group, error)
	ListGroups(ctx context.Context, f ListFilter) ([]*Group, error)
	UpdateGroup(ctx context.Context, g grn.GRN, p Patch) (*Group, error)
	DeleteGroup(ctx context.Context, g grn.GRN) error
	AddGroupMember(ctx context.Context, groupGRN, memberGRN grn.GRN) error
	RemoveGroupMember(ctx context.Context, groupGRN, memberGRN grn.GRN) error
	ListGroupMembers(ctx context.Context, groupGRN grn.GRN) ([]grn.GRN, error)

	// Project
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, g grn.GRN) (*Project, error)
	ListProjects(ctx context.Context, f ListFilter) ([]*Project, error)
	UpdateProject(ctx context.Context, g grn.GRN, p Patch) (*Project, error)
	DeleteProject(ctx context.Context, g grn.GRN) error
	AddProjectMember(ctx context.Context, projectGRN, memberGRN grn.GRN) error
	RemoveProjectMember(ctx context.Context, projectGRN, memberGRN grn.GRN) error
	ListProjectMembers(ctx context.Context, projectGRN grn.GRN) ([]grn.GRN, error)
}
