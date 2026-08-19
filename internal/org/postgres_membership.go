package org

import (
	"context"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/org/db"
)

// AddMember persists a new org Membership.
func (s *PostgresStore) AddMember(ctx context.Context, m *Membership) error {
	err := s.q.AddOrgMember(ctx, db.AddOrgMemberParams{
		OrgGrn:    m.orgGRN.String(),
		UserGrn:   m.userGRN.String(),
		Role:      string(m.role),
		CreatedAt: stamp(m.createdAt),
	})
	return classify(err, "add member", nil, ErrMemberAlreadyExists)
}

// GetMember returns the Membership of a user in an org, or
// ErrMemberNotFound.
func (s *PostgresStore) GetMember(ctx context.Context, orgGRN, userGRN grn.GRN) (*Membership, error) {
	row, err := s.q.GetOrgMember(ctx, db.GetOrgMemberParams{
		OrgGrn:  orgGRN.String(),
		UserGrn: userGRN.String(),
	})
	if err != nil {
		return nil, classify(err, "get member", ErrMemberNotFound, nil)
	}
	return rowToMembership(row)
}

// ListMembers returns the Memberships of an org ordered by user GRN.
func (s *PostgresStore) ListMembers(ctx context.Context, orgGRN grn.GRN) ([]*Membership, error) {
	rows, err := s.q.ListOrgMembers(ctx, orgGRN.String())
	if err != nil {
		return nil, classify(err, "list members", nil, nil)
	}
	members := make([]*Membership, 0, len(rows))
	for _, row := range rows {
		m, err := rowToMembership(row)
		if err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, nil
}

// RemoveMember removes a user's Membership from an org, or returns
// ErrMemberNotFound.
func (s *PostgresStore) RemoveMember(ctx context.Context, orgGRN, userGRN grn.GRN) error {
	n, err := s.q.RemoveOrgMember(ctx, db.RemoveOrgMemberParams{
		OrgGrn:  orgGRN.String(),
		UserGrn: userGRN.String(),
	})
	return classifyRows(n, err, "remove member", ErrMemberNotFound)
}

// ListOrgsByMember returns the Organizations a user belongs to,
// ordered by GRN.
func (s *PostgresStore) ListOrgsByMember(ctx context.Context, userGRN grn.GRN, f listfilter.Filter) ([]*Organization, error) {
	rows, err := s.q.ListOrgsByMember(ctx, db.ListOrgsByMemberParams{
		UserGrn:   userGRN.String(),
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, classify(err, "list orgs by member", nil, nil)
	}
	orgs := make([]*Organization, 0, len(rows))
	for _, row := range rows {
		o, err := rowToOrg(row)
		if err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, nil
}

func rowToMembership(row db.OrgMember) (*Membership, error) {
	role, err := ParseRole(row.Role)
	if err != nil {
		return nil, errorx.Wrap(err, "member row role", errorx.KindInternal)
	}
	og, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "member row org grn", errorx.KindInternal)
	}
	ug, err := grn.Parse(row.UserGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "member row user grn", errorx.KindInternal)
	}
	return &Membership{
		orgGRN:    og,
		userGRN:   ug,
		role:      role,
		createdAt: row.CreatedAt.Time,
	}, nil
}
