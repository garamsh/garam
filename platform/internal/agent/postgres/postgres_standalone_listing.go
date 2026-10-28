package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/agent/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
)

func (s *Store) ListEnrollments(ctx context.Context, org grn.GRN, f listfilter.Filter) ([]agent.EnrollmentSummary, error) {
	rows, err := s.q.ListEnrollments(ctx, db.ListEnrollmentsParams{
		OrgGrn: org.String(), RowOffset: int32(f.Offset), RowLimit: rowLimit(f.Limit),
	})
	if err != nil {
		return nil, storageError(err, "list enrollments", nil, nil)
	}
	out := make([]agent.EnrollmentSummary, 0, len(rows))
	for _, row := range rows {
		e, err := enrollmentSummaryOf(row)
		if err != nil {
			return nil, errorx.Wrap(err, "stored enrollment", errorx.KindInternal)
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) ListSupervisors(ctx context.Context, org grn.GRN, f listfilter.Filter) ([]agent.SupervisorSummary, error) {
	rows, err := s.q.ListSupervisors(ctx, db.ListSupervisorsParams{
		OrgGrn: org.String(), RowOffset: int32(f.Offset), RowLimit: rowLimit(f.Limit),
	})
	if err != nil {
		return nil, storageError(err, "list supervisors", nil, nil)
	}
	out := make([]agent.SupervisorSummary, 0, len(rows))
	for _, row := range rows {
		sv, err := supervisorSummaryOf(row)
		if err != nil {
			return nil, errorx.Wrap(err, "stored supervisor", errorx.KindInternal)
		}
		out = append(out, sv)
	}
	return out, nil
}

func enrollmentSummaryOf(row db.ListEnrollmentsRow) (agent.EnrollmentSummary, error) {
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return agent.EnrollmentSummary{}, err
	}
	supervisor, err := grn.Parse(row.SupervisorGrn)
	if err != nil {
		return agent.EnrollmentSummary{}, err
	}
	approver, err := grn.Parse(row.ApprovedBy)
	if err != nil {
		return agent.EnrollmentSummary{}, err
	}
	placement, err := agent.ParsePlacement(row.Placement)
	if err != nil {
		return agent.EnrollmentSummary{}, err
	}
	e := agent.EnrollmentSummary{
		ID: agent.EnrollmentID(row.ID.Bytes), Placement: placement, Agent: agentGRN, Supervisor: supervisor,
		HostName: agent.Name(row.HostName), ApprovedBy: approver,
		ExpiresAt: row.ExpiresAt.Time, CreatedAt: row.CreatedAt.Time,
		WithdrawnAt: optionalTime(row.WithdrawnAt), SpentAt: optionalTime(row.ConsumedAt),
		Epoch: int(row.Epoch.Int32),
	}
	if row.ExpectedAssigneeGrn.Valid {
		assignee, err := grn.Parse(row.ExpectedAssigneeGrn.String)
		if err != nil {
			return agent.EnrollmentSummary{}, err
		}
		e.Replaces = &agent.Replacement{Agent: agentGRN, Assignee: assignee, Epoch: int(row.ExpectedEpoch.Int32)}
	}
	return e, nil
}

func supervisorSummaryOf(row db.ListSupervisorsRow) (agent.SupervisorSummary, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return agent.SupervisorSummary{}, err
	}
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return agent.SupervisorSummary{}, err
	}
	placement, err := agent.ParsePlacement(row.Placement)
	if err != nil {
		return agent.SupervisorSummary{}, err
	}
	return agent.SupervisorSummary{
		GRN: g, Agent: agentGRN, Enrollment: agent.EnrollmentID(row.EnrollmentID.Bytes), Placement: placement,
		HostName: agent.Name(row.HostName), Epoch: int(row.Epoch.Int32), RegisteredAt: row.RegisteredAt.Time,
		EndedAt: optionalTime(row.EndedAt), LastActivatedAt: optionalTime(row.LastActivatedAt),
	}, nil
}

// rowLimit is a listing's LIMIT: none for a filter's 0.
func rowLimit(n int) pgtype.Int4 {
	if n <= 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(n), Valid: true}
}

// optionalTime is a nullable timestamp as an optional time.
func optionalTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
