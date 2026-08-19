package org

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/org/db"
)

// CreateGroup persists a new Group.
func (s *PostgresStore) CreateGroup(ctx context.Context, g *Group) error {
	err := s.q.CreateGroup(ctx, db.CreateGroupParams{
		Grn:       g.grn.String(),
		OrgGrn:    g.orgGRN.String(),
		Name:      g.name,
		Roles:     strs(g.roles),
		Policies:  strs(g.policies),
		CreatedAt: stamp(g.createdAt),
		UpdatedAt: stamp(g.updatedAt),
	})
	return classify(err, "create group", nil, nil)
}

// GetGroup returns the Group with the given GRN, or ErrGroupNotFound.
func (s *PostgresStore) GetGroup(ctx context.Context, g grn.GRN) (*Group, error) {
	row, err := s.q.GetGroup(ctx, g.String())
	if err != nil {
		return nil, classify(err, "get group", ErrGroupNotFound, nil)
	}
	return rowToGroup(row)
}

// ListGroups returns the Organization's Groups ordered by GRN.
func (s *PostgresStore) ListGroups(ctx context.Context, orgGRN grn.GRN, f listfilter.Filter) ([]*Group, error) {
	rows, err := s.q.ListGroups(ctx, db.ListGroupsParams{
		OrgGrn:    orgGRN.String(),
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, classify(err, "list groups", nil, nil)
	}
	groups := make([]*Group, 0, len(rows))
	for _, row := range rows {
		gr, err := rowToGroup(row)
		if err != nil {
			return nil, err
		}
		groups = append(groups, gr)
	}
	return groups, nil
}

// UpdateGroup applies a GroupPatch and returns the updated Group.
func (s *PostgresStore) UpdateGroup(ctx context.Context, g grn.GRN, p GroupPatch) (*Group, error) {
	row, err := s.q.UpdateGroup(ctx, db.UpdateGroupParams{
		Grn:       g.String(),
		Name:      text(p.Name),
		Roles:     p.Roles,
		Policies:  p.Policies,
		UpdatedAt: stamp(time.Now().UTC()),
	})
	if err != nil {
		return nil, classify(err, "update group", ErrGroupNotFound, nil)
	}
	return rowToGroup(row)
}

// DeleteGroup removes the Group with the given GRN.
func (s *PostgresStore) DeleteGroup(ctx context.Context, g grn.GRN) error {
	n, err := s.q.DeleteGroup(ctx, g.String())
	return classifyRows(n, err, "delete group", ErrGroupNotFound)
}

// AddGroupMember adds a member to the Group.
func (s *PostgresStore) AddGroupMember(ctx context.Context, groupGRN, memberGRN grn.GRN) error {
	err := s.q.AddGroupMember(ctx, db.AddGroupMemberParams{
		GroupGrn:  groupGRN.String(),
		MemberGrn: memberGRN.String(),
	})
	return classify(err, "add group member", nil, nil)
}

// RemoveGroupMember removes a member from the Group, or returns
// ErrGroupMemberNotFound.
func (s *PostgresStore) RemoveGroupMember(ctx context.Context, groupGRN, memberGRN grn.GRN) error {
	n, err := s.q.RemoveGroupMember(ctx, db.RemoveGroupMemberParams{
		GroupGrn:  groupGRN.String(),
		MemberGrn: memberGRN.String(),
	})
	return classifyRows(n, err, "remove group member", ErrGroupMemberNotFound)
}

// ListGroupMembers returns the GRNs of the Group's members.
func (s *PostgresStore) ListGroupMembers(ctx context.Context, groupGRN grn.GRN) ([]grn.GRN, error) {
	rows, err := s.q.ListGroupMembers(ctx, groupGRN.String())
	if err != nil {
		return nil, classify(err, "list group members", nil, nil)
	}
	return parseGRNs(rows, "group member row")
}

// ListGroupsByMember returns the GRNs of the org's Groups the member
// belongs to.
func (s *PostgresStore) ListGroupsByMember(ctx context.Context, orgGRN, memberGRN grn.GRN) ([]grn.GRN, error) {
	rows, err := s.q.ListGroupsByMember(ctx, db.ListGroupsByMemberParams{
		OrgGrn:    orgGRN.String(),
		MemberGrn: memberGRN.String(),
	})
	if err != nil {
		return nil, classify(err, "list groups by member", nil, nil)
	}
	return parseGRNs(rows, "group row")
}

func rowToGroup(row db.Group) (*Group, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "group row grn", errorx.KindInternal)
	}
	og, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "group row org grn", errorx.KindInternal)
	}
	return &Group{
		grn:       g,
		name:      row.Name,
		orgGRN:    og,
		roles:     row.Roles,
		policies:  row.Policies,
		createdAt: row.CreatedAt.Time,
		updatedAt: row.UpdatedAt.Time,
	}, nil
}
