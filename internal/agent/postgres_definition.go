package agent

import (
	"context"
	"encoding/json"

	"github.com/garamsh/garam/internal/agent/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Define persists a definition. It writes no agent row: what an
// organization says an agent should be and what an operator says
// exists are separate records (ADR-0026).
func (s *PostgresStore) Define(ctx context.Context, d *Definition) error {
	values, err := json.Marshal(d.values)
	if err != nil {
		return errorx.Wrap(err, "agent store: define: values", errorx.KindInternal)
	}
	err = s.q.CreateAgentDefinition(ctx, db.CreateAgentDefinitionParams{
		AgentGrn:    d.agentGRN.String(),
		OrgGrn:      d.orgGRN.String(),
		OperatorGrn: d.operatorGRN.String(),
		AgentValues: values,
		DefinedBy:   d.definedBy.String(),
		CreatedAt:   stamp(d.createdAt),
	})
	return classify(err, "define", nil, nil)
}

// ListDefinitionsByOrg returns the organization's definitions.
func (s *PostgresStore) ListDefinitionsByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Definition, error) {
	rows, err := s.q.ListAgentDefinitionsByOrg(ctx, orgGRN.String())
	if err != nil {
		return nil, classify(err, "list definitions", nil, nil)
	}
	definitions := make([]*Definition, 0, len(rows))
	for _, row := range rows {
		d, err := rowToDefinition(row)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, d)
	}
	return definitions, nil
}

// rowToDefinition converts a sqlc row into the domain aggregate. A
// stored value that no longer parses is data corruption, hence
// internal — the values included, which reach the row only through
// [ParseValues].
func rowToDefinition(row db.AgentDefinition) (*Definition, error) {
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "agent store: definition agent grn", errorx.KindInternal)
	}
	orgGRN, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "agent store: definition org grn", errorx.KindInternal)
	}
	if err := validateOwnership(agentGRN, orgGRN); err != nil {
		return nil, errorx.Wrap(err, "agent store: stored definition", errorx.KindInternal)
	}
	operatorGRN, err := grn.Parse(row.OperatorGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "agent store: definition operator grn", errorx.KindInternal)
	}
	if err := validateOperator(operatorGRN, orgGRN); err != nil {
		return nil, errorx.Wrap(err, "agent store: stored definition", errorx.KindInternal)
	}
	definedBy, err := grn.Parse(row.DefinedBy)
	if err != nil {
		return nil, errorx.Wrap(err, "agent store: definition defined by", errorx.KindInternal)
	}
	var raw map[string]string
	if err := json.Unmarshal(row.AgentValues, &raw); err != nil {
		return nil, errorx.Wrap(err, "agent store: definition values", errorx.KindInternal)
	}
	values, err := ParseValues(raw)
	if err != nil {
		return nil, errorx.Wrap(err, "agent store: definition values", errorx.KindInternal)
	}
	return &Definition{
		agentGRN:    agentGRN,
		orgGRN:      orgGRN,
		operatorGRN: operatorGRN,
		values:      values,
		definedBy:   definedBy,
		createdAt:   row.CreatedAt.Time,
	}, nil
}
