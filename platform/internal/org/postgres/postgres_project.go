package postgres

import (
	"context"
	"time"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/org/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// CreateProject persists a new Project.
func (s *Store) CreateProject(ctx context.Context, p *org.Project) error {
	err := s.q.CreateProject(ctx, db.CreateProjectParams{
		Grn:       p.GRN.String(),
		OrgGrn:    p.Organization.String(),
		Name:      p.Name,
		Roles:     strs(p.Roles),
		Policies:  strs(p.Policies),
		CreatedAt: pgxtype.Timestamptz(p.CreatedAt),
		UpdatedAt: pgxtype.Timestamptz(p.UpdatedAt),
	})
	return pgxerr.Classify(err, "create project", nil, nil)
}

// GetProject returns the Project with the given GRN, or
// ErrProjectNotFound.
func (s *Store) GetProject(ctx context.Context, g grn.GRN) (*org.Project, error) {
	row, err := s.q.GetProject(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get project", org.ErrProjectNotFound, nil)
	}
	return rowToProject(row)
}

// ListProjects returns the Organization's Projects ordered by GRN.
func (s *Store) ListProjects(ctx context.Context, orgGRN grn.GRN, f listfilter.Filter) ([]*org.Project, error) {
	rows, err := s.q.ListProjects(ctx, db.ListProjectsParams{
		OrgGrn:    orgGRN.String(),
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list projects", nil, nil)
	}
	projects := make([]*org.Project, 0, len(rows))
	for _, row := range rows {
		pr, err := rowToProject(row)
		if err != nil {
			return nil, err
		}
		projects = append(projects, pr)
	}
	return projects, nil
}

// SearchProjects returns the org's projects whose name or identifier
// holds n, ordered by GRN.
func (s *Store) SearchProjects(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*org.Project, error) {
	rows, err := s.q.SearchProjects(ctx, db.SearchProjectsParams{
		OrgGrn:  orgGRN.String(),
		Pattern: pgxtype.Contains(n.Text()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "search projects", nil, nil)
	}
	projects := make([]*org.Project, 0, len(rows))
	for _, row := range rows {
		pr, err := rowToProject(row)
		if err != nil {
			return nil, err
		}
		projects = append(projects, pr)
	}
	return projects, nil
}

// UpdateProject applies a ProjectPatch and returns the updated Project.
func (s *Store) UpdateProject(ctx context.Context, g grn.GRN, p org.ProjectPatch) (*org.Project, error) {
	row, err := s.q.UpdateProject(ctx, db.UpdateProjectParams{
		Grn:       g.String(),
		Name:      text(p.Name),
		Roles:     p.Roles,
		Policies:  p.Policies,
		UpdatedAt: pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "update project", org.ErrProjectNotFound, nil)
	}
	return rowToProject(row)
}

// DeleteProject removes the Project with the given GRN.
func (s *Store) DeleteProject(ctx context.Context, g grn.GRN) error {
	n, err := s.q.DeleteProject(ctx, g.String())
	return classifyRows(n, err, "delete project", org.ErrProjectNotFound)
}

// AddProjectMember adds a member to the Project, or returns
// ErrMemberOfAnotherOrg and stores nothing.
func (s *Store) AddProjectMember(ctx context.Context, projectGRN, memberGRN grn.GRN) error {
	if err := org.CheckMemberOrg(projectGRN, memberGRN); err != nil {
		return err
	}
	err := s.q.AddProjectMember(ctx, db.AddProjectMemberParams{
		ProjectGrn: projectGRN.String(),
		MemberGrn:  memberGRN.String(),
	})
	return pgxerr.Classify(err, "add project member", nil, nil)
}

// RemoveProjectMember removes a member from the Project, or returns
// ErrProjectMemberNotFound.
func (s *Store) RemoveProjectMember(ctx context.Context, projectGRN, memberGRN grn.GRN) error {
	n, err := s.q.RemoveProjectMember(ctx, db.RemoveProjectMemberParams{
		ProjectGrn: projectGRN.String(),
		MemberGrn:  memberGRN.String(),
	})
	return classifyRows(n, err, "remove project member", org.ErrProjectMemberNotFound)
}

// ListProjectMembers returns the GRNs of the Project's members.
func (s *Store) ListProjectMembers(ctx context.Context, projectGRN grn.GRN) ([]grn.GRN, error) {
	rows, err := s.q.ListProjectMembers(ctx, projectGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list project members", nil, nil)
	}
	return parseGRNs(rows, "project member row")
}

// ListProjectsByMember returns the GRNs of the org's Projects the
// member belongs to.
func (s *Store) ListProjectsByMember(ctx context.Context, orgGRN, memberGRN grn.GRN) ([]grn.GRN, error) {
	rows, err := s.q.ListProjectsByMember(ctx, db.ListProjectsByMemberParams{
		OrgGrn:    orgGRN.String(),
		MemberGrn: memberGRN.String(),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list projects by member", nil, nil)
	}
	return parseGRNs(rows, "project row")
}

func rowToProject(row db.Project) (*org.Project, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "project row grn", errorx.KindInternal)
	}
	og, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "project row org grn", errorx.KindInternal)
	}
	return &org.Project{
		GRN:          g,
		Name:         row.Name,
		Organization: og,
		Roles:        row.Roles,
		Policies:     row.Policies,
		CreatedAt:    row.CreatedAt.Time,
		UpdatedAt:    row.UpdatedAt.Time,
	}, nil
}
