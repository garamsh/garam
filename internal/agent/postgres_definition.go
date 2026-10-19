package agent

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/agent/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
)

// GetDefinition returns the definition naming the agent, or
// ErrDefinitionNotFound.
func (s *PostgresStore) GetDefinition(ctx context.Context, agentGRN grn.GRN) (*Definition, error) {
	row, err := s.q.GetAgentDefinition(ctx, agentGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get definition", ErrDefinitionNotFound, nil)
	}
	return definitionAtStage(db.AgentDefinition{
		AgentGrn: row.AgentGrn, OrgGrn: row.OrgGrn, OperatorGrn: row.OperatorGrn, Name: row.Name,
		AgentValues: row.AgentValues, DefinedBy: row.DefinedBy, CreatedAt: row.CreatedAt,
	}, row.CutoverStage)
}

// RenameDefinition changes what the organization calls the agent and
// nothing else: the operator, the values and the GRN every other record
// joins on stay as they were (ADR-0075).
func (s *PostgresStore) RenameDefinition(ctx context.Context, agentGRN grn.GRN, name Name) (*Definition, error) {
	row, err := s.q.RenameAgentDefinition(ctx, db.RenameAgentDefinitionParams{
		AgentGrn: agentGRN.String(),
		Name:     string(name),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "rename definition", ErrDefinitionNotFound, nil)
	}
	return definitionAtStage(db.AgentDefinition{
		AgentGrn: row.AgentGrn, OrgGrn: row.OrgGrn, OperatorGrn: row.OperatorGrn, Name: row.Name,
		AgentValues: row.AgentValues, DefinedBy: row.DefinedBy, CreatedAt: row.CreatedAt,
	}, row.CutoverStage)
}

// ListDefinitionsByOrg returns the organization's definitions.
func (s *PostgresStore) ListDefinitionsByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Definition, error) {
	rows, err := s.q.ListAgentDefinitionsByOrg(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list definitions", nil, nil)
	}
	definitions := make([]*Definition, 0, len(rows))
	for _, row := range rows {
		d, err := definitionAtStage(db.AgentDefinition{
			AgentGrn: row.AgentGrn, OrgGrn: row.OrgGrn, OperatorGrn: row.OperatorGrn, Name: row.Name,
			AgentValues: row.AgentValues, DefinedBy: row.DefinedBy, CreatedAt: row.CreatedAt,
		}, row.CutoverStage)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, d)
	}
	return definitions, nil
}

// SearchByOrg returns the organization's agents whose name or
// identifier holds n: the defined ones by GRN, then the rest by GRN.
func (s *PostgresStore) SearchByOrg(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]Found, error) {
	pattern := pgxtype.Contains(n.Text())
	defined, err := s.q.SearchAgentDefinitionsByOrg(ctx, db.SearchAgentDefinitionsByOrgParams{OrgGrn: orgGRN.String(), Pattern: pattern})
	if err != nil {
		return nil, pgxerr.Classify(err, "search definitions", nil, nil)
	}
	undefined, err := s.q.SearchUndefinedAgentsByOrg(ctx, db.SearchUndefinedAgentsByOrgParams{OrgGrn: orgGRN.String(), Pattern: pattern})
	if err != nil {
		return nil, pgxerr.Classify(err, "search agents", nil, nil)
	}
	found := make([]Found, 0, len(defined)+len(undefined))
	for _, row := range defined {
		g, err := grn.Parse(row.AgentGrn)
		if err != nil {
			return nil, errorx.Wrap(err, "definition agent grn", errorx.KindInternal)
		}
		name, err := ParseName(row.Name)
		if err != nil {
			return nil, errorx.Wrap(err, "definition name", errorx.KindInternal)
		}
		found = append(found, Found{GRN: g, Name: &name})
	}
	for _, row := range undefined {
		g, err := grn.Parse(row)
		if err != nil {
			return nil, errorx.Wrap(err, "agent grn", errorx.KindInternal)
		}
		found = append(found, Found{GRN: g})
	}
	return found, nil
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
			Name:        row.Name,
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
				assigneeGRN: d.operatorGRN,
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
//
// The organization's row is held before the definition's, the order an
// organization's deletion takes them in, so a claim racing one either
// commits first or waits for it holding nothing and finds its
// definition gone (#1441).
func (s *PostgresStore) Claim(ctx context.Context, agentGRN, operatorGRN grn.GRN) (*Assignment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	// An organization that is gone holds no definition either.
	if _, err := q.HoldAgentOrg(ctx, grn.Org(agentGRN.Organization).String()); err != nil {
		return nil, pgxerr.Classify(err, "claim", ErrDefinitionNotFound, nil)
	}
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
	now := pgxtype.Timestamptz(time.Now().UTC())
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
	// The execution row is written, and so held, before the
	// assignment: the lock order every transaction follows (ADR-0080).
	if err := q.CreateAgentExecution(ctx, db.CreateAgentExecutionParams{
		AgentGrn:  agentGRN.String(),
		UpdatedAt: now,
	}); err != nil {
		return nil, pgxerr.Classify(err, "claim", nil, nil)
	}
	assigned, err := q.AssignAgent(ctx, db.AssignAgentParams{
		AgentGrn:    agentGRN.String(),
		AssigneeGrn: operatorGRN.String(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "claim", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "commit")
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
		return nil, errorx.Wrap(err, "definition agent grn", errorx.KindInternal)
	}
	orgGRN, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "definition org grn", errorx.KindInternal)
	}
	if err := validateOwnership(agentGRN, orgGRN); err != nil {
		return nil, errorx.Wrap(err, "stored definition", errorx.KindInternal)
	}
	operatorGRN, err := grn.Parse(row.OperatorGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "definition operator grn", errorx.KindInternal)
	}
	if err := validateOperator(operatorGRN, orgGRN); err != nil {
		return nil, errorx.Wrap(err, "stored definition", errorx.KindInternal)
	}
	definedBy, err := grn.Parse(row.DefinedBy)
	if err != nil {
		return nil, errorx.Wrap(err, "definition defined by", errorx.KindInternal)
	}
	name, err := ParseName(row.Name)
	if err != nil {
		return nil, errorx.Wrap(err, "definition name", errorx.KindInternal)
	}
	values, err := valuesFromRow(row.AgentValues)
	if err != nil {
		return nil, errorx.Wrap(err, "definition values", errorx.KindInternal)
	}
	return &Definition{
		agentGRN:    agentGRN,
		orgGRN:      orgGRN,
		operatorGRN: operatorGRN,
		Name:        name,
		values:      values,
		definedBy:   definedBy,
		createdAt:   row.CreatedAt.Time,
	}, nil
}

// definitionAtStage converts a definition read with the stage of its
// open cutover attempt, which is null where none is open.
func definitionAtStage(row db.AgentDefinition, stage pgtype.Text) (*Definition, error) {
	d, err := rowToDefinition(row)
	if err != nil || !stage.Valid {
		return d, err
	}
	st, err := ParseCutoverStage(stage.String)
	if err != nil {
		return nil, errorx.Wrap(err, "definition cutover stage", errorx.KindInternal)
	}
	d.Cutover = &st
	return d, nil
}

// valuesFromRow reads a stored values payload back through
// [ParseValues], which is the only way one reached the row.
func valuesFromRow(stored []byte) (Values, error) {
	var raw map[string]string
	if err := json.Unmarshal(stored, &raw); err != nil {
		return nil, err
	}
	return ParseValues(raw)
}
