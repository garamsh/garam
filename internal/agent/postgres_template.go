package agent

import (
	"context"

	"github.com/garamsh/garam/internal/agent/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
)

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
		GRN:             g,
		OrgGRN:          orgGRN,
		TemplateContent: TemplateContent{Name: name, Description: description, Values: values},
		CreatedBy:       createdBy,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}, nil
}
