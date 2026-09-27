package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/garamsh/garam/internal/agent/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
)

// CreateTemplate persists a template.
func (s *PostgresStore) CreateTemplate(ctx context.Context, t *Template) error {
	values, err := json.Marshal(t.content.Values)
	if err != nil {
		return errorx.Wrap(err, "create template: values", errorx.KindInternal)
	}
	err = s.q.CreateAgentTemplate(ctx, db.CreateAgentTemplateParams{
		Grn:         t.grn.String(),
		OrgGrn:      t.orgGRN.String(),
		Name:        string(t.content.Name),
		Description: string(t.content.Description),
		AgentValues: values,
		CreatedBy:   t.createdBy.String(),
		CreatedAt:   stamp(t.createdAt),
		UpdatedAt:   stamp(t.updatedAt),
	})
	return pgxerr.Classify(err, "create template", nil, nil)
}

// GetTemplate returns the template, or ErrTemplateNotFound.
func (s *PostgresStore) GetTemplate(ctx context.Context, g grn.GRN) (*Template, error) {
	row, err := s.q.GetAgentTemplate(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get template", ErrTemplateNotFound, nil)
	}
	return rowToTemplate(row)
}

// ListTemplatesByOrg returns the organization's templates.
func (s *PostgresStore) ListTemplatesByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Template, error) {
	rows, err := s.q.ListAgentTemplatesByOrg(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list templates", nil, nil)
	}
	templates := make([]*Template, 0, len(rows))
	for _, row := range rows {
		t, err := rowToTemplate(row)
		if err != nil {
			return nil, err
		}
		templates = append(templates, t)
	}
	return templates, nil
}

// UpdateTemplate replaces what the template says, or answers
// ErrTemplateNotFound. No definition references the template, so the
// write reaches no agent defined from it (ADR-0075).
func (s *PostgresStore) UpdateTemplate(ctx context.Context, g grn.GRN, content TemplateContent) (*Template, error) {
	if content.Values == nil {
		content.Values = Values{}
	}
	values, err := json.Marshal(content.Values)
	if err != nil {
		return nil, errorx.Wrap(err, "update template: values", errorx.KindInternal)
	}
	row, err := s.q.UpdateAgentTemplate(ctx, db.UpdateAgentTemplateParams{
		Grn:         g.String(),
		Name:        string(content.Name),
		Description: string(content.Description),
		AgentValues: values,
		UpdatedAt:   stamp(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "update template", ErrTemplateNotFound, nil)
	}
	return rowToTemplate(row)
}

// DeleteTemplate removes the template, or answers ErrTemplateNotFound.
func (s *PostgresStore) DeleteTemplate(ctx context.Context, g grn.GRN) error {
	n, err := s.q.DeleteAgentTemplate(ctx, g.String())
	if err != nil {
		return pgxerr.Classify(err, "delete template", nil, nil)
	}
	if n == 0 {
		return ErrTemplateNotFound
	}
	return nil
}

// rowToTemplate converts a sqlc row into the domain aggregate. Every
// field reached the row through its parse, so one that no longer
// parses is data corruption, hence internal.
func rowToTemplate(row db.AgentTemplate) (*Template, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "template grn", errorx.KindInternal)
	}
	orgGRN, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "template org grn", errorx.KindInternal)
	}
	if err := validateTemplateOwnership(g, orgGRN); err != nil {
		return nil, errorx.Wrap(err, "stored template", errorx.KindInternal)
	}
	name, err := ParseName(row.Name)
	if err != nil {
		return nil, errorx.Wrap(err, "template name", errorx.KindInternal)
	}
	description, err := ParseDescription(row.Description)
	if err != nil {
		return nil, errorx.Wrap(err, "template description", errorx.KindInternal)
	}
	values, err := valuesFromRow(row.AgentValues)
	if err != nil {
		return nil, errorx.Wrap(err, "template values", errorx.KindInternal)
	}
	createdBy, err := grn.Parse(row.CreatedBy)
	if err != nil {
		return nil, errorx.Wrap(err, "template created by", errorx.KindInternal)
	}
	return &Template{
		grn:       g,
		orgGRN:    orgGRN,
		content:   TemplateContent{Name: name, Description: description, Values: values},
		createdBy: createdBy,
		createdAt: row.CreatedAt.Time,
		updatedAt: row.UpdatedAt.Time,
	}, nil
}
