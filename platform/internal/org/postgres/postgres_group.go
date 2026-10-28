package postgres

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/org/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// CreateGroup persists a new Group.
func (s *Store) CreateGroup(ctx context.Context, g *org.Group) error {
	err := s.q.CreateGroup(ctx, db.CreateGroupParams{
		Grn:       g.GRN.String(),
		OrgGrn:    g.Organization.String(),
		Name:      g.Name,
		Roles:     strs(g.Roles),
		Policies:  strs(g.Policies),
		CreatedAt: pgxtype.Timestamptz(g.CreatedAt),
		UpdatedAt: pgxtype.Timestamptz(g.UpdatedAt),
	})
	return pgxerr.Classify(err, "create group", nil, nil)
}

// GetGroup returns the Group with the given GRN, or ErrGroupNotFound.
func (s *Store) GetGroup(ctx context.Context, g grn.GRN) (*org.Group, error) {
	row, err := s.q.GetGroup(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get group", org.ErrGroupNotFound, nil)
	}
	return rowToGroup(row)
}

// ListGroups returns the Organization's Groups ordered by GRN.
func (s *Store) ListGroups(ctx context.Context, orgGRN grn.GRN, f listfilter.Filter) ([]*org.Group, error) {
	rows, err := s.q.ListGroups(ctx, db.ListGroupsParams{
		OrgGrn:    orgGRN.String(),
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list groups", nil, nil)
	}
	groups := make([]*org.Group, 0, len(rows))
	for _, row := range rows {
		gr, err := rowToGroup(row)
		if err != nil {
			return nil, err
		}
		groups = append(groups, gr)
	}
	return groups, nil
}

// SearchGroups returns the org's groups whose name or identifier holds
// n, ordered by GRN.
func (s *Store) SearchGroups(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*org.Group, error) {
	rows, err := s.q.SearchGroups(ctx, db.SearchGroupsParams{
		OrgGrn:  orgGRN.String(),
		Pattern: pgxtype.Contains(n.Text()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "search groups", nil, nil)
	}
	groups := make([]*org.Group, 0, len(rows))
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
func (s *Store) UpdateGroup(ctx context.Context, g grn.GRN, p org.GroupPatch) (*org.Group, error) {
	row, err := s.q.UpdateGroup(ctx, db.UpdateGroupParams{
		Grn:       g.String(),
		Name:      text(p.Name),
		Roles:     p.Roles,
		Policies:  p.Policies,
		UpdatedAt: pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "update group", org.ErrGroupNotFound, nil)
	}
	return rowToGroup(row)
}

// DeleteGroup removes the Group with the given GRN.
func (s *Store) DeleteGroup(ctx context.Context, g grn.GRN) error {
	n, err := s.q.DeleteGroup(ctx, g.String())
	return classifyRows(n, err, "delete group", org.ErrGroupNotFound)
}

// AddGroupMember adds a member to the Group, or returns
// ErrMemberOfAnotherOrg and stores nothing.
func (s *Store) AddGroupMember(ctx context.Context, groupGRN, memberGRN grn.GRN) error {
	if err := org.CheckMemberOrg(groupGRN, memberGRN); err != nil {
		return err
	}
	err := s.q.AddGroupMember(ctx, db.AddGroupMemberParams{
		GroupGrn:  groupGRN.String(),
		MemberGrn: memberGRN.String(),
	})
	return pgxerr.Classify(err, "add group member", nil, nil)
}

// RemoveGroupMember removes a member from the Group, or returns
// ErrGroupMemberNotFound.
func (s *Store) RemoveGroupMember(ctx context.Context, groupGRN, memberGRN grn.GRN) error {
	n, err := s.q.RemoveGroupMember(ctx, db.RemoveGroupMemberParams{
		GroupGrn:  groupGRN.String(),
		MemberGrn: memberGRN.String(),
	})
	return classifyRows(n, err, "remove group member", org.ErrGroupMemberNotFound)
}

// ListGroupMembers returns the GRNs of the Group's members.
func (s *Store) ListGroupMembers(ctx context.Context, groupGRN grn.GRN) ([]grn.GRN, error) {
	rows, err := s.q.ListGroupMembers(ctx, groupGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list group members", nil, nil)
	}
	return parseGRNs(rows, "group member row")
}

// ListGroupsByMember returns the GRNs of the org's Groups the member
// belongs to.
func (s *Store) ListGroupsByMember(ctx context.Context, orgGRN, memberGRN grn.GRN) ([]grn.GRN, error) {
	rows, err := s.q.ListGroupsByMember(ctx, db.ListGroupsByMemberParams{
		OrgGrn:    orgGRN.String(),
		MemberGrn: memberGRN.String(),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list groups by member", nil, nil)
	}
	return parseGRNs(rows, "group row")
}

// ListMemberGroups returns the org's Groups the member belongs to,
// ordered by GRN.
func (s *Store) ListMemberGroups(ctx context.Context, orgGRN, memberGRN grn.GRN, f listfilter.Filter) ([]*org.Group, error) {
	rows, err := s.q.ListMemberGroups(ctx, db.ListMemberGroupsParams{
		OrgGrn:    orgGRN.String(),
		MemberGrn: memberGRN.String(),
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list member groups", nil, nil)
	}
	groups := make([]*org.Group, 0, len(rows))
	for _, row := range rows {
		gr, err := rowToGroup(row)
		if err != nil {
			return nil, err
		}
		groups = append(groups, gr)
	}
	return groups, nil
}

func rowToGroup(row db.Group) (*org.Group, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "group row grn", errorx.KindInternal)
	}
	og, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "group row org grn", errorx.KindInternal)
	}
	return &org.Group{
		GRN:          g,
		Name:         row.Name,
		Organization: og,
		Roles:        row.Roles,
		Policies:     row.Policies,
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}, nil
}
