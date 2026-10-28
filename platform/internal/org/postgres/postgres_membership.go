package postgres

import (
	"context"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/org/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// AddMember persists a new org Membership.
func (s *Store) AddMember(ctx context.Context, m *org.Membership) error {
	err := s.q.AddOrgMember(ctx, db.AddOrgMemberParams{
		OrgGrn:    m.Organization.String(),
		UserGrn:   m.User.String(),
		Role:      string(m.Role),
		CreatedAt: pgxtype.Timestamptz(m.CreatedAt),
	})
	return pgxerr.Classify(err, "add member", nil, org.ErrMemberAlreadyExists)
}

// GetMember returns the Membership of a user in an org, or
// ErrMemberNotFound.
func (s *Store) GetMember(ctx context.Context, orgGRN, userGRN grn.GRN) (*org.Membership, error) {
	row, err := s.q.GetOrgMember(ctx, db.GetOrgMemberParams{
		OrgGrn:  orgGRN.String(),
		UserGrn: userGRN.String(),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "get member", org.ErrMemberNotFound, nil)
	}
	return rowToMembership(row)
}

// ListMembers returns the Memberships of an org ordered by user GRN.
func (s *Store) ListMembers(ctx context.Context, orgGRN grn.GRN) ([]*org.Membership, error) {
	rows, err := s.q.ListOrgMembers(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list members", nil, nil)
	}
	members := make([]*org.Membership, 0, len(rows))
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
func (s *Store) RemoveMember(ctx context.Context, orgGRN, userGRN grn.GRN) error {
	n, err := s.q.RemoveOrgMember(ctx, db.RemoveOrgMemberParams{
		OrgGrn:  orgGRN.String(),
		UserGrn: userGRN.String(),
	})
	return classifyRows(n, err, "remove member", org.ErrMemberNotFound)
}

// ListOrgsByMember returns the Organizations a user belongs to,
// ordered by GRN.
func (s *Store) ListOrgsByMember(ctx context.Context, userGRN grn.GRN, f listfilter.Filter) ([]*org.Organization, error) {
	rows, err := s.q.ListOrgsByMember(ctx, db.ListOrgsByMemberParams{
		UserGrn:   userGRN.String(),
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list orgs by member", nil, nil)
	}
	orgs := make([]*org.Organization, 0, len(rows))
	for _, row := range rows {
		o, err := rowToOrg(row)
		if err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, nil
}

func rowToMembership(row db.OrgMember) (*org.Membership, error) {
	role, err := org.ParseRole(row.Role)
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
	return &org.Membership{
		Organization: og,
		User:         ug,
		Role:         role,
		CreatedAt:    row.CreatedAt.Time,
	}, nil
}
