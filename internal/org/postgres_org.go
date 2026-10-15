package org

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/org/db"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
)

// CreateOrg persists a new Organization.
func (s *PostgresStore) CreateOrg(ctx context.Context, o *Organization) error {
	err := s.q.CreateOrg(ctx, db.CreateOrgParams{
		Grn:       o.grn.String(),
		Name:      o.name,
		CreatedAt: pgxtype.Timestamptz(o.createdAt),
		UpdatedAt: pgxtype.Timestamptz(o.updatedAt),
	})
	return pgxerr.Classify(err, "create org", nil, nil)
}

// GetOrg returns the Organization with the given GRN, or
// ErrOrgNotFound.
func (s *PostgresStore) GetOrg(ctx context.Context, g grn.GRN) (*Organization, error) {
	row, err := s.q.GetOrg(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get org", ErrOrgNotFound, nil)
	}
	return rowToOrg(row)
}

// ListOrgs returns Organizations ordered by GRN.
func (s *PostgresStore) ListOrgs(ctx context.Context, f listfilter.Filter) ([]*Organization, error) {
	rows, err := s.q.ListOrgs(ctx, db.ListOrgsParams{
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list orgs", nil, nil)
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

// UpdateOrg applies a Patch and returns the updated Organization.
func (s *PostgresStore) UpdateOrg(ctx context.Context, g grn.GRN, p Patch) (*Organization, error) {
	row, err := s.q.UpdateOrg(ctx, db.UpdateOrgParams{
		Grn:       g.String(),
		Name:      text(p.Name),
		UpdatedAt: pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "update org", ErrOrgNotFound, nil)
	}
	return rowToOrg(row)
}

// DeleteOrg removes the Organization and, via foreign keys, its
// Groups, Projects, Memberships, and Invitations.
func (s *PostgresStore) DeleteOrg(ctx context.Context, g grn.GRN) error {
	n, err := s.q.DeleteOrg(ctx, g.String())
	return classifyRows(n, err, "delete org", ErrOrgNotFound)
}

// CountOrgContents counts the Groups, Projects, Memberships and
// Invitations that DeleteOrg's cascade would remove with the
// Organization. One query, so the four numbers describe one moment
// rather than four.
func (s *PostgresStore) CountOrgContents(ctx context.Context, g grn.GRN) (Contents, error) {
	row, err := s.q.CountOrgContents(ctx, g.String())
	if err != nil {
		return Contents{}, pgxerr.Classify(err, "count org contents", nil, nil)
	}
	return Contents{
		Groups:      int(row.GroupCount),
		Projects:    int(row.ProjectCount),
		Members:     int(row.MemberCount),
		Invitations: int(row.InvitationCount),
	}, nil
}

// rowToOrg converts a sqlc row into the domain aggregate. A stored
// GRN that no longer parses is data corruption, hence internal.
func rowToOrg(row db.Org) (*Organization, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "org row grn", errorx.KindInternal)
	}
	return &Organization{grn: g, name: row.Name, createdAt: row.CreatedAt.Time, updatedAt: row.UpdatedAt.Time}, nil
}
