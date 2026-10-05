package agent

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/garamsh/garam/internal/agent/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
	"github.com/garamsh/garam/internal/pgxerr"
)

// Create persists a new Agent and its execution row, in one
// transaction, so no agent exists without the row the execution fence
// locks (ADR-0080).
func (s *PostgresStore) Create(ctx context.Context, a *Agent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if err := q.CreateAgent(ctx, db.CreateAgentParams{
		Grn:               a.grn.String(),
		OrgGrn:            a.orgGRN.String(),
		ProvisioningState: string(a.provisioning),
		RuntimeState:      string(a.runtime),
		CreatedAt:         stamp(a.createdAt),
		UpdatedAt:         stamp(a.updatedAt),
	}); err != nil {
		return pgxerr.Classify(err, "create", nil, ErrAgentAlreadyExists)
	}
	if err := q.CreateAgentExecution(ctx, db.CreateAgentExecutionParams{
		AgentGrn:  a.grn.String(),
		UpdatedAt: stamp(a.createdAt),
	}); err != nil {
		return pgxerr.Classify(err, "create", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit")
	}
	return nil
}

// Get returns the Agent with the given GRN, or ErrAgentNotFound.
func (s *PostgresStore) Get(ctx context.Context, g grn.GRN) (*Agent, error) {
	row, err := s.q.GetAgent(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get", ErrAgentNotFound, nil)
	}
	return rowToAgent(row)
}

// ListByOrg returns the organization's agents ordered by GRN.
func (s *PostgresStore) ListByOrg(ctx context.Context, orgGRN grn.GRN, f listfilter.Filter) ([]*Agent, error) {
	rows, err := s.q.ListAgentsByOrg(ctx, db.ListAgentsByOrgParams{
		OrgGrn:    orgGRN.String(),
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list agents", nil, nil)
	}
	agents := make([]*Agent, 0, len(rows))
	for _, row := range rows {
		a, err := rowToAgent(row)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// ReportProvisioningState records what the operator assigned at epoch
// reports about the agent's pod.
func (s *PostgresStore) ReportProvisioningState(ctx context.Context, agentGRN grn.GRN, epoch int, state ProvisioningState) (*Agent, error) {
	e, err := narrowEpoch(epoch)
	if err != nil {
		return nil, err
	}
	row, err := s.q.ReportAgentProvisioningState(ctx, db.ReportAgentProvisioningStateParams{
		Grn:               agentGRN.String(),
		ProvisioningState: string(state),
		Epoch:             e,
		UpdatedAt:         stamp(time.Now().UTC()),
	})
	if err != nil {
		return nil, s.fenced(ctx, s.q, err, "report provisioning state", agentGRN)
	}
	return rowToAgent(row)
}

// ReportRuntimeState records what the agent assigned at epoch reports
// about itself.
func (s *PostgresStore) ReportRuntimeState(ctx context.Context, agentGRN grn.GRN, epoch int, state RuntimeState, fence Fencing) (*Agent, error) {
	e, err := narrowEpoch(epoch)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	answer, err := q.AdmitExecution(ctx, db.AdmitExecutionParams{
		AgentGrn:  fence.Agent.String(),
		TokenHash: fence.TokenBytes(),
		Serial:    fence.Credential.Serial,
		NotBefore: stamp(fence.Credential.NotBefore),
		NotAfter:  stamp(fence.Credential.NotAfter),
	})
	if err != nil {
		return nil, errorx.Wrap(err, "report runtime state: admit", errorx.KindUnavailable)
	}
	if err := Admission(answer).Err(); err != nil {
		return nil, err
	}
	row, err := q.ReportAgentRuntimeState(ctx, db.ReportAgentRuntimeStateParams{
		Grn:          agentGRN.String(),
		RuntimeState: string(state),
		Epoch:        e,
		UpdatedAt:    stamp(time.Now().UTC()),
	})
	if err != nil {
		// The answer is read in the same transaction: no second
		// connection is asked for while the fence is held.
		return nil, s.fenced(ctx, q, err, "report runtime state", agentGRN)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "commit", errorx.KindUnavailable)
	}
	return rowToAgent(row)
}

// AdmitLegacyIssuance answers whether the operator's certificate
// route may answer for the agent now: its execution is still legacy
// and its cutover is not switched. Both are read under the execution
// row FOR SHARE, so a switch committing first is seen.
func (s *PostgresStore) AdmitLegacyIssuance(ctx context.Context, agentGRN grn.GRN) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	stored, err := q.ShareAgentExecution(ctx, agentGRN.String())
	if err != nil {
		return pgxerr.Classify(err, "execution mode", ErrAgentNotFound, nil)
	}
	mode, err := ParseMode(stored)
	if err != nil {
		return errorx.Wrap(err, "stored execution mode", errorx.KindInternal)
	}
	if mode == ModeFenced {
		return ErrIssuedByLifecycle
	}
	cutOver, err := q.IsAgentCutOver(ctx, agentGRN.String())
	if err != nil {
		return pgxerr.Classify(err, "cutover", nil, nil)
	}
	if cutOver {
		return ErrCutOver
	}
	return nil
}

// narrowEpoch converts a reported epoch to the width of the `epoch`
// column. The module converts to a sqlc parameter without checking;
// here the value decides the fence, and one that truncates onto the
// live epoch would be accepted.
func narrowEpoch(epoch int) (int32, error) {
	e := int32(epoch)
	if int(e) != epoch {
		return 0, ErrStaleEpoch
	}
	return e, nil
}

// fenced says why a state report matched no row: the agent is gone, or
// the epoch is not the one it is assigned at.
func (s *PostgresStore) fenced(ctx context.Context, q *db.Queries, err error, op string, agentGRN grn.GRN) error {
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgxerr.Classify(err, op, nil, nil)
	}
	if _, getErr := q.GetAgent(ctx, agentGRN.String()); getErr != nil {
		return pgxerr.Classify(getErr, op, ErrAgentNotFound, nil)
	}
	return ErrStaleEpoch
}

// rowToAgent converts a sqlc row into the domain aggregate. A stored
// value that no longer parses is data corruption, hence internal.
func rowToAgent(row db.Agent) (*Agent, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "agent grn", errorx.KindInternal)
	}
	orgGRN, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "agent org grn", errorx.KindInternal)
	}
	if err := validateOwnership(g, orgGRN); err != nil {
		return nil, errorx.Wrap(err, "stored agent", errorx.KindInternal)
	}
	provisioning, err := ParseProvisioningState(row.ProvisioningState)
	if err != nil {
		return nil, errorx.Wrap(err, "provisioning state", errorx.KindInternal)
	}
	runtime, err := ParseRuntimeState(row.RuntimeState)
	if err != nil {
		return nil, errorx.Wrap(err, "runtime state", errorx.KindInternal)
	}
	return &Agent{
		grn:          g,
		orgGRN:       orgGRN,
		provisioning: provisioning,
		runtime:      runtime,
		createdAt:    row.CreatedAt.Time,
		updatedAt:    row.UpdatedAt.Time,
	}, nil
}

// CountByOrg counts the organization's agents, definitions and
// templates, which deleting the organization removes with it. One
// query, so the numbers describe one moment rather than several.
func (s *PostgresStore) CountByOrg(ctx context.Context, orgGRN grn.GRN) (OrgCounts, error) {
	row, err := s.q.CountAgentsByOrg(ctx, orgGRN.String())
	if err != nil {
		return OrgCounts{}, pgxerr.Classify(err, "count agents by org", nil, nil)
	}
	return OrgCounts{
		Agents:      int(row.AgentCount),
		Definitions: int(row.DefinitionCount),
		Templates:   int(row.TemplateCount),
	}, nil
}
