package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/agent/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
)

// Assign binds the agent to an assignee at the next epoch. Every call
// raises the epoch, so the pods of the assignment it replaces are
// fenced out whether or not the assignee changed, and ends the
// activation the replaced assignment's runtime held (ADR-0078).
//
// The execution row is locked before the assignment is written: that
// is the order every transaction taking both follows (ADR-0080).
func (s *Store) Assign(ctx context.Context, agentGRN, assigneeGRN grn.GRN) (*agent.Assignment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if _, err := q.LockAgentExecution(ctx, agentGRN.String()); err != nil {
		return nil, pgxerr.Classify(err, "assign", agent.ErrAgentNotFound, nil)
	}
	now := pgxtype.Timestamptz(time.Now().UTC())
	row, err := q.AssignAgent(ctx, db.AssignAgentParams{
		AgentGrn:    agentGRN.String(),
		AssigneeGrn: assigneeGRN.String(),
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "assign", agent.ErrAgentNotFound, nil)
	}
	if err := q.EndReassignedActivation(ctx, db.EndReassignedActivationParams{
		AgentGrn:  agentGRN.String(),
		UpdatedAt: now,
	}); err != nil {
		return nil, pgxerr.Classify(err, "assign", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "commit")
	}
	return rowToAssignment(row)
}

// GetAssignment returns the assignee the agent is assigned to and the
// epoch it is assigned at, or ErrAssignmentNotFound.
func (s *Store) GetAssignment(ctx context.Context, agentGRN grn.GRN) (*agent.Assignment, error) {
	row, err := s.q.GetAgentAssignment(ctx, agentGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get assignment", agent.ErrAssignmentNotFound, nil)
	}
	return rowToAssignment(row)
}

// ListAssignmentsByOrg returns the assignments of the organization's
// agents. An unassigned agent contributes none.
func (s *Store) ListAssignmentsByOrg(ctx context.Context, orgGRN grn.GRN) ([]*agent.Assignment, error) {
	rows, err := s.q.ListAgentAssignmentsByOrg(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list assignments", nil, nil)
	}
	assignments := make([]*agent.Assignment, 0, len(rows))
	for _, row := range rows {
		a, err := rowToAssignment(row)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, a)
	}
	return assignments, nil
}

// errNeverActivated is the row LatestGeneration finds for an agent
// never activated: none, which it answers as nil rather than an error.
var errNeverActivated = errorx.New(errorx.KindNotFound, "agent never activated")

// LatestGeneration reads the generation of the agent's most recent
// activation.
func (s *Store) LatestGeneration(ctx context.Context, agentGRN grn.GRN) (*agent.Generation, error) {
	raw, err := s.q.GetLatestGeneration(ctx, agentGRN.String())
	if err != nil {
		err = pgxerr.Classify(err, "get latest generation", errNeverActivated, nil)
		if errors.Is(err, errNeverActivated) {
			return nil, nil
		}
		return nil, err
	}
	g, err := agent.ParseGeneration(raw)
	if err != nil {
		return nil, errorx.Wrap(err, "stored generation", errorx.KindInternal)
	}
	return &g, nil
}

// LatestGenerationsByOrg reads the generation of each of the
// organization's agents' most recent activation.
func (s *Store) LatestGenerationsByOrg(ctx context.Context, orgGRN grn.GRN) (map[grn.GRN]agent.Generation, error) {
	rows, err := s.q.ListLatestGenerationsByOrg(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list latest generations", nil, nil)
	}
	out := make(map[grn.GRN]agent.Generation, len(rows))
	for _, row := range rows {
		agentGRN, err := grn.Parse(row.AgentGrn)
		if err != nil {
			return nil, errorx.Wrap(err, "stored activation agent grn", errorx.KindInternal)
		}
		g, err := agent.ParseGeneration(row.Generation)
		if err != nil {
			return nil, errorx.Wrap(err, "stored generation", errorx.KindInternal)
		}
		out[agentGRN] = g
	}
	return out, nil
}

func rowToAssignment(row db.AgentAssignment) (*agent.Assignment, error) {
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "assignment agent grn", errorx.KindInternal)
	}
	assigneeGRN, err := grn.Parse(row.AssigneeGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "assignment assignee grn", errorx.KindInternal)
	}
	return &agent.Assignment{
		Agent:     agentGRN,
		Assignee:  assigneeGRN,
		Epoch:     int(row.Epoch),
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}, nil
}
