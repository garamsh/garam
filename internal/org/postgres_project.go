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

// CreateProject persists a new Project.
func (s *PostgresStore) CreateProject(ctx context.Context, p *Project) error {
	err := s.q.CreateProject(ctx, db.CreateProjectParams{
		Grn:       p.grn.String(),
		OrgGrn:    p.orgGRN.String(),
		Name:      p.name,
		Roles:     strs(p.roles),
		Policies:  strs(p.policies),
		CreatedAt: pgxtype.Timestamptz(p.createdAt),
		UpdatedAt: pgxtype.Timestamptz(p.updatedAt),
	})
	return pgxerr.Classify(err, "create project", nil, nil)
}

// GetProject returns the Project with the given GRN, or
// ErrProjectNotFound.
func (s *PostgresStore) GetProject(ctx context.Context, g grn.GRN) (*Project, error) {
	row, err := s.q.GetProject(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get project", ErrProjectNotFound, nil)
	}
	return rowToProject(row)
}

// ListProjects returns the Organization's Projects ordered by GRN.
func (s *PostgresStore) ListProjects(ctx context.Context, orgGRN grn.GRN, f listfilter.Filter) ([]*Project, error) {
	rows, err := s.q.ListProjects(ctx, db.ListProjectsParams{
		OrgGrn:    orgGRN.String(),
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list projects", nil, nil)
	}
	projects := make([]*Project, 0, len(rows))
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
func (s *PostgresStore) UpdateProject(ctx context.Context, g grn.GRN, p ProjectPatch) (*Project, error) {
	row, err := s.q.UpdateProject(ctx, db.UpdateProjectParams{
		Grn:       g.String(),
		Name:      text(p.Name),
		Roles:     p.Roles,
		Policies:  p.Policies,
		UpdatedAt: pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "update project", ErrProjectNotFound, nil)
	}
	return rowToProject(row)
}

// DeleteProject removes the Project with the given GRN.
func (s *PostgresStore) DeleteProject(ctx context.Context, g grn.GRN) error {
	n, err := s.q.DeleteProject(ctx, g.String())
	return classifyRows(n, err, "delete project", ErrProjectNotFound)
}

// AddProjectMember adds a member to the Project, or returns
// ErrMemberOfAnotherOrg and stores nothing.
func (s *PostgresStore) AddProjectMember(ctx context.Context, projectGRN, memberGRN grn.GRN) error {
	if err := checkMemberOrg(projectGRN, memberGRN); err != nil {
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
func (s *PostgresStore) RemoveProjectMember(ctx context.Context, projectGRN, memberGRN grn.GRN) error {
	n, err := s.q.RemoveProjectMember(ctx, db.RemoveProjectMemberParams{
		ProjectGrn: projectGRN.String(),
		MemberGrn:  memberGRN.String(),
	})
	return classifyRows(n, err, "remove project member", ErrProjectMemberNotFound)
}

// ListProjectMembers returns the GRNs of the Project's members.
func (s *PostgresStore) ListProjectMembers(ctx context.Context, projectGRN grn.GRN) ([]grn.GRN, error) {
	rows, err := s.q.ListProjectMembers(ctx, projectGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list project members", nil, nil)
	}
	return parseGRNs(rows, "project member row")
}

func rowToProject(row db.Project) (*Project, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "project row grn", errorx.KindInternal)
	}
	og, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "project row org grn", errorx.KindInternal)
	}
	return &Project{
		grn:       g,
		name:      row.Name,
		orgGRN:    og,
		roles:     row.Roles,
		policies:  row.Policies,
		createdAt: row.CreatedAt.Time,
		updatedAt: row.UpdatedAt.Time,
	}, nil
}
