package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/garamsh/garam/internal/agent/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
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
	return pgxerr.Classify(err, "define", nil, nil)
}

// ListDefinitionsByOrg returns the organization's definitions.
func (s *PostgresStore) ListDefinitionsByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Definition, error) {
	rows, err := s.q.ListAgentDefinitionsByOrg(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list definitions", nil, nil)
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

// ListDefinitionsByOperator returns the definitions naming the
// operator, each with the assignment claiming it where one does.
func (s *PostgresStore) ListDefinitionsByOperator(ctx context.Context, operatorGRN grn.GRN) ([]*DefinitionState, error) {
	rows, err := s.q.ListAgentDefinitionsByOperator(ctx, operatorGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list definitions", nil, nil)
	}
	states := make([]*DefinitionState, 0, len(rows))
	for _, row := range rows {
		d, err := rowToDefinition(db.AgentDefinition{
			AgentGrn:    row.AgentGrn,
			OrgGrn:      row.OrgGrn,
			OperatorGrn: row.OperatorGrn,
			AgentValues: row.AgentValues,
			DefinedBy:   row.DefinedBy,
			CreatedAt:   row.CreatedAt,
		})
		if err != nil {
			return nil, err
		}
		var claim *Assignment
		if row.ClaimEpoch.Valid {
			claim = &Assignment{
				agentGRN:    d.agentGRN,
				operatorGRN: d.operatorGRN,
				epoch:       int(row.ClaimEpoch.Int32),
				createdAt:   row.ClaimCreatedAt.Time,
				updatedAt:   row.ClaimUpdatedAt.Time,
			}
		}
		states = append(states, NewDefinitionState(d, claim))
	}
	return states, nil
}

// Claim binds the operator to the agent its definition minted: the
// agent row and the assignment are written in one transaction holding
// the definition's row lock, so two concurrent claims serialize and the
// second reads the assignment the first wrote — the conflict, never an
// override (ADR-0033). The definition writes no agent row before this
// (ADR-0026), so the claim is also where the agent comes to exist.
func (s *PostgresStore) Claim(ctx context.Context, agentGRN, operatorGRN grn.GRN) (*Assignment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "agent store: begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	row, err := q.LockAgentDefinition(ctx, agentGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "claim", ErrDefinitionNotFound, nil)
	}
	// A definition naming another operator is one this caller cannot
	// read either, so the claim answers as the read does.
	if row.OperatorGrn != operatorGRN.String() {
		return nil, ErrDefinitionNotFound
	}
	if _, err := q.GetAgentAssignment(ctx, agentGRN.String()); err == nil {
		return nil, ErrDefinitionClaimed
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, pgxerr.Classify(err, "claim", nil, nil)
	}
	d, err := rowToDefinition(row)
	if err != nil {
		return nil, err
	}
	a, err := New(d.agentGRN, d.orgGRN)
	if err != nil {
		return nil, err
	}
	now := stamp(time.Now().UTC())
	if err := q.CreateAgent(ctx, db.CreateAgentParams{
		Grn:               a.grn.String(),
		OrgGrn:            a.orgGRN.String(),
		ProvisioningState: string(a.provisioning),
		RuntimeState:      string(a.runtime),
		CreatedAt:         now,
		UpdatedAt:         now,
	}); err != nil {
		return nil, pgxerr.Classify(err, "claim", nil, nil)
	}
	assigned, err := q.AssignAgent(ctx, db.AssignAgentParams{
		AgentGrn:    agentGRN.String(),
		OperatorGrn: operatorGRN.String(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "claim", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "agent store: commit")
	}
	return rowToAssignment(assigned)
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
