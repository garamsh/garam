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

// CreateOrg persists a new Organization.
func (s *Store) CreateOrg(ctx context.Context, o *org.Organization) error {
	err := s.q.CreateOrg(ctx, db.CreateOrgParams{
		Grn:       o.GRN.String(),
		Name:      o.Name,
		CreatedAt: pgxtype.Timestamptz(o.CreatedAt),
		UpdatedAt: pgxtype.Timestamptz(o.UpdatedAt),
	})
	return pgxerr.Classify(err, "create org", nil, nil)
}

// GetOrg returns the Organization with the given GRN, or
// ErrOrgNotFound.
func (s *Store) GetOrg(ctx context.Context, g grn.GRN) (*org.Organization, error) {
	row, err := s.q.GetOrg(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get org", org.ErrOrgNotFound, nil)
	}
	return rowToOrg(row)
}

// ListOrgs returns Organizations ordered by GRN.
func (s *Store) ListOrgs(ctx context.Context, f listfilter.Filter) ([]*org.Organization, error) {
	rows, err := s.q.ListOrgs(ctx, db.ListOrgsParams{
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list orgs", nil, nil)
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

// UpdateOrg applies a Patch and returns the updated Organization.
func (s *Store) UpdateOrg(ctx context.Context, g grn.GRN, p org.Patch) (*org.Organization, error) {
	row, err := s.q.UpdateOrg(ctx, db.UpdateOrgParams{
		Grn:       g.String(),
		Name:      text(p.Name),
		UpdatedAt: pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "update org", org.ErrOrgNotFound, nil)
	}
	return rowToOrg(row)
}

// DeleteOrg removes the Organization and, via foreign keys, its
// Groups, Projects, Memberships, and Invitations.
func (s *Store) DeleteOrg(ctx context.Context, g grn.GRN) error {
	n, err := s.q.DeleteOrg(ctx, g.String())
	return classifyRows(n, err, "delete org", org.ErrOrgNotFound)
}

// CountOrgContents counts the Groups, Projects, Memberships and
// Invitations that DeleteOrg's cascade would remove with the
// Organization. One query, so the four numbers describe one moment
// rather than four.
func (s *Store) CountOrgContents(ctx context.Context, g grn.GRN) (org.Contents, error) {
	row, err := s.q.CountOrgContents(ctx, g.String())
	if err != nil {
		return org.Contents{}, pgxerr.Classify(err, "count org contents", nil, nil)
	}
	return org.Contents{
		Groups:      int(row.GroupCount),
		Projects:    int(row.ProjectCount),
		Members:     int(row.MemberCount),
		Invitations: int(row.InvitationCount),
	}, nil
}

// HoldsCertificateAuthority reports whether the organization still
// holds a retired certificate authority, which deleting it destroys
// (ADR-0118). It reads presence and nothing of the row.
func (s *Store) HoldsCertificateAuthority(ctx context.Context, g grn.GRN) (bool, error) {
	held, err := s.q.HoldsCertificateAuthority(ctx, g.String())
	if err != nil {
		return false, pgxerr.Classify(err, "read certificate authority presence", nil, nil)
	}
	return held, nil
}

// rowToOrg converts a sqlc row into the domain aggregate. A stored
// GRN that no longer parses is data corruption, hence internal.
func rowToOrg(row db.Org) (*org.Organization, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "org row grn", errorx.KindInternal)
	}
	return &org.Organization{GRN: g, Name: row.Name, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}, nil
}
