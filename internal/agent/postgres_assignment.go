package agent

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/agent/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
)

// Assign binds the agent to an operator at the next epoch. Every call
// raises the epoch, so the pods of the assignment it replaces are
// fenced out whether or not the operator changed.
func (s *PostgresStore) Assign(ctx context.Context, agentGRN, operatorGRN grn.GRN) (*Assignment, error) {
	now := stamp(time.Now().UTC())
	row, err := s.q.AssignAgent(ctx, db.AssignAgentParams{
		AgentGrn:    agentGRN.String(),
		OperatorGrn: operatorGRN.String(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "assign", ErrAgentNotFound, nil)
	}
	return rowToAssignment(row)
}

// GetAssignment returns the operator the agent is assigned to and the
// epoch it is assigned at, or ErrAssignmentNotFound.
func (s *PostgresStore) GetAssignment(ctx context.Context, agentGRN grn.GRN) (*Assignment, error) {
	row, err := s.q.GetAgentAssignment(ctx, agentGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get assignment", ErrAssignmentNotFound, nil)
	}
	return rowToAssignment(row)
}

// ListAssignmentsByOrg returns the assignments of the organization's
// agents. An unassigned agent contributes none.
func (s *PostgresStore) ListAssignmentsByOrg(ctx context.Context, orgGRN grn.GRN) ([]*Assignment, error) {
	rows, err := s.q.ListAgentAssignmentsByOrg(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list assignments", nil, nil)
	}
	assignments := make([]*Assignment, 0, len(rows))
	for _, row := range rows {
		a, err := rowToAssignment(row)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, a)
	}
	return assignments, nil
}

func rowToAssignment(row db.AgentAssignment) (*Assignment, error) {
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "assignment agent grn", errorx.KindInternal)
	}
	operatorGRN, err := grn.Parse(row.OperatorGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "assignment operator grn", errorx.KindInternal)
	}
	return &Assignment{
		agentGRN:    agentGRN,
		operatorGRN: operatorGRN,
		epoch:       int(row.Epoch),
		createdAt:   row.CreatedAt.Time,
		updatedAt:   row.UpdatedAt.Time,
	}, nil
}
